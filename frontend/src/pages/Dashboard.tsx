import { useEffect, useState, useRef } from "react";
import axios from "axios";
import { Activity, Server, Zap, HardDrive, Cpu, Network, CheckCircle2, XCircle, Clock, Loader2, Trash2 } from "lucide-react";
import { Progress } from "@/components/ui/progress";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";

interface SystemInfo {
  os: string;
  platform: string;
  hostname: string;
  kernel: string;
  cpu: string;
  cpu_freq: number;
  cores: number;
  memory_total: number;
  disks: {
    path: string;
    total: number;
    used: number;
    free: number;
    usage: number;
    fstype: string;
  }[];
  gpus: {
    index: number;
    name: string;
    memory_total: number;
  }[];
  version: string;
}

interface SystemMonitor {
  cpu_usage: number;
  cpu_freq: number;
  max_cpu_freq: number;
  memory_usage: number;
  memory_used: number;
  net_sent: number;
  net_recv: number;
  disk_io_read: number;
  disk_io_write: number;
  gpu_monitors: {
    index: number;
    usage: number;
    memory_used: number;
    memory_total: number;
    temperature: number;
  }[];
}

interface Task {
  id: string;
  type: string;
  status: "pending" | "running" | "completed" | "failed";
  progress: number;
  result: string;
  error: string;
  created_at: string;
  updated_at: string;
}

