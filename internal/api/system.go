package api

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"

	// "github.com/NVIDIA/go-nvml/pkg/nvml" // Disabled due to missing CGO/GCC
	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// Global state for rate calculation
var (
	lastMonitorTime time.Time
	lastDiskRead    uint64
	lastDiskWrite   uint64
	lastNetSent     uint64
	lastNetRecv     uint64
	maxCPUFreq      float64

	// Cache for monitor data
	monitorCache      SystemMonitor
	monitorCacheMutex sync.RWMutex
	updateMutex       sync.Mutex
)

// InitSystemMonitor starts the background updater
func InitSystemMonitor() {
	// Initialize cache with empty data
	monitorCacheMutex.Lock()
	monitorCache = SystemMonitor{}
	lastMonitorTime = time.Now()
	monitorCacheMutex.Unlock()

	// Start background goroutine
	/*
		go func() {
			// Recovery from panic to keep the monitor alive
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("System Monitor background task panicked: %v\n", r)
					// Restart the monitor if needed or just log
					// In a real app, we might want to restart it after a delay
					time.Sleep(5 * time.Second)
					InitSystemMonitor()
				}
			}()

			ticker := time.NewTicker(2 * time.Second)
			for range ticker.C {
				updateSystemMetrics()
			}
		}()
	*/
}

func updateSystemMetrics() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("updateSystemMetrics panicked: %v\n", r)
		}
	}()

	// CPU
	percent, _ := cpu.Percent(0, false)
	cpuUsage := 0.0
	if len(percent) > 0 {
		cpuUsage = percent[0]
	}

	// CPU Frequency (Realtime)
	cpuFreq := 0.0

	// Windows WMI Fallback for CPU Frequency if gopsutil fails or returns static base clock
	if runtime.GOOS == "windows" {
		// Use a single PowerShell command to get both Base Frequency and Performance Percentage
		// Formula: BaseFreq * (% Processor Performance / 100)
		psCmd := `
$proc = Get-CimInstance Win32_Processor | Select-Object -First 1
$base = $proc.MaxClockSpeed
$perf = (Get-Counter '\Processor Information(_Total)\% Processor Performance').CounterSamples.CookedValue
if ($base -gt 0 -and $perf -gt 0) {
    $real = $base * ($perf / 100)
    Write-Output $real
} else {
    Write-Output 0
}
`
		cmd := exec.Command("powershell", "-Command", psCmd)
		// Hide window to prevent flashing (though in service/backend mode it shouldn't show)
		// cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

		if out, err := cmd.Output(); err == nil {
			output := strings.TrimSpace(string(out))
			output = strings.ReplaceAll(output, ",", ".")
			var freq float64
			if _, err := fmt.Sscanf(output, "%f", &freq); err == nil && freq > 0 {
				cpuFreq = freq
			}
		}
	}

	// Fallback to gopsutil if PowerShell failed or on other OS
	if cpuFreq == 0 {
		if cInfo, err := cpu.Info(); err == nil && len(cInfo) > 0 {
			cpuFreq = cInfo[0].Mhz
		}
	}

	if cpuFreq > maxCPUFreq {
		maxCPUFreq = cpuFreq
	}

	// Memory
	mInfo, _ := mem.VirtualMemory()

	// Disk IO & Network IO (Rate Calculation)
	// On Windows, gopsutil disk.IOCounters() needs no args to get all drives
	currentDiskIO, _ := disk.IOCounters()
	currentNetIO, _ := net.IOCounters(false)

	now := time.Now()
	duration := now.Sub(lastMonitorTime).Seconds()
	if duration <= 0 {
		duration = 1
	}

	diskReadRate := uint64(0)
	diskWriteRate := uint64(0)
	netSentRate := uint64(0)
	netRecvRate := uint64(0)

	totalRead := uint64(0)
	totalWrite := uint64(0)
	for _, io := range currentDiskIO {
		totalRead += io.ReadBytes
		totalWrite += io.WriteBytes
	}

	if lastDiskRead > 0 {
		diskReadRate = uint64(float64(totalRead-lastDiskRead) / duration)
		diskWriteRate = uint64(float64(totalWrite-lastDiskWrite) / duration)
	}
	lastDiskRead = totalRead
	lastDiskWrite = totalWrite

	totalSent := uint64(0)
	totalRecv := uint64(0)
	if len(currentNetIO) > 0 {
		totalSent = currentNetIO[0].BytesSent
		totalRecv = currentNetIO[0].BytesRecv
	}

	if lastNetSent > 0 {
		netSentRate = uint64(float64(totalSent-lastNetSent) / duration)
		netRecvRate = uint64(float64(totalRecv-lastNetRecv) / duration)
	}
	lastNetSent = totalSent
	lastNetRecv = totalRecv
	lastMonitorTime = now

	// GPU (Skipped for now or add lightweight check if needed)
	var gpuMonitors []GPUMonitor

	// Update Cache
	monitorCacheMutex.Lock()
	monitorCache = SystemMonitor{
		CPUUsage:    cpuUsage,
		CPUFreq:     cpuFreq,
		MaxCPUFreq:  maxCPUFreq,
		MemoryUsage: mInfo.UsedPercent,
		MemoryUsed:  mInfo.Used,
		NetSent:     netSentRate,
		NetRecv:     netRecvRate,
		DiskIORead:  diskReadRate,
		DiskIOWrite: diskWriteRate,
		GPUMonitors: gpuMonitors,
	}
	monitorCacheMutex.Unlock()
}

