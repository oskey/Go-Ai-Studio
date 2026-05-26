package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/gin-gonic/gin"
)

func upsertLLMStreamState(streamID uint, taskID string, provider models.LLMProvider, label string, content string, status string) uint {
	if status == "" {
		status = "running"
	}

	entry := models.LLMStreamState{
		TaskID:       strings.TrimSpace(taskID),
		ProviderName: llmProviderDisplayName(provider),
		Label:        label,
		Status:       status,
		Content:      content,
		CharCount:    len([]rune(content)),
	}

	if streamID == 0 {
		db.DB.Create(&entry)
		return entry.ID
	}

	db.DB.Model(&models.LLMStreamState{}).Where("id = ?", streamID).Updates(map[string]interface{}{
		"task_id":       entry.TaskID,
		"provider_name": entry.ProviderName,
		"label":         entry.Label,
		"status":        entry.Status,
		"content":       entry.Content,
		"char_count":    entry.CharCount,
		"updated_at":    time.Now(),
	})
	return streamID
}

func finalizeLLMStreamState(streamID uint, taskID string, provider models.LLMProvider, label string, content string, status string) {
	if streamID == 0 {
		if content == "" {
			return
		}
		upsertLLMStreamState(0, taskID, provider, label, content, status)
		return
	}
	upsertLLMStreamState(streamID, taskID, provider, label, content, status)
}

func GetCurrentLLMStream(c *gin.Context) {
	var stream models.LLMStreamState
	query := db.DB.Order("CASE WHEN status = 'running' THEN 0 ELSE 1 END").Order("updated_at desc")
	if err := query.First(&stream).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"stream": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"stream": stream})
}

func GetTaskLLMStream(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id is required"})
		return
	}

	var stream models.LLMStreamState
	query := db.DB.Where("task_id = ?", taskID).
		Order("CASE WHEN status = 'running' THEN 0 ELSE 1 END").
		Order("updated_at desc")
	if err := query.First(&stream).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"stream": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"stream": stream})
}

func markTaskLLMStreamFailed(taskID string, failureMessage string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	failureMessage = strings.TrimSpace(failureMessage)
	if failureMessage == "" {
		failureMessage = "未知错误"
	}

	var stream models.LLMStreamState
	if err := db.DB.Where("task_id = ?", taskID).Order("updated_at desc").First(&stream).Error; err != nil {
		return
	}

	content := strings.TrimSpace(stream.Content)
	errorLine := fmt.Sprintf("[解析失败] %s", failureMessage)
	if content == "" {
		content = errorLine
	} else if !strings.Contains(content, errorLine) {
		content = strings.TrimSpace(content + "\n\n" + errorLine)
	}

	db.DB.Model(&models.LLMStreamState{}).Where("id = ?", stream.ID).Updates(map[string]interface{}{
		"status":     "failed",
		"content":    content,
		"char_count": len([]rune(content)),
		"updated_at": time.Now(),
	})
}
