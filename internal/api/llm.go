package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/sashabaranov/go-openai"
)

// AddLLMProvider adds a new LLM provider
func AddLLMProvider(c *gin.Context) {
	var provider models.LLMProvider
	if err := c.ShouldBindJSON(&provider); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	provider.CreatedAt = time.Now()
	provider.UpdatedAt = time.Now()

	if err := db.DB.Create(&provider).Error; err != nil {
		Log(LogLevelError, "创建 LLM 引擎失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create provider"})
		return
	}

	Log(LogLevelInfo, "创建 LLM 引擎", fmt.Sprintf("创建了新引擎: %s (%s)", provider.Name, provider.Provider))

	// 如果这是第一个创建的提供商，自动设为激活状态
	var count int64
	db.DB.Model(&models.LLMProvider{}).Count(&count)
	if count == 1 {
		provider.IsActive = true
		db.DB.Save(&provider)
	}

	c.JSON(http.StatusCreated, provider)
}

// ListLLMProviders retrieves all LLM providers
func ListLLMProviders(c *gin.Context) {
	var providers []models.LLMProvider
	if err := db.DB.Find(&providers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch providers"})
		return
	}
	if err := attachProviderUsageStats(providers); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch provider usage statistics"})
		return
	}
	c.JSON(http.StatusOK, providers)
}

// UpdateLLMProvider updates an existing LLM provider
func UpdateLLMProvider(c *gin.Context) {
	id := c.Param("id")
	var provider models.LLMProvider
	if err := db.DB.First(&provider, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Provider not found"})
		return
	}

	var updateData models.LLMProvider
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	provider.Name = updateData.Name
	provider.Provider = updateData.Provider
	provider.APIAddress = updateData.APIAddress
	provider.APIKey = updateData.APIKey
	provider.ModelName = updateData.ModelName
	provider.EnableAdvancedRequestParams = updateData.EnableAdvancedRequestParams
	provider.RequestMaxTokens = updateData.RequestMaxTokens
	provider.RequestTemperature = updateData.RequestTemperature
	provider.UpdatedAt = time.Now()

	if err := db.DB.Save(&provider).Error; err != nil {
		Log(LogLevelError, "更新 LLM 引擎失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update provider"})
		return
	}

	Log(LogLevelInfo, "更新 LLM 引擎", fmt.Sprintf("更新了引擎: %s", provider.Name))
	c.JSON(http.StatusOK, provider)
}

// SetActiveLLMProvider sets the active provider
func SetActiveLLMProvider(c *gin.Context) {
	id := c.Param("id")

	// 开启事务
	tx := db.DB.Begin()

	// 1. 将所有提供商设为非激活
	if err := tx.Model(&models.LLMProvider{}).Where("1 = 1").Update("is_active", false).Error; err != nil {
		tx.Rollback()
		Log(LogLevelError, "重置默认 LLM 引擎失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset active providers"})
		return
	}

	// 2. 将指定提供商设为激活
	if err := tx.Model(&models.LLMProvider{}).Where("id = ?", id).Update("is_active", true).Error; err != nil {
		tx.Rollback()
		Log(LogLevelError, "设置默认 LLM 引擎失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set active provider"})
		return
	}

	tx.Commit()
	Log(LogLevelInfo, "切换默认 LLM 引擎", fmt.Sprintf("切换默认引擎 ID: %s", id))
	c.JSON(http.StatusOK, gin.H{"message": "Active provider updated successfully"})
}

// DeleteLLMProvider deletes an LLM provider
func DeleteLLMProvider(c *gin.Context) {
	id := c.Param("id")
	if err := db.DB.Delete(&models.LLMProvider{}, id).Error; err != nil {
		Log(LogLevelError, "删除 LLM 引擎失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete provider"})
		return
	}

	Log(LogLevelInfo, "删除 LLM 引擎", fmt.Sprintf("删除引擎 ID: %s", id))
	c.JSON(http.StatusOK, gin.H{"message": "Provider deleted successfully"})
}

// TestLLMConnection tests the connection to the LLM provider
func TestLLMConnection(c *gin.Context) {
	var provider models.LLMProvider
	if err := c.ShouldBindJSON(&provider); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := testLLMConnection(provider)
	if err != nil {
		Log(LogLevelWarn, "测试 LLM 连接失败", fmt.Sprintf("引擎: %s, 错误: %v", provider.Name, err))
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("连接失败: %v", err)})
		return
	}

	successMsg := fmt.Sprintf("【%s】连接成功！LLM 引擎响应正常。", provider.ModelName)
	if provider.ModelName == "" {
		successMsg = "连接成功！LLM 引擎响应正常。"
	}

	Log(LogLevelInfo, "测试 LLM 连接成功", fmt.Sprintf("引擎: %s, 模型: %s", provider.Name, provider.ModelName))

	c.JSON(http.StatusOK, gin.H{"success": true, "message": successMsg})
}

func testLLMConnection(p models.LLMProvider) error {
	if p.APIAddress == "" {
		return fmt.Errorf("API 地址是必填项")
	}
	if p.Provider != "Local" && p.APIKey == "" {
		return fmt.Errorf("API Key 是必填项")
	}
	model, err := requireProviderModelName(p)
	if err != nil {
		return err
	}

	req := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "你是一个测试助手。请只返回一个JSON格式的响应，内容为: {\"status\": \"success\", \"message\": \"communication established\"}。不要包含任何其他文字。",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: "测试连接。",
			},
		},
	}

	content, err := requestLLMContentNonStreaming(p, req, 30*time.Second, "", "")
	if err != nil {
		return fmt.Errorf("API 请求失败: %v", err)
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("API 返回了空内容")
	}

	fmt.Printf("LLM Test Response: %s\n", content)
	return nil
}

func requireProviderModelName(provider models.LLMProvider) (string, error) {
	model := strings.TrimSpace(provider.ModelName)
	if model == "" {
		return "", fmt.Errorf("LLM 模型名称未配置")
	}
	return model, nil
}
