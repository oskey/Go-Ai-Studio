package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"

	"github.com/gin-gonic/gin"
)

type storeVisitProjectBatchTaskPayload struct {
	ProjectID uint `json:"project_id"`
}

func StartStoreVisitProjectGenerateAllImages(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "补齐探店区域失败"})
		return
	}

	if hasRunning, err := storeVisitProjectHasRunningGeneration(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查项目生成状态失败"})
		return
	} else if hasRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "项目仍在生成中，请等待完成后再操作"})
		return
	}

	payload := storeVisitProjectBatchTaskPayload{ProjectID: project.ID}
	taskRecord, err := task.GlobalTaskManager.AddTask("batch_generate_store_visit_project_images", payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交批量图片生成任务失败"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "批量图片生成任务已提交",
		"task_id": taskRecord.ID,
	})
}

func StartStoreVisitProjectGenerateAllVideos(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "补齐探店区域失败"})
		return
	}

	if hasRunning, err := storeVisitProjectHasRunningGeneration(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查项目生成状态失败"})
		return
	} else if hasRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "项目仍在生成中，请等待完成后再操作"})
		return
	}

	payload := storeVisitProjectBatchTaskPayload{ProjectID: project.ID}
	taskRecord, err := task.GlobalTaskManager.AddTask("batch_generate_store_visit_project_videos", payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交批量视频生成任务失败"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "批量视频生成任务已提交",
		"task_id": taskRecord.ID,
	})
}

func ResetStoreVisitProjectAllImages(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "补齐探店区域失败"})
		return
	}

	spots, err := listStoreVisitSpotsForProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取探店区域失败"})
		return
	}

	resetSpots := 0
	for i := range spots {
		spot := &spots[i]
		spotType := normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
		if isDeprecatedStoreVisitSpotType(spotType) || spotType == storeVisitSpotTypeDishGeneration {
			continue
		}
		if err := resetStoreVisitSpotImageState(spot); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("重置%s图片失败", getStoreVisitSpotDisplayName(*spot))})
			return
		}
		resetSpots++
		BroadcastUpdate("store_visit_spot", spot.ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "项目图片状态已重置",
		"reset_spots": resetSpots,
	})
}

func ResetStoreVisitProjectAllVideos(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "补齐探店区域失败"})
		return
	}

	spots, err := listStoreVisitSpotsForProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取探店区域失败"})
		return
	}

	resetSpots := 0
	for i := range spots {
		spot := &spots[i]
		spotType := normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
		if isDeprecatedStoreVisitSpotType(spotType) || spotType == storeVisitSpotTypeDishGeneration {
			continue
		}

		if err := resetStoreVisitSpotVideoState(spot); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("重置%s视频失败", getStoreVisitSpotDisplayName(*spot))})
			return
		}
		resetSpots++
		BroadcastUpdate("store_visit_spot", spot.ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "项目视频状态已重置",
		"reset_spots": resetSpots,
	})
}

func ResetStoreVisitProjectAllStates(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "补齐探店区域失败"})
		return
	}

	spots, err := listStoreVisitSpotsForProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取探店区域失败"})
		return
	}

	resetSpots := 0
	resetDishItems := 0
	for i := range spots {
		spot := &spots[i]
		spotType := normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
		if isDeprecatedStoreVisitSpotType(spotType) {
			continue
		}

		if spotType == storeVisitSpotTypeDishGeneration {
			items, err := listStoreVisitDishGenerationItemsBySpot(spot.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取%s条目失败", getStoreVisitSpotDisplayName(*spot))})
				return
			}
			for _, item := range items {
				itemCopy := item
				if err := resetStoreVisitDishGenerationItemState(&itemCopy); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("重置%s条目失败", getStoreVisitSpotDisplayName(*spot))})
					return
				}
				resetDishItems++
			}
		}

		if err := resetStoreVisitSpotAssetsAndState(spot); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("重置%s状态失败", getStoreVisitSpotDisplayName(*spot))})
			return
		}
		resetSpots++
		BroadcastUpdate("store_visit_spot", spot.ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "项目全部状态已重置",
		"reset_spots":      resetSpots,
		"reset_dish_items": resetDishItems,
	})
}

func HandleBatchGenerateStoreVisitProjectImagesTask(t *models.Task) (interface{}, error) {
	var payload storeVisitProjectBatchTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	project, spots, err := loadStoreVisitProjectBatchContext(payload.ProjectID)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 12, "批量提交图片生成到 ComfyUI 队列")
	imageSummary, err := queueAndRenderStoreVisitProjectImages(t.ID, project, spots)
	if err != nil {
		return nil, err
	}
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 100, "项目全部图片生成完成")
	return gin.H{
		"project_id":     project.ID,
		"image_summary":  imageSummary,
		"video_summary":  nil,
		"dish_summary":   nil,
		"generated_type": "images",
	}, nil
}

func HandleBatchGenerateStoreVisitProjectVideosTask(t *models.Task) (interface{}, error) {
	var payload storeVisitProjectBatchTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	project, spots, err := loadStoreVisitProjectBatchContext(payload.ProjectID)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 18, "批量提交视频生成到 ComfyUI 队列")
	videoSummary, err := queueAndRenderStoreVisitProjectVideos(t.ID, project, spots)
	if err != nil {
		return nil, err
	}

	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		return nil, err
	}
	spots, err = listStoreVisitSpotsForProject(project.ID)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 72, "菜品生成最后批量执行")
	dishSummary, err := queueAndRenderStoreVisitProjectDishVideos(t.ID, project, spots)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 100, "项目全部视频生成完成")
	return gin.H{
		"project_id":     project.ID,
		"image_summary":  nil,
		"video_summary":  videoSummary,
		"dish_summary":   dishSummary,
		"generated_type": "videos",
	}, nil
}

func loadStoreVisitProjectBatchContext(projectID uint) (models.StoreVisitProject, []models.StoreVisitSpot, error) {
	var project models.StoreVisitProject
	if err := db.DB.First(&project, projectID).Error; err != nil {
		return project, nil, fmt.Errorf("博主探店项目不存在")
	}
	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		return project, nil, err
	}
	spots, err := listStoreVisitSpotsForProject(project.ID)
	if err != nil {
		return project, nil, err
	}
	return project, spots, nil
}
