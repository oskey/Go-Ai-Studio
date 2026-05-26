package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"

	"github.com/gin-gonic/gin"
)

type queuedImageBatchResult struct {
	QueuedIDs    []uint
	QueuedCount  int
	SkippedCount int
}

func calculateCharacterBatchTimeout(count int) time.Duration {
	if count <= 0 {
		return 60 * time.Second
	}
	perItem := 40 * time.Second
	buffer := 4 * time.Minute
	return time.Duration(count)*perItem + buffer
}

func calculateSceneBatchTimeout(count int) time.Duration {
	if count <= 0 {
		return 60 * time.Second
	}
	perItem := 120 * time.Second
	buffer := 6 * time.Minute
	return time.Duration(count)*perItem + buffer
}

func calculateVideoBatchTimeout(segmentCount int) time.Duration {
	if segmentCount <= 0 {
		return 2 * time.Minute
	}
	perSegment := 10 * time.Minute
	buffer := 5 * time.Minute
	return time.Duration(segmentCount)*perSegment + buffer
}

func queueProjectCharacterImages(projectID uint, taskID string) (*queuedImageBatchResult, error) {
	var characters []models.Character
	if err := db.DB.Where("project_id = ?", projectID).Find(&characters).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch characters: %v", err)
	}

	result := &queuedImageBatchResult{}
	failures := make([]string, 0)

	for i, char := range characters {
		progress := 0
		if len(characters) > 0 {
			progress = int(float64(i) / float64(len(characters)) * 100)
		}
		if taskID != "" {
			task.GlobalTaskManager.UpdateTaskProgress(taskID, progress, fmt.Sprintf("检查人物 %d/%d：%s", i+1, len(characters), char.Name))
		}

		if strings.TrimSpace(char.GeneratedImage) != "" {
			result.SkippedCount++
			continue
		}

		if strings.TrimSpace(char.Appearance) == "" {
			failures = append(failures, fmt.Sprintf("%s: missing appearance", char.Name))
			continue
		}

		promptID, err := triggerCharacterImageGeneration(char)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", char.Name, err))
			continue
		}

		if promptID != "" {
			result.QueuedIDs = append(result.QueuedIDs, char.ID)
			result.QueuedCount++
			if taskID != "" {
				task.GlobalTaskManager.UpdateTaskProgress(taskID, progress, fmt.Sprintf("人物已提交 %d/%d：%s", i+1, len(characters), char.Name))
			}
		}
	}

	if len(failures) > 0 {
		return nil, fmt.Errorf("character image queue failed for %d items: %s", len(failures), strings.Join(failures, " | "))
	}

	return result, nil
}

func queueProjectSceneImages(projectID uint, taskID string) (*queuedImageBatchResult, error) {
	var scenes []models.Scene
	if err := db.DB.Preload("Characters").Where("project_id = ?", projectID).Order("episode asc, scene_number asc").Find(&scenes).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch scenes: %v", err)
	}

	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		return nil, fmt.Errorf("project not found")
	}

	result := &queuedImageBatchResult{}
	failures := make([]string, 0)

	for i, scene := range scenes {
		progress := 0
		if len(scenes) > 0 {
			progress = int(float64(i) / float64(len(scenes)) * 100)
		}
		if taskID != "" {
			task.GlobalTaskManager.UpdateTaskProgress(taskID, progress, fmt.Sprintf("检查场景 %d/%d：%s", i+1, len(scenes), scene.Name))
		}

		if strings.TrimSpace(scene.GeneratedImage) != "" {
			result.SkippedCount++
			continue
		}

		if strings.TrimSpace(scene.ImagePrompt) == "" {
			failures = append(failures, fmt.Sprintf("%s: missing image_prompt", firstNonEmptyString(scene.Name, fmt.Sprintf("第%d集第%d镜", scene.Episode, scene.SceneNumber))))
			continue
		}

		promptID, err := triggerSceneImageGeneration(scene)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", firstNonEmptyString(scene.Name, fmt.Sprintf("第%d集第%d镜", scene.Episode, scene.SceneNumber)), err))
			continue
		}

		go pollSceneImage(promptID, scene.ID, project.Code, taskID)

		if promptID != "" {
			result.QueuedIDs = append(result.QueuedIDs, scene.ID)
			result.QueuedCount++
			if taskID != "" {
				task.GlobalTaskManager.UpdateTaskProgress(taskID, progress, fmt.Sprintf("场景已提交 %d/%d：%s", i+1, len(scenes), scene.Name))
			}
		}
	}

	if len(failures) > 0 {
		return nil, fmt.Errorf("scene image queue failed for %d items: %s", len(failures), strings.Join(failures, " | "))
	}

	return result, nil
}