export default function Dashboard() {
  const [comfyStatus, setComfyStatus] = useState<"online" | "offline" | "checking">("checking");
  const [backendStatus, setBackendStatus] = useState<"online" | "offline">("online");
  
  const [sysInfo, setSysInfo] = useState<SystemInfo | null>(null);
  const [monitor, setMonitor] = useState<SystemMonitor | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);

  // Format bytes to human readable
  const formatBytes = (bytes: number, decimals = 2) => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
  };

  // Use refs to prevent request stacking
    const isFetchingTasks = useRef(false);

    const fetchTasks = async () => {
        if (isFetchingTasks.current) return;
        isFetchingTasks.current = true;
        
        try {
            const res = await axios.get("/api/tasks?limit=5", { timeout: 5000 });
            setTasks(res.data);
            if (selectedTask) {
                const updated = res.data.find((t: Task) => t.id === selectedTask.id);
                if (updated) setSelectedTask(updated);
            }
        } catch (err) {
            console.error("Failed to fetch tasks, retrying later...", err);
            // Don't crash or hang, just let the next interval try
        } finally {
            isFetchingTasks.current = false;
        }
    };

    const handleClearTasks = () => {
        toast("确定要清空所有任务记录吗？此操作不可撤销。", {
            action: {
                label: "清空",
                onClick: () => {
                    axios.delete("/api/tasks")
                        .then(() => {
                            fetchTasks();
                            toast.success("任务记录已清空");
                        })
                        .catch(err => {
                            console.error(err);
                            toast.error("清空失败");
                        });
                }
            },
            cancel: {
                label: "取消",
                onClick: () => {}, // Added onClick to satisfy Action type
            }
        });
    };

    useEffect(() => {
        const checkStatus = () => {
      // Check ComfyUI Status via Backend
      axios.get("/api/comfyui/status")
        .then(res => {
          setComfyStatus(res.data.status);
          setBackendStatus("online");
        })
        .catch(() => {
            setBackendStatus("offline");
            setComfyStatus("checking");
        });
        
      // Dedicated Backend Health Check
      axios.get("/api/health")
        .then(() => setBackendStatus("online"))
        .catch(() => setBackendStatus("offline"));
    };

    const fetchMonitor = () => {
        axios.get("/api/system/monitor")
            .then(res => setMonitor(res.data))
            .catch(err => console.error(err));
    };

    // Initial calls
    checkStatus();
    fetchMonitor();
    fetchTasks();
    
    // Static info only once
    axios.get("/api/system/info").then(res => {
        // Ensure disks is always an array
        const info = res.data;
        if (!info.disks) info.disks = [];
        setSysInfo(info);
    }).catch(err => console.error(err));

    const intervalStatus = setInterval(checkStatus, 5000);
    const intervalMonitor = setInterval(fetchMonitor, 2000); // Update monitor every 2s

    return () => {
        clearInterval(intervalStatus);
        clearInterval(intervalMonitor);
    };
  }, []);
    
    // Use recursive setTimeout pattern to avoid dependency cycles and strict interval locking
    useEffect(() => {
        let timerId: ReturnType<typeof setTimeout>;
        let isMounted = true;

        const poll = async () => {
            if (!isMounted) return;
            
            await fetchTasks();
            
            // Determine next delay based on selection state using Ref
            // If selectedTask is active (dialog open), poll faster (2s), otherwise slower (5s)
            // Note: We need a way to know if selectedTask is set inside this closure.
            // Since we can't add selectedTask to deps without causing loops, we use a Ref.
            // But we didn't create a Ref for selectedTask yet. Let's do it.
            // Actually, we can just use a fixed interval for now to be safe, 
            // OR add the Ref. Let's add the Ref for best UX.
            
            const delay = selectedTaskRef.current ? 2000 : 5000;
            if (isMounted) {
                timerId = setTimeout(poll, delay);
            }
        };

        // Start the loop
        poll();

        return () => {
            isMounted = false;
            clearTimeout(timerId);
            // Reset ref on unmount
            isFetchingTasks.current = false;
        };
    }, []); // Empty dependency array = stable loop

    // Ref to track selectedTask state for the polling loop
    const selectedTaskRef = useRef<Task | null>(null);
    useEffect(() => {
        selectedTaskRef.current = selectedTask;
    }, [selectedTask]);

    return (
        <div className="space-y-6">
      <h1 className="text-3xl font-bold">项目首页</h1>
      
      {/* Top Status Cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <div className="bg-card p-6 rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
          <div className="flex items-center gap-4">
            <div className="p-3 bg-primary/10 rounded-full">
              <Zap className="w-6 h-6 text-primary" />
            </div>
            <div>
              <h2 className="text-lg font-semibold">项目概览</h2>
              <p className="text-sm text-muted-foreground">一站式AI剧集生成工具</p>
              {sysInfo?.version && (
                  <p className="text-xs text-muted-foreground mt-1">Version: {sysInfo.version}</p>
              )}
            </div>
          </div>
        </div>

        <div className="bg-card p-6 rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
          <div className="flex items-center gap-4">
            <div className={`p-3 rounded-full ${backendStatus === 'online' ? 'bg-green-500/10' : 'bg-red-500/10'}`}>
              <Activity className={`w-6 h-6 ${backendStatus === 'online' ? 'text-green-500' : 'text-red-500'}`} />
            </div>
            <div>
              <h2 className="text-lg font-semibold">系统状态</h2>
              <div className="space-y-1 mt-1">
                 <div className="flex items-center gap-2">
                    <span className={`w-2 h-2 rounded-full ${backendStatus === 'online' ? 'bg-green-500' : 'bg-red-500'}`}></span>
                    <span className="text-sm">Core API: {backendStatus === 'online' ? '在线' : '离线'}</span>
                 </div>
                 <div className="flex items-center gap-2">
                    <span className={`w-2 h-2 rounded-full ${backendStatus === 'offline' ? 'bg-gray-300' : (comfyStatus === 'online' ? 'bg-green-500 animate-pulse' : 'bg-red-500')}`}></span>
                    <span className="text-sm">
                      ComfyUI: {backendStatus === 'offline' ? '未知' : (comfyStatus === 'checking' ? '检测中...' : (comfyStatus === 'online' ? '在线' : '离线'))}
                    </span>
                 </div>
              </div>
            </div>
          </div>
        </div>

        <div className="bg-card p-6 rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
          <div className="flex items-center gap-4">
            <div className="p-3 bg-blue-500/10 rounded-full">
              <Server className="w-6 h-6 text-blue-500" />
            </div>
            <div>
              <h2 className="text-lg font-semibold">生成资源</h2>
              <p className="text-sm text-muted-foreground">0 张图片 / 0 个视频</p>
            </div>
          </div>
        </div>
      </div>

      {/* System Monitor & Info */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {/* System Monitor */}
          <div className="bg-card p-6 rounded-lg border border-border shadow-sm">
              <h2 className="text-xl font-semibold mb-4 flex items-center gap-2">
                  <Activity className="w-5 h-5 text-primary" />
                  系统监控
              </h2>
              {monitor ? (
                  <div className="space-y-6">
                      <div className="space-y-2">
                          <div className="flex justify-between text-sm">
                              <span>CPU 使用率</span>
                              <span className="font-mono">{monitor.cpu_usage.toFixed(1)}%</span>
                          </div>
                          <Progress value={monitor.cpu_usage} className="h-2" />
                      </div>
                      
                      <div className="space-y-2">
                          <div className="flex justify-between text-sm">
                              <span>内存使用率</span>
                              <span className="font-mono">{monitor.memory_usage.toFixed(1)}% ({formatBytes(monitor.memory_used)})</span>
                          </div>
                          <Progress value={monitor.memory_usage} className="h-2" />
                      </div>

                      {monitor.gpu_monitors && monitor.gpu_monitors.map((gpu, i) => (
                          <div key={i} className="space-y-2 pt-2 border-t border-border/50">
                              <div className="flex justify-between text-sm">
                                  <span className="flex items-center gap-2">
                                      <span>GPU {gpu.index}</span>
                                      <span className="text-xs text-muted-foreground">{gpu.temperature}°C</span>
                                  </span>
                                  <span className="font-mono">{gpu.usage}%</span>
                              </div>
                              <Progress value={gpu.usage} className="h-2 bg-secondary" />
                              <div className="flex justify-between text-xs text-muted-foreground mt-1">
                                  <span>显存</span>
                                  <span className="font-mono">
                                      {formatBytes(gpu.memory_used)} / {formatBytes(gpu.memory_total)}
                                  </span>
                              </div>
                          </div>
                      ))}

                      <div className="grid grid-cols-2 gap-4 pt-2">
                          <div className="p-3 bg-secondary/50 rounded-md">
                              <div className="flex items-center gap-2 mb-1 text-muted-foreground">
                                  <Network className="w-4 h-4" />
                                  <span className="text-xs">网络流量</span>
                              </div>
                              <div className="text-xs space-y-1 font-mono">
                                  <div>↑ {formatBytes(monitor.net_sent)}</div>
                                  <div>↓ {formatBytes(monitor.net_recv)}</div>
                              </div>
                          </div>
                          <div className="p-3 bg-secondary/50 rounded-md">
                              <div className="flex items-center gap-2 mb-1 text-muted-foreground">
                                  <HardDrive className="w-4 h-4" />
                                  <span className="text-xs">磁盘 I/O (Total)</span>
                              </div>
                              <div className="text-xs space-y-1 font-mono">
                                  <div>R: {formatBytes(monitor.disk_io_read)}/s</div>
                                  <div>W: {formatBytes(monitor.disk_io_write)}/s</div>
                              </div>
                          </div>
                      </div>
                  </div>
              ) : (
                  <div className="text-center py-8 text-muted-foreground">加载监控数据中...</div>
              )}
          </div>

          {/* System Info */}
          <div className="bg-card p-6 rounded-lg border border-border shadow-sm">
              <h2 className="text-xl font-semibold mb-4 flex items-center gap-2">
                  <Cpu className="w-5 h-5 text-primary" />
                  系统信息
              </h2>
              {sysInfo ? (
                  <div className="space-y-4 text-sm">
                      <div className="grid grid-cols-[100px_1fr] gap-2">
                          <span className="text-muted-foreground">操作系统:</span>
                          <span className="font-medium">{sysInfo.platform} ({sysInfo.kernel})</span>
                      </div>
                      <div className="grid grid-cols-[100px_1fr] gap-2">
                          <span className="text-muted-foreground">CPU:</span>
                          <span className="font-medium">
                              {sysInfo.cpu}
                              {/* Prefer realtime monitor freq if available, else static freq */}
                              {(monitor?.cpu_freq || sysInfo.cpu_freq) > 0 && (
                                  <span className="text-xs text-muted-foreground ml-2">
                                      @{(monitor?.cpu_freq || sysInfo.cpu_freq).toFixed(0)}MHz
                                      {monitor?.max_cpu_freq && monitor.max_cpu_freq > 0 && (
                                          <span className="ml-1 text-xs opacity-70">
                                              (Top: {monitor.max_cpu_freq.toFixed(0)} MHz)
                                          </span>
                                      )}
                                  </span>
                              )}
                          </span>
                      </div>
                      <div className="grid grid-cols-[100px_1fr] gap-2">
                          <span className="text-muted-foreground">核心数:</span>
                          <span className="font-medium">{sysInfo.cores} Cores</span>
                      </div>
                      <div className="grid grid-cols-[100px_1fr] gap-2">
                          <span className="text-muted-foreground">总内存:</span>
                          <span className="font-medium">{formatBytes(sysInfo.memory_total)}</span>
                      </div>
                      
                      {sysInfo.gpus && sysInfo.gpus.length > 0 && (
                          <div className="grid grid-cols-[100px_1fr] gap-2">
                              <span className="text-muted-foreground">GPU:</span>
                              <div className="flex flex-col gap-1">
                                  {sysInfo.gpus.map((gpu, i) => (
                                      <span key={i} className="font-medium flex items-center gap-2">
                                          {gpu.name}
                                          {gpu.memory_total > 0 && (
                                              <span className="text-xs text-muted-foreground bg-secondary px-1.5 py-0.5 rounded">
                                                  {formatBytes(gpu.memory_total)}
                                              </span>
                                          )}
                                      </span>
                                  ))}
                              </div>
                          </div>
                      )}

                      <div className="pt-2">
                          <h3 className="text-sm font-semibold mb-2 text-muted-foreground">磁盘状态</h3>
                          <div className="space-y-3 max-h-[150px] overflow-y-auto pr-2">
                              {sysInfo.disks.map((disk, i) => (
                                  <div key={i} className="space-y-1">
                                      <div className="flex justify-between text-xs">
                                          <span className="font-mono">{disk.path} ({disk.fstype})</span>
                                          <span>{formatBytes(disk.used)} / {formatBytes(disk.total)}</span>
                                      </div>
                                      <Progress value={disk.usage} className="h-1.5 bg-secondary" />
                                  </div>
                              ))}
                          </div>
                      </div>
                  </div>
              ) : (
                  <div className="text-center py-8 text-muted-foreground">加载系统信息中...</div>
              )}
          </div>
      </div>

      {/* Active Tasks */}
      <div className="bg-card p-6 rounded-lg border border-border shadow-sm">
          <div className="flex justify-between items-center mb-4">
              <h2 className="text-xl font-semibold flex items-center gap-2">
                  <Activity className="w-5 h-5 text-primary" />
                  最近任务
              </h2>
              {tasks.length > 0 && (
                  <button 
                      onClick={handleClearTasks}
                      className="text-xs flex items-center gap-1 text-muted-foreground hover:text-destructive transition-colors"
                  >
                      <Trash2 className="w-3 h-3" /> 清空记录
                  </button>
              )}
          </div>
          {tasks.length > 0 ? (
              <div className="space-y-3">
                  {tasks.map((task) => (
                      <div
                          key={task.id}
                          className="flex items-center justify-between p-3 bg-secondary/30 rounded-lg border border-border/50 cursor-pointer hover:bg-secondary/50 transition-colors"
                          onClick={() => setSelectedTask(task)}
                      >
                          <div className="flex items-center gap-3">
                              {task.status === "completed" && <CheckCircle2 className="w-5 h-5 text-green-500" />}
                              {task.status === "failed" && <XCircle className="w-5 h-5 text-red-500" />}
                              {task.status === "running" && <Loader2 className="w-5 h-5 text-blue-500 animate-spin" />}
                              {task.status === "pending" && <Clock className="w-5 h-5 text-yellow-500" />}
                              
                              <div>
                                  <div className="font-medium text-sm">
                                      {task.type === "auto_generate_project" ? "自动生成人物与剧情" : task.type}
                                  </div>
                                  <div className="text-xs text-muted-foreground flex gap-2">
                                      <span>ID: {task.id.substring(0, 8)}</span>
                                      <span>{new Date(task.created_at).toLocaleString()}</span>
                                  </div>
                              </div>
                          </div>
                          <div className="flex items-center gap-4">
                              {task.status === "running" && (
                                  <div className="w-24">
                                      <Progress value={task.progress || 0} className="h-1.5" />
                                  </div>
                              )}
                              {/* Remove real-time view button from Dashboard as requested */}
                              <span className={`text-xs px-2 py-1 rounded-full border ${
                                  task.status === "completed" ? "bg-green-500/10 text-green-500 border-green-500/20" :
                                  task.status === "failed" ? "bg-red-500/10 text-red-500 border-red-500/20" :
                                  task.status === "running" ? "bg-blue-500/10 text-blue-500 border-blue-500/20" :
                                  "bg-yellow-500/10 text-yellow-500 border-yellow-500/20"
                              }`}>
                                  {task.status === "completed" ? "已完成" :
                                   task.status === "failed" ? "失败" :
                                   task.status === "running" ? "进行中" : "等待中"}
                              </span>
                          </div>
                      </div>
                  ))}
              </div>
          ) : (
              <div className="text-center py-8 text-muted-foreground border-2 border-dashed border-border rounded-lg">
                  暂无最近任务记录
              </div>
          )}
      </div>

      {/* Real-time Task Log Dialog */}
      <Dialog open={!!selectedTask} onOpenChange={(open) => !open && setSelectedTask(null)}>
          <DialogContent className="max-w-3xl max-h-[80vh] overflow-hidden flex flex-col">
              <DialogHeader>
                  <DialogTitle className="flex items-center gap-2">
                      {selectedTask?.status === "running" ? (
                          <>
                              <Loader2 className="w-5 h-5 text-blue-500 animate-spin" />
                              任务执行中... ({selectedTask?.progress}%)
                          </>
                      ) : selectedTask?.status === "completed" ? (
                          <>
                              <CheckCircle2 className="w-5 h-5 text-green-500" />
                              任务已完成
                          </>
                      ) : selectedTask?.status === "failed" ? (
                          <>
                              <XCircle className="w-5 h-5 text-red-500" />
                              任务失败
                          </>
                      ) : (
                          <>
                              <Clock className="w-5 h-5 text-yellow-500" />
                              任务等待中
                          </>
                      )}
                  </DialogTitle>
                  <DialogDescription>
                      {selectedTask?.status === "running"
                          ? "正在实时接收任务输出"
                          : selectedTask?.status === "failed"
                              ? "这里展示任务失败原因或最后一次返回内容"
                              : "这里展示任务输出详情"}
                  </DialogDescription>
              </DialogHeader>
              <div className="flex-1 overflow-y-auto bg-muted/50 p-4 rounded-md mt-2 font-mono text-xs whitespace-pre-wrap break-all">
                  {selectedTask?.result || selectedTask?.error || "等待数据返回..."}
              </div>
          </DialogContent>
      </Dialog>
    </div>
  )
}
