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

func listSelectedGeneralGuideTags(ids []uint) ([]models.GeneralGuideTag, error) {
	normalized := normalizeAutoGenerateTagIDs(ids)
	if len(normalized) == 0 {
		return []models.GeneralGuideTag{}, nil
	}
	var tags []models.GeneralGuideTag
	if err := db.DB.
		Where("id IN ?", normalized).
		Order("sort_order ASC, id ASC").
		Find(&tags).Error; err != nil {
		return nil, err
	}
	return tags, nil
}

func buildSelectedGeneralGuideTagRulesBlock(tags []models.GeneralGuideTag) string {
	if len(tags) == 0 {
		return ""
	}
	sections := make([]string, 0, len(tags))
	for _, tag := range tags {
		name := strings.TrimSpace(tag.Name)
		rules := strings.TrimSpace(tag.Rules)
		if name == "" || rules == "" {
			continue
		}
		sections = append(sections, fmt.Sprintf("【%s补充规则】\n%s", name, rules))
	}
	if len(sections) == 0 {
		return ""
	}
	return "【补充规则】\n" + strings.Join(sections, "\n\n")
}

func ListGeneralGuideTags(c *gin.Context) {
	var tags []models.GeneralGuideTag
	if err := db.DB.Order("sort_order ASC, id ASC").Find(&tags).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch general guide tags"})
		return
	}
	c.JSON(http.StatusOK, tags)
}

func AddGeneralGuideTag(c *gin.Context) {
	var tag models.GeneralGuideTag
	if err := c.ShouldBindJSON(&tag); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tag.Name = strings.TrimSpace(tag.Name)
	tag.Slug = strings.TrimSpace(tag.Slug)
	tag.Description = strings.TrimSpace(tag.Description)
	tag.Rules = strings.TrimSpace(tag.Rules)
	if tag.Name == "" || tag.Description == "" || tag.Rules == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name, description and rules are required"})
		return
	}
	if tag.Slug == "" {
		tag.Slug = sanitizeAutoGenerateTagSlug(tag.Name)
	}
	tag.CreatedAt = time.Now()
	tag.UpdatedAt = time.Now()
	if err := db.DB.Create(&tag).Error; err != nil {
		Log(LogLevelError, "创建讲解标签失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create general guide tag"})
		return
	}
	Log(LogLevelInfo, "创建讲解标签", fmt.Sprintf("创建了新标签: %s", tag.Name))
	c.JSON(http.StatusCreated, tag)
}

func UpdateGeneralGuideTag(c *gin.Context) {
	id := c.Param("id")
	var tag models.GeneralGuideTag
	if err := db.DB.First(&tag, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "General guide tag not found"})
		return
	}

	var updateData models.GeneralGuideTag
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tag.Name = strings.TrimSpace(updateData.Name)
	tag.Description = strings.TrimSpace(updateData.Description)
	tag.Rules = strings.TrimSpace(updateData.Rules)
	tag.SortOrder = updateData.SortOrder
	slug := strings.TrimSpace(updateData.Slug)
	if slug == "" {
		slug = sanitizeAutoGenerateTagSlug(tag.Name)
	}
	tag.Slug = slug
	tag.UpdatedAt = time.Now()

	if tag.Name == "" || tag.Description == "" || tag.Rules == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name, description and rules are required"})
		return
	}

	if err := db.DB.Save(&tag).Error; err != nil {
		Log(LogLevelError, "更新讲解标签失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update general guide tag"})
		return
	}

	Log(LogLevelInfo, "更新讲解标签", fmt.Sprintf("更新了标签: %s", tag.Name))
	c.JSON(http.StatusOK, tag)
}

func DeleteGeneralGuideTag(c *gin.Context) {
	id := c.Param("id")
	if err := db.DB.Delete(&models.GeneralGuideTag{}, id).Error; err != nil {
		Log(LogLevelError, "删除讲解标签失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete general guide tag"})
		return
	}
	Log(LogLevelInfo, "删除讲解标签", fmt.Sprintf("删除标签 ID: %s", id))
	c.JSON(http.StatusOK, gin.H{"message": "General guide tag deleted successfully"})
}