func waitForCharacterImages(projectID uint, ids []uint, timeout time.Duration, taskID string) error {
	if len(ids) == 0 {
		return nil
	}

	start := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		var characters []models.Character
		if err := db.DB.Where("project_id = ? AND id IN ?", projectID, ids).Find(&characters).Error; err != nil {
			return fmt.Errorf("failed to refresh character status: %v", err)
		}

		remaining := 0
		failed := make([]string, 0)
		for _, char := range characters {
			if strings.TrimSpace(char.GeneratedImage) != "" {
				continue
			}
			if strings.TrimSpace(char.Status) == "failed" {
				failed = append(failed, char.Name)
				continue
			}
			remaining++
		}

		if len(failed) > 0 {
			return fmt.Errorf("character images failed: %s", strings.Join(failed, ", "))
		}
		if remaining == 0 {
			return nil
		}

		if taskID != "" {
			done := len(ids) - remaining
			task.GlobalTaskManager.UpdateTaskProgress(taskID, 45, fmt.Sprintf("等待人物图完成 %d/%d", done, len(ids)))
		}

		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for %d character images", remaining)
		}

		<-ticker.C
	}
}

func waitForSceneImages(projectID uint, ids []uint, timeout time.Duration, taskID string) error {
	if len(ids) == 0 {
		return nil
	}

	start := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		var scenes []models.Scene
		if err := db.DB.Where("project_id = ? AND id IN ?", projectID, ids).Find(&scenes).Error; err != nil {
			return fmt.Errorf("failed to refresh scene status: %v", err)
		}

		remaining := 0
		failed := make([]string, 0)
		for _, scene := range scenes {
			if strings.TrimSpace(scene.GeneratedImage) != "" {
				continue
			}
			if strings.TrimSpace(scene.Status) == "failed" {
				failed = append(failed, scene.Name)
				continue
			}
			remaining++
		}

		if len(failed) > 0 {
			return fmt.Errorf("scene images failed: %s", strings.Join(failed, ", "))
		}
		if remaining == 0 {
			return nil
		}

		if taskID != "" {
			done := len(ids) - remaining
			task.GlobalTaskManager.UpdateTaskProgress(taskID, 78, fmt.Sprintf("等待场景图完成 %d/%d", done, len(ids)))
		}

		if time.Since(start) > timeout {
			return fmt.Errorf("timeout waiting for %d scene images", remaining)
		}

		<-ticker.C
	}
}

func collectPendingVideoIDs(projectID uint) ([]uint, int, error) {
	var videos []models.Video
	if err := db.DB.Where("project_id = ? AND video_status = ?", projectID, "pending").Find(&videos).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to fetch pending videos: %v", err)
	}

	ids := make([]uint, 0, len(videos))
	totalSegments := 0
	for _, video := range videos {
		ids = append(ids, video.ID)
		var segmentCount int64
		if err := db.DB.Model(&models.VideoSegment{}).Where("video_id = ?", video.ID).Count(&segmentCount).Error; err != nil {
			return nil, 0, fmt.Errorf("failed to count video segments: %v", err)
		}
		if segmentCount <= 0 {
			totalSegments++
			continue
		}
		totalSegments += int(segmentCount)
	}

	return ids, totalSegments, nil
}