// InitNVML tries to initialize NVIDIA Management Library
// Returns true if successful, false otherwise (e.g. no NVIDIA GPU)
// Note: This requires CGO and NVML installed. If failing, we disable it.
func InitNVML() bool {
	// ret := nvml.Init()
	// return ret == nvml.SUCCESS
	return false // Disabled due to missing CGO/GCC environment
}

type SystemInfo struct {
	OS          string     `json:"os"`
	Platform    string     `json:"platform"`
	Hostname    string     `json:"hostname"`
	Kernel      string     `json:"kernel"`
	CPU         string     `json:"cpu"`
	CPUFreq     float64    `json:"cpu_freq"` // MHz
	Cores       int        `json:"cores"`
	MemoryTotal uint64     `json:"memory_total"`
	Disks       []DiskInfo `json:"disks"`
	GPUs        []GPUInfo  `json:"gpus"`
	Version     string     `json:"version"`
}

type GPUInfo struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	MemoryTotal uint64 `json:"memory_total"`
}

type DiskInfo struct {
	Path   string  `json:"path"`
	Total  uint64  `json:"total"`
	Used   uint64  `json:"used"`
	Free   uint64  `json:"free"`
	Usage  float64 `json:"usage"`
	Fstype string  `json:"fstype"`
}

type SystemMonitor struct {
	CPUUsage    float64      `json:"cpu_usage"`
	CPUFreq     float64      `json:"cpu_freq"` // MHz (Realtime)
	MemoryUsage float64      `json:"memory_usage"`
	MemoryUsed  uint64       `json:"memory_used"`
	NetSent     uint64       `json:"net_sent"`
	NetRecv     uint64       `json:"net_recv"`
	DiskIORead  uint64       `json:"disk_io_read"`
	DiskIOWrite uint64       `json:"disk_io_write"`
	GPUMonitors []GPUMonitor `json:"gpu_monitors"`
	MaxCPUFreq  float64      `json:"max_cpu_freq"` // Max observed frequency
}

type GPUMonitor struct {
	Index       int    `json:"index"`
	Usage       uint32 `json:"usage"`        // %
	MemoryUsed  uint64 `json:"memory_used"`  // Bytes
	MemoryTotal uint64 `json:"memory_total"` // Bytes
	Temperature uint32 `json:"temperature"`  // Celsius
}

// GetSystemInfo returns static system information
func GetSystemInfo(c *gin.Context) {
	hInfo, _ := host.Info()
	cInfo, _ := cpu.Info()
	mInfo, _ := mem.VirtualMemory()

	cpuModel := ""
	cpuFreq := 0.0
	if len(cInfo) > 0 {
		cpuModel = cInfo[0].ModelName
		cpuFreq = cInfo[0].Mhz
	}

	partitions, _ := disk.Partitions(true)
	var disks []DiskInfo
	for _, p := range partitions {
		// Filter out some common virtual/system partitions on Linux if needed
		// For Windows, we usually want to see all drive letters
		if usage, err := disk.Usage(p.Mountpoint); err == nil {
			disks = append(disks, DiskInfo{
				Path:   p.Mountpoint,
				Total:  usage.Total,
				Used:   usage.Used,
				Free:   usage.Free,
				Usage:  usage.UsedPercent,
				Fstype: p.Fstype,
			})
		}
	}

	// GPU Info (Static)
	var gpus []GPUInfo

	// Try PowerShell for Windows GPU info if NVML fails/disabled
	if runtime.GOOS == "windows" {
		// Use Get-CimInstance instead of deprecated wmic
		cmd := exec.Command("powershell", "-Command", "Get-CimInstance Win32_VideoController | Select-Object -ExpandProperty Name")
		if out, err := cmd.Output(); err == nil {
			output := strings.TrimSpace(string(out))
			if output != "" {
				lines := strings.Split(output, "\r\n")
				idx := 0
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line != "" {
						gpus = append(gpus, GPUInfo{
							Index:       idx,
							Name:        line,
							MemoryTotal: 0,
						})
						idx++
					}
				}
			}
		}
	} else if runtime.GOOS == "linux" {
		// Linux: Try lspci
		if out, err := exec.Command("lspci").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			idx := 0
			for _, line := range lines {
				if strings.Contains(line, "VGA") || strings.Contains(line, "3D controller") {
					parts := strings.Split(line, ":")
					if len(parts) > 1 {
						name := strings.TrimSpace(parts[len(parts)-1])
						gpus = append(gpus, GPUInfo{
							Index:       idx,
							Name:        name,
							MemoryTotal: 0,
						})
						idx++
					}
				}
			}
		}
	} else if runtime.GOOS == "darwin" {
		// macOS: system_profiler
		if out, err := exec.Command("system_profiler", "SPDisplaysDataType").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			idx := 0
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "Chipset Model:") {
					name := strings.TrimSpace(strings.TrimPrefix(line, "Chipset Model:"))
					gpus = append(gpus, GPUInfo{
						Index:       idx,
						Name:        name,
						MemoryTotal: 0,
					})
					idx++
				}
			}
		}
	}

	if len(gpus) == 0 && InitNVML() {
		/*
			defer nvml.Shutdown()
			count, ret := nvml.DeviceGetCount()
			if ret == nvml.SUCCESS {
				for i := 0; i < count; i++ {
					device, ret := nvml.DeviceGetHandleByIndex(i)
					if ret == nvml.SUCCESS {
						name, _ := device.GetName()
						memInfo, _ := device.GetMemoryInfo()
						gpus = append(gpus, GPUInfo{
							Index:       i,
							Name:        name,
							MemoryTotal: memInfo.Total,
						})
					}
				}
			}
		*/
	}

	// Read version.txt
	version := "Unknown"
	if verBytes, err := os.ReadFile("version.txt"); err == nil {
		version = strings.TrimSpace(string(verBytes))
	}

	info := SystemInfo{
		OS:          hInfo.OS,
		Platform:    hInfo.Platform + " " + hInfo.PlatformVersion,
		Hostname:    hInfo.Hostname,
		Kernel:      hInfo.KernelVersion,
		CPU:         cpuModel,
		CPUFreq:     cpuFreq,
		Cores:       runtime.NumCPU(),
		MemoryTotal: mInfo.Total,
		Disks:       disks,
		GPUs:        gpus,
		Version:     version,
	}

	c.JSON(http.StatusOK, info)
}

