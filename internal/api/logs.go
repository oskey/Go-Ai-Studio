package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/gin-gonic/gin"
)

// LogLevel constants
const (
	LogLevelInfo  = "INFO"
	LogLevelWarn  = "WARN"
	LogLevelError = "ERROR"
)

// Log creates a new system log entry
// This is an internal helper function, not an API handler
var logChannel = make(chan models.SystemLog, 1000)

func init() {
	go processLogs()
}

func processLogs() {
	// Batch processing or simple buffered write
	// For now, simple buffered sequential write to avoid locking
	for logEntry := range logChannel {
		persistSystemLog(logEntry)
	}
}

func persistSystemLog(logEntry models.SystemLog) {
	// Use a retry mechanism for busy DB
	for i := 0; i < 3; i++ {
		if err := db.DB.Create(&logEntry).Error; err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond) // Backoff
	}
}

func Log(level, message, details string) {
	logEntry := models.SystemLog{
		Level:     level,
		Message:   message,
		Details:   details,
		CreatedAt: time.Now(),
	}

	// Non-blocking send if channel is full
	select {
	case logChannel <- logEntry:
	default:
		// Fall back to direct persistence instead of silently dropping logs.
		persistSystemLog(logEntry)
	}
}

func logComfyWorkflowPayload(message string, workflowName string, workflowJSON map[string]interface{}) {
	details := map[string]interface{}{
		"workflow_name": strings.TrimSpace(workflowName),
		"workflow":      workflowJSON,
	}
	payloadBytes, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		Log(LogLevelError, "ComfyUI Payload Log Failed", err.Error())
		return
	}
	Log(LogLevelInfo, message, string(payloadBytes))
}

// ListLogs retrieves system logs with pagination.
func ListLogs(c *gin.Context) {
	var logs []models.SystemLog

	page := 1
	if value := c.Query("page"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			page = parsed
		}
	}

	limit := 100
	if value := c.Query("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 500 {
		limit = 500
	}

	var total int64
	if err := db.DB.Model(&models.SystemLog{}).Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count logs"})
		return
	}

	totalPages := 1
	if total > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}
	if page > totalPages {
		page = totalPages
	}

	offset := (page - 1) * limit
	query := db.DB.Order("created_at desc").Offset(offset).Limit(limit)

	if err := query.Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch logs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":       logs,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": totalPages,
	})
}

// ClearLogs deletes all system logs
func ClearLogs(c *gin.Context) {
	if err := db.DB.Exec("DELETE FROM system_logs").Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear logs"})
		return
	}

	Log(LogLevelWarn, "系统日志已清空", "用户手动清空了所有系统日志")
	c.JSON(http.StatusOK, gin.H{"message": "Logs cleared successfully"})
}