func waitForVideos(projectID uint, ids []uint, timeout time.Duration, taskID string) error {
	if len(ids) == 0 {
		return nil
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		var videos []models.Video
		if err := db.DB.Where("project_id = ? AND id IN ?", projectID, ids).Find(&videos).Error; err != nil {
			return fmt.Errorf("failed to refresh video status: %v", err)
		}

		remaining := 0
		failed := make([]string, 0)
		for _, video := range videos {
			if strings.TrimSpace(video.GeneratedVideo) != "" && strings.TrimSpace(video.Status) == "generated" {
				continue
			}
			if strings.TrimSpace(video.Status) == "failed" {
				failed = append(failed, fmt.Sprintf("video-%d", video.ID))
				continue
			}
			remaining++
		}

		if len(failed) > 0 {
			return fmt.Errorf("videos failed: %s", strings.Join(failed, ", "))
		}
		if remaining == 0 {
			return nil
		}

		if taskID != "" {
			done := len(ids) - remaining
			task.GlobalTaskManager.UpdateTaskProgress(taskID, 96, fmt.Sprintf("等待视频完成 %d/%d", done, len(ids)))
		}

		<-ticker.C
	}
}

func AutoGenerateCharactersAndScenes(c *gin.Context) {
	projectID := c.Param("id")

	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	t, err := task.GlobalTaskManager.AddTask("batch_generate_characters_scenes", map[string]interface{}{
		"project_id": project.ID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to submit task"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Character + scene generation task submitted", "task_id": t.ID})
}

func AutoGenerateAllMedia(c *gin.Context) {
	projectID := c.Param("id")

	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	t, err := task.GlobalTaskManager.AddTask("batch_generate_all_media", map[string]interface{}{
		"project_id": project.ID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to submit task"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Character + scene + video generation task submitted", "task_id": t.ID})
}

func HandleBatchGenerateCharactersAndScenesTask(t *models.Task) (interface{}, error) {
	var payload struct {
		ProjectID uint `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 5, "开始检查并提交人物基础图生成任务")
	charResult, err := queueProjectCharacterImages(payload.ProjectID, t.ID)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 35, "人物任务已提交，等待人物基础图完成")
	if err := waitForCharacterImages(payload.ProjectID, charResult.QueuedIDs, calculateCharacterBatchTimeout(len(charResult.QueuedIDs)), t.ID); err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 60, "人物基础图已就绪，开始提交场景图生成任务")
	sceneResult, err := queueProjectSceneImages(payload.ProjectID, t.ID)
	if err != nil {
		return nil, err
	}

	return fmt.Sprintf("人物+场景任务完成：人物 %d 提交、%d 跳过；场景 %d 提交、%d 跳过。", charResult.QueuedCount, charResult.SkippedCount, sceneResult.QueuedCount, sceneResult.SkippedCount), nil
}

func HandleBatchGenerateAllMediaTask(t *models.Task) (interface{}, error) {
	var payload struct {
		ProjectID uint `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 5, "开始检查并提交人物基础图生成任务")
	charResult, err := queueProjectCharacterImages(payload.ProjectID, t.ID)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 28, "人物任务已提交，等待人物基础图完成")
	if err := waitForCharacterImages(payload.ProjectID, charResult.QueuedIDs, calculateCharacterBatchTimeout(len(charResult.QueuedIDs)), t.ID); err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 48, "人物基础图已完成，开始提交场景图生成任务")
	sceneResult, err := queueProjectSceneImages(payload.ProjectID, t.ID)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 72, "场景任务已提交，等待场景图完成")
	if err := waitForSceneImages(payload.ProjectID, sceneResult.QueuedIDs, calculateSceneBatchTimeout(len(sceneResult.QueuedIDs)), t.ID); err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 90, "场景图已完成，开始提交视频生成任务")
	videoIDs, estimatedSegments, err := collectPendingVideoIDs(payload.ProjectID)
	if err != nil {
		return nil, err
	}
	videoTask, err := task.GlobalTaskManager.AddTask("batch_generate_videos", map[string]interface{}{
		"project_id": payload.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to submit batch video task: %v", err)
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 94, "视频任务已提交，等待视频生成完成")
	if err := waitForVideos(payload.ProjectID, videoIDs, calculateVideoBatchTimeout(estimatedSegments), t.ID); err != nil {
		return nil, err
	}

	return fmt.Sprintf("人物+场景+视频任务已完成：人物 %d 提交、%d 跳过；场景 %d 提交、%d 跳过；视频 %d 条已完成（视频任务 TaskID: %s）。", charResult.QueuedCount, charResult.SkippedCount, sceneResult.QueuedCount, sceneResult.SkippedCount, len(videoIDs), videoTask.ID), nil
}