// ListTasks returns recent tasks
func ListTasks(c *gin.Context) {
	limit := 10
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
		limit = l
	}
	tasks := task.GlobalTaskManager.GetTasks(limit)
	c.JSON(http.StatusOK, tasks)
}

func GetTask(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id is required"})
		return
	}

	taskRecord := task.GlobalTaskManager.GetTask(taskID)
	if taskRecord == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}
	if maybeMarkGeneralGuidePlanningTaskStale(taskRecord) {
		taskRecord = task.GlobalTaskManager.GetTask(taskID)
	}

	c.JSON(http.StatusOK, taskRecord)
}

func maybeMarkGeneralGuidePlanningTaskStale(taskRecord *models.Task) bool {
	if taskRecord == nil {
		return false
	}
	if taskRecord.Type != "plan_general_guide_project" {
		return false
	}
	if taskRecord.Status != "running" && taskRecord.Status != "pending" {
		return false
	}
	if time.Since(taskRecord.UpdatedAt) < 90*time.Second {
		return false
	}

	task.GlobalTaskManager.UpdateTaskStatus(taskRecord.ID, "failed", taskRecord.Progress, "场景规划任务长时间未更新，已自动标记为失败，请重新尝试")
	_ = db.DB.Model(&models.GeneralGuideProject{}).Where("current_planning_task_id = ?", taskRecord.ID).Updates(map[string]interface{}{
		"current_planning_task_id": "",
		"last_planning_error":      "场景规划任务长时间未更新，已自动标记为失败，请重新尝试",
		"updated_at":               time.Now(),
	}).Error
	return true
}

// ClearTasks deletes all tasks from the database and stops running processes
func ClearTasks(c *gin.Context) {
	// 1. Clear TaskManager (Memory + DB)
	if err := task.GlobalTaskManager.ClearAllTasks(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear tasks: " + err.Error()})
		return
	}

	// 2. Stop ComfyUI Generation
	go func() {
		if err := StopComfyUI(); err != nil {
			fmt.Printf("Failed to stop ComfyUI: %v\n", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "Tasks cleared and ComfyUI stopped successfully"})
}

// GetSystemMonitor returns real-time monitoring data
func GetSystemMonitor(c *gin.Context) {
	// Determine min interval based on running tasks
	minInterval := 2 * time.Second
	if task.GlobalTaskManager.HasRunningTasks() {
		minInterval = 20 * time.Second
	}

	// Check if update is needed
	monitorCacheMutex.RLock()
	needsUpdate := time.Since(lastMonitorTime) > minInterval
	monitorCacheMutex.RUnlock()

	if needsUpdate {
		// Ensure only one update happens at a time
		updateMutex.Lock()

		// Double check condition
		monitorCacheMutex.RLock()
		stillNeedsUpdate := time.Since(lastMonitorTime) > minInterval
		monitorCacheMutex.RUnlock()

		if stillNeedsUpdate {
			updateSystemMetrics()
		}
		updateMutex.Unlock()
	}

	// Return cached data immediately
	monitorCacheMutex.RLock()
	monitor := monitorCache
	monitorCacheMutex.RUnlock()

	c.JSON(http.StatusOK, monitor)
}

// GetSystemMonitorReal is the actual heavy implementation placeholder
// ... (Logic removed to fix compilation errors and latency)
