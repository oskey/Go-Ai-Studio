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
	"gorm.io/gorm"
)

type generalGuideBatchVideoSizeRequest struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type generalGuideProjectBatchTaskPayload struct {
	ProjectID        uint `json:"project_id"`
	UseLightningLoRA bool `json:"use_lightning_lora"`
}

type generalGuideProjectBatchImageGenerateRequest struct {
	UseLightningLoRA *bool `json:"use_lightning_lora"`
}

type generalGuideProjectBatchSummary struct {
	Queued        int      `json:"queued"`
	Skipped       int      `json:"skipped"`
	SkippedTitles []string `json:"skipped_titles,omitempty"`
}

func listGeneralGuideScenesForProject(projectID uint) ([]models.GeneralGuideScene, error) {
	var scenes []models.GeneralGuideScene
	if err := db.DB.Where("project_id = ?", projectID).Order("sort_order asc, id asc").Find(&scenes).Error; err != nil {
		return nil, err
	}
	return scenes, nil
}

func listGeneralGuideTransitionsForProject(projectID uint) ([]models.GeneralGuideTransition, error) {
	var transitions []models.GeneralGuideTransition
	if err := db.DB.Where("project_id = ?", projectID).Order("from_sort_order asc, to_sort_order asc, id asc").Find(&transitions).Error; err != nil {
		return nil, err
	}
	return transitions, nil
}

func generalGuideProjectHasRunningGeneration(projectID uint) (bool, error) {
	var runningCount int64
	if err := db.DB.Model(&models.GeneralGuideScene{}).
		Where("project_id = ? AND (image_status = ? OR video_status = ?)", projectID, "generating", "generating").
		Count(&runningCount).Error; err != nil {
		return false, err
	}
	if runningCount > 0 {
		return true, nil
	}
	if err := db.DB.Model(&models.GeneralGuideTransition{}).
		Where("project_id = ? AND video_status = ?", projectID, "generating").
		Count(&runningCount).Error; err != nil {
		return false, err
	}
	if runningCount > 0 {
		return true, nil
	}

	projectMarker := fmt.Sprintf("%%\"project_id\":%d%%", projectID)
	if err := db.DB.Model(&models.Task{}).
		Where("status IN ? AND type IN ? AND payload LIKE ?",
			[]string{"pending", "running"},
			[]string{
				"batch_generate_general_guide_project_images",
				"batch_generate_general_guide_project_videos",
				"batch_generate_general_guide_project_transitions",
				"batch_generate_general_guide_project_images_and_videos",
				"batch_generate_general_guide_project_images_videos_and_transitions",
			},
			projectMarker,
		).
		Count(&runningCount).Error; err != nil {
		return false, err
	}
	return runningCount > 0, nil
}

func maxGeneralGuideBatchInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func calculateGeneralGuideBatchTimeout(itemCount int, perItem time.Duration, min time.Duration) time.Duration {
	if itemCount <= 0 {
		return min
	}
	timeout := time.Duration(itemCount) * perItem
	if timeout < min {
		return min
	}
	return timeout
}

func generalGuideSceneDisplayName(scene models.GeneralGuideScene) string {
	title := strings.TrimSpace(scene.Title)
	if title != "" {
		return title
	}
	return fmt.Sprintf("第%d行", scene.SortOrder)
}

func generalGuideTransitionDisplayName(transition models.GeneralGuideTransition) string {
	return fmt.Sprintf("转场 %d→%d", transition.FromSortOrder, transition.ToSortOrder)
}

func canQueueGeneralGuideSceneImage(scene models.GeneralGuideScene, project models.GeneralGuideProject) bool {
	if strings.TrimSpace(scene.GeneratedImage) != "" {
		return false
	}
	if strings.TrimSpace(scene.ReferenceImage) == "" {
		return false
	}
	if generalGuideDefaultNeedPresenter(scene.ImagePreset, scene.SceneType) &&
		normalizeGeneralGuideImagePreset(scene.ImagePreset) != generalGuideImagePresetMaterialOnly &&
		strings.TrimSpace(project.PresenterReferenceImage) == "" {
		return false
	}
	return true
}

func canQueueGeneralGuideSceneVideo(scene models.GeneralGuideScene) bool {
	if strings.TrimSpace(scene.GeneratedVideo) != "" {
		return false
	}
	if strings.TrimSpace(scene.VideoPositivePrompt) == "" {
		return false
	}
	if normalizeGeneralGuideImagePreset(scene.ImagePreset) != generalGuideImagePresetMaterialOnly &&
		scene.NeedPresenter &&
		strings.TrimSpace(scene.GeneratedImage) == "" {
		return false
	}
	return strings.TrimSpace(scene.GeneratedImage) != "" || strings.TrimSpace(scene.ReferenceImage) != ""
}

func canQueueGeneralGuideTransitionVideo(transition models.GeneralGuideTransition, sceneByID map[uint]models.GeneralGuideScene) bool {
	if strings.TrimSpace(transition.GeneratedVideo) != "" {
		return false
	}
	if strings.TrimSpace(transition.TransitionPrompt) == "" {
		return false
	}
	fromScene, ok := sceneByID[transition.FromSceneID]
	if !ok || strings.TrimSpace(fromScene.GeneratedVideo) == "" {
		return false
	}
	toScene, ok := sceneByID[transition.ToSceneID]
	if !ok || strings.TrimSpace(generalGuideTransitionNextImage(toScene)) == "" {
		return false
	}
	return true
}

func generalGuideTransitionIsReadyForBatch(transition models.GeneralGuideTransition, sceneByID map[uint]models.GeneralGuideScene) bool {
	if strings.TrimSpace(transition.GeneratedVideo) != "" {
		return true
	}
	return canQueueGeneralGuideTransitionVideo(transition, sceneByID)
}

func collectGeneralGuideTransitionBatchBlockers(transitions []models.GeneralGuideTransition, sceneByID map[uint]models.GeneralGuideScene) []string {
	blocked := make([]string, 0)
	for _, transition := range transitions {
		if generalGuideTransitionIsReadyForBatch(transition, sceneByID) {
			continue
		}
		blocked = append(blocked, generalGuideTransitionDisplayName(transition))
	}
	return blocked
}

func ensureGeneralGuideTransitionTailFramePrepared(project models.GeneralGuideProject, transition models.GeneralGuideTransition, fromScene models.GeneralGuideScene) (models.GeneralGuideTransition, error) {
	engine := getConfiguredGeneralGuideTransitionEngine()
	framesFromEnd := sanitizeGeneralGuideTransitionFramesFromEndForEngine(transition.FramesFromEnd, engine)
	currentTail := strings.TrimSpace(transition.TailFrameImage)
	currentSource := strings.TrimSpace(transition.TailFrameSourceVideo)
	currentVideo := strings.TrimSpace(fromScene.GeneratedVideo)
	if normalizeGeneralGuideTransitionEngine(engine) != GeneralGuideTransitionEngineFFmpeg && currentTail != "" && currentSource == currentVideo {
		return transition, nil
	}
	webPath, err := extractGeneralGuideTransitionTailFrameAsset(project, transition, fromScene.GeneratedVideo, framesFromEnd)
	if err != nil {
		return transition, err
	}
	if currentTail != "" && currentTail != webPath {
		if err := removeGeneralGuideAsset(currentTail); err != nil {
			return transition, err
		}
	}
	if err := db.DB.Model(&models.GeneralGuideTransition{}).Where("id = ?", transition.ID).Updates(map[string]interface{}{
		"frames_from_end":         framesFromEnd,
		"tail_frame_image":        webPath,
		"tail_frame_source_video": fromScene.GeneratedVideo,
		"updated_at":              time.Now(),
	}).Error; err != nil {
		return transition, err
	}
	transition.FramesFromEnd = framesFromEnd
	transition.TailFrameImage = webPath
	transition.TailFrameSourceVideo = fromScene.GeneratedVideo
	transition.UpdatedAt = time.Now()
	return transition, nil
}

func waitForGeneralGuideSceneImages(projectID uint, ids []uint, timeout time.Duration, taskID string, progressBase int, progressSpan int) error {
	if len(ids) == 0 {
		return nil
	}
	start := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		var scenes []models.GeneralGuideScene
		if err := db.DB.Where("project_id = ? AND id IN ?", projectID, ids).Find(&scenes).Error; err != nil {
			return fmt.Errorf("刷新综合讲解图片状态失败: %w", err)
		}
		remaining := 0
		failed := make([]string, 0)
		for _, scene := range scenes {
			if strings.TrimSpace(scene.GeneratedImage) != "" {
				continue
			}
			if strings.TrimSpace(scene.ImageStatus) == "failed" {
				failed = append(failed, generalGuideSceneDisplayName(scene))
				continue
			}
			remaining++
		}
		if len(failed) > 0 {
			return fmt.Errorf("以下行图片生成失败：%s", strings.Join(failed, "、"))
		}
		if remaining == 0 {
			return nil
		}
		done := len(ids) - remaining
		progress := progressBase + int(float64(done)/float64(maxGeneralGuideBatchInt(1, len(ids)))*float64(progressSpan))
		task.GlobalTaskManager.UpdateTaskProgress(taskID, progress, fmt.Sprintf("等待图片生成 %d/%d", done, len(ids)))
		if time.Since(start) > timeout {
			return fmt.Errorf("等待图片生成超时，仍有 %d 行未完成", remaining)
		}
		<-ticker.C
	}
}

func waitForGeneralGuideSceneVideos(projectID uint, ids []uint, timeout time.Duration, taskID string, progressBase int, progressSpan int) error {
	if len(ids) == 0 {
		return nil
	}
	start := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		var scenes []models.GeneralGuideScene
		if err := db.DB.Where("project_id = ? AND id IN ?", projectID, ids).Find(&scenes).Error; err != nil {
			return fmt.Errorf("刷新综合讲解视频状态失败: %w", err)
		}
		remaining := 0
		failed := make([]string, 0)
		for _, scene := range scenes {
			if strings.TrimSpace(scene.GeneratedVideo) != "" {
				continue
			}
			if strings.TrimSpace(scene.VideoStatus) == "failed" {
				failed = append(failed, generalGuideSceneDisplayName(scene))
				continue
			}
			remaining++
		}
		if len(failed) > 0 {
			return fmt.Errorf("以下行视频生成失败：%s", strings.Join(failed, "、"))
		}
		if remaining == 0 {
			return nil
		}
		done := len(ids) - remaining
		progress := progressBase + int(float64(done)/float64(maxGeneralGuideBatchInt(1, len(ids)))*float64(progressSpan))
		task.GlobalTaskManager.UpdateTaskProgress(taskID, progress, fmt.Sprintf("等待视频生成 %d/%d", done, len(ids)))
		if time.Since(start) > timeout {
			return fmt.Errorf("等待视频生成超时，仍有 %d 行未完成", remaining)
		}
		<-ticker.C
	}
}

func waitForGeneralGuideTransitions(projectID uint, ids []uint, timeout time.Duration, taskID string, progressBase int, progressSpan int) error {
	if len(ids) == 0 {
		return nil
	}
	start := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		var transitions []models.GeneralGuideTransition
		if err := db.DB.Where("project_id = ? AND id IN ?", projectID, ids).Find(&transitions).Error; err != nil {
			return fmt.Errorf("刷新综合讲解转场状态失败: %w", err)
		}
		remaining := 0
		failed := make([]string, 0)
		for _, transition := range transitions {
			if strings.TrimSpace(transition.GeneratedVideo) != "" {
				continue
			}
			if strings.TrimSpace(transition.VideoStatus) == "failed" {
				failed = append(failed, generalGuideTransitionDisplayName(transition))
				continue
			}
			remaining++
		}
		if len(failed) > 0 {
			return fmt.Errorf("以下转场生成失败：%s", strings.Join(failed, "、"))
		}
		if remaining == 0 {
			return nil
		}
		done := len(ids) - remaining
		progress := progressBase + int(float64(done)/float64(maxGeneralGuideBatchInt(1, len(ids)))*float64(progressSpan))
		task.GlobalTaskManager.UpdateTaskProgress(taskID, progress, fmt.Sprintf("等待转场生成 %d/%d", done, len(ids)))
		if time.Since(start) > timeout {
			return fmt.Errorf("等待转场生成超时，仍有 %d 条未完成", remaining)
		}
		<-ticker.C
	}
}

func queueAndRenderGeneralGuideProjectImages(taskID string, project models.GeneralGuideProject, scenes []models.GeneralGuideScene, useLightningLoRA bool) (generalGuideProjectBatchSummary, error) {
	summary := generalGuideProjectBatchSummary{}
	queuedIDs := make([]uint, 0)
	for _, scene := range scenes {
		if !canQueueGeneralGuideSceneImage(scene, project) {
			summary.Skipped++
			summary.SkippedTitles = append(summary.SkippedTitles, generalGuideSceneDisplayName(scene))
			continue
		}
		if normalizeGeneralGuideImagePreset(scene.ImagePreset) == generalGuideImagePresetMaterialOnly || !scene.NeedPresenter {
			if strings.TrimSpace(scene.GeneratedImage) != "" {
				if err := removeGeneralGuideAsset(scene.GeneratedImage); err != nil {
					return summary, err
				}
			}
			webPath, err := copyGeneralGuideReferenceAsGeneratedImage(scene, project)
			if err != nil {
				return summary, err
			}
			if err := db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
				"generated_image":          webPath,
				"image_status":             "generated",
				"image_current_task_id":    "",
				"image_last_error":         "",
				"image_generated_workflow": "纯素材直出",
				"updated_at":               time.Now(),
			}).Error; err != nil {
				return summary, err
			}
			summary.Queued++
			continue
		}
		if _, err := startGeneralGuideSceneImageTask(&scene, &project, getConfiguredGlobalSeed(), useLightningLoRA); err != nil {
			return summary, err
		}
		queuedIDs = append(queuedIDs, scene.ID)
	}
	summary.Queued += len(queuedIDs)
	if err := waitForGeneralGuideSceneImages(project.ID, queuedIDs, calculateGeneralGuideBatchTimeout(len(queuedIDs), 10*time.Minute, 5*time.Minute), taskID, 28, 60); err != nil {
		return summary, err
	}
	return summary, nil
}

func queueAndRenderGeneralGuideProjectVideos(taskID string, project models.GeneralGuideProject, scenes []models.GeneralGuideScene) (generalGuideProjectBatchSummary, error) {
	summary := generalGuideProjectBatchSummary{}
	queuedIDs := make([]uint, 0)
	for _, scene := range scenes {
		if !canQueueGeneralGuideSceneVideo(scene) {
			summary.Skipped++
			summary.SkippedTitles = append(summary.SkippedTitles, generalGuideSceneDisplayName(scene))
			continue
		}
		if _, err := startGeneralGuideSceneVideoTask(&scene, &project, getConfiguredGlobalSeed()); err != nil {
			return summary, err
		}
		queuedIDs = append(queuedIDs, scene.ID)
	}
	summary.Queued = len(queuedIDs)
	if err := waitForGeneralGuideSceneVideos(project.ID, queuedIDs, calculateGeneralGuideBatchTimeout(len(queuedIDs), 14*time.Minute, 5*time.Minute), taskID, 28, 60); err != nil {
		return summary, err
	}
	return summary, nil
}

func queueAndRenderGeneralGuideProjectTransitions(taskID string, project models.GeneralGuideProject, scenes []models.GeneralGuideScene, transitions []models.GeneralGuideTransition) (generalGuideProjectBatchSummary, error) {
	summary := generalGuideProjectBatchSummary{}
	sceneByID := make(map[uint]models.GeneralGuideScene, len(scenes))
	for _, scene := range scenes {
		sceneByID[scene.ID] = scene
	}
	queuedIDs := make([]uint, 0)
	for _, transition := range transitions {
		if !canQueueGeneralGuideTransitionVideo(transition, sceneByID) {
			summary.Skipped++
			summary.SkippedTitles = append(summary.SkippedTitles, generalGuideTransitionDisplayName(transition))
			continue
		}
		fromScene := sceneByID[transition.FromSceneID]
		preparedTransition, err := ensureGeneralGuideTransitionTailFramePrepared(project, transition, fromScene)
		if err != nil {
			return summary, err
		}
		transition = preparedTransition
		if _, err := startGeneralGuideTransitionVideoTask(&transition, &project); err != nil {
			return summary, err
		}
		queuedIDs = append(queuedIDs, transition.ID)
	}
	summary.Queued = len(queuedIDs)
	if err := waitForGeneralGuideTransitions(project.ID, queuedIDs, calculateGeneralGuideBatchTimeout(len(queuedIDs), 8*time.Minute, 4*time.Minute), taskID, 28, 60); err != nil {
		return summary, err
	}
	return summary, nil
}

func resetGeneralGuideProjectAssetsAndState(projectID uint) (int, int, error) {
	var scenes []models.GeneralGuideScene
	if err := db.DB.Where("project_id = ?", projectID).Order("sort_order asc, id asc").Find(&scenes).Error; err != nil {
		return 0, 0, err
	}
	var transitions []models.GeneralGuideTransition
	if err := db.DB.Where("project_id = ?", projectID).Order("from_sort_order asc, to_sort_order asc, id asc").Find(&transitions).Error; err != nil {
		return 0, 0, err
	}

	for _, scene := range scenes {
		if err := removeGeneralGuideAsset(scene.ReferenceImage); err != nil {
			return 0, 0, err
		}
		if err := removeGeneralGuideAsset(scene.GeneratedImage); err != nil {
			return 0, 0, err
		}
		if err := removeGeneralGuideAsset(scene.GeneratedVideo); err != nil {
			return 0, 0, err
		}
	}
	for _, transition := range transitions {
		if err := removeGeneralGuideAsset(transition.TailFrameImage); err != nil {
			return 0, 0, err
		}
		if err := removeGeneralGuideAsset(transition.GeneratedVideo); err != nil {
			return 0, 0, err
		}
	}

	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", projectID).Delete(&models.GeneralGuideTransition{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", projectID).Delete(&models.GeneralGuideScene{}).Error; err != nil {
			return err
		}
		return tx.Model(&models.GeneralGuideProject{}).Where("id = ?", projectID).Updates(map[string]interface{}{
			"current_planning_task_id": "",
			"last_planning_error":      "",
			"updated_at":               time.Now(),
		}).Error
	}); err != nil {
		return 0, 0, err
	}

	return len(scenes), len(transitions), nil
}

func resetGeneralGuideProjectGeneratedMediaAndStatuses(projectID uint) (int, int, error) {
	var scenes []models.GeneralGuideScene
	if err := db.DB.Where("project_id = ?", projectID).Order("sort_order asc, id asc").Find(&scenes).Error; err != nil {
		return 0, 0, err
	}
	var transitions []models.GeneralGuideTransition
	if err := db.DB.Where("project_id = ?", projectID).Order("from_sort_order asc, to_sort_order asc, id asc").Find(&transitions).Error; err != nil {
		return 0, 0, err
	}

	for _, scene := range scenes {
		if err := removeGeneralGuideAsset(scene.GeneratedImage); err != nil {
			return 0, 0, err
		}
		if err := removeGeneralGuideAsset(scene.GeneratedVideo); err != nil {
			return 0, 0, err
		}
	}
	for _, transition := range transitions {
		if err := removeGeneralGuideAsset(transition.TailFrameImage); err != nil {
			return 0, 0, err
		}
		if err := removeGeneralGuideAsset(transition.GeneratedVideo); err != nil {
			return 0, 0, err
		}
	}

	now := time.Now()
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.GeneralGuideScene{}).Where("project_id = ?", projectID).Updates(map[string]interface{}{
			"image_status":             "draft",
			"image_current_task_id":    "",
			"image_last_error":         "",
			"generated_image":          "",
			"image_generated_workflow": "",
			"video_status":             "draft",
			"video_current_task_id":    "",
			"video_last_error":         "",
			"generated_video":          "",
			"video_generated_workflow": "",
			"updated_at":               now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.GeneralGuideTransition{}).Where("project_id = ?", projectID).Updates(map[string]interface{}{
			"tail_frame_image":         "",
			"tail_frame_source_video":  "",
			"video_status":             "draft",
			"video_current_task_id":    "",
			"video_last_error":         "",
			"generated_video":          "",
			"video_generated_workflow": "",
			"updated_at":               now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&models.GeneralGuideProject{}).Where("id = ?", projectID).Updates(map[string]interface{}{
			"current_planning_task_id": "",
			"last_planning_error":      "",
			"updated_at":               now,
		}).Error
	}); err != nil {
		return 0, 0, err
	}

	return len(scenes), len(transitions), nil
}

func StartGeneralGuideProjectGenerateAllImages(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	if hasRunning, err := generalGuideProjectHasRunningGeneration(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查综合讲解项目生成状态失败"})
		return
	} else if hasRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "当前仍有综合讲解内容在生成中，请等待完成后再操作"})
		return
	}
	scenes, err := listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取综合讲解场景失败"})
		return
	}
	runnable := 0
	for _, scene := range scenes {
		if canQueueGeneralGuideSceneImage(scene, *project) {
			runnable++
		}
	}
	if runnable == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前没有可批量生成的图片"})
		return
	}
	var req generalGuideProjectBatchImageGenerateRequest
	_ = c.ShouldBindJSON(&req)
	useLightningLoRA := false
	if req.UseLightningLoRA != nil {
		useLightningLoRA = *req.UseLightningLoRA
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("batch_generate_general_guide_project_images", generalGuideProjectBatchTaskPayload{
		ProjectID:        project.ID,
		UseLightningLoRA: useLightningLoRA,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交综合讲解批量图片任务失败"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message":            "综合讲解批量图片任务已提交",
		"task_id":            taskRecord.ID,
		"use_lightning_lora": useLightningLoRA,
	})
}

func StartGeneralGuideProjectGenerateAllVideos(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	if hasRunning, err := generalGuideProjectHasRunningGeneration(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查综合讲解项目生成状态失败"})
		return
	} else if hasRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "当前仍有综合讲解内容在生成中，请等待完成后再操作"})
		return
	}
	scenes, err := listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取综合讲解场景失败"})
		return
	}
	runnable := 0
	for _, scene := range scenes {
		if canQueueGeneralGuideSceneVideo(scene) {
			runnable++
		}
	}
	if runnable == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前没有可批量生成的视频"})
		return
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("batch_generate_general_guide_project_videos", generalGuideProjectBatchTaskPayload{ProjectID: project.ID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交综合讲解批量视频任务失败"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message": "综合讲解批量视频任务已提交",
		"task_id": taskRecord.ID,
	})
}

func StartGeneralGuideProjectGenerateAllTransitions(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	if hasRunning, err := generalGuideProjectHasRunningGeneration(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查综合讲解项目生成状态失败"})
		return
	} else if hasRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "当前仍有综合讲解内容在生成中，请等待完成后再操作"})
		return
	}
	scenes, err := listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取综合讲解场景失败"})
		return
	}
	transitions, err := listGeneralGuideTransitionsForProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取综合讲解转场失败"})
		return
	}
	sceneByID := make(map[uint]models.GeneralGuideScene, len(scenes))
	for _, scene := range scenes {
		sceneByID[scene.ID] = scene
	}
	blocked := collectGeneralGuideTransitionBatchBlockers(transitions, sceneByID)
	if len(blocked) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("仍有转场不满足批量生成条件：%s", strings.Join(blocked, "、"))})
		return
	}
	runnable := 0
	for _, transition := range transitions {
		if canQueueGeneralGuideTransitionVideo(transition, sceneByID) {
			runnable++
		}
	}
	if runnable == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前没有可批量生成的转场"})
		return
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("batch_generate_general_guide_project_transitions", generalGuideProjectBatchTaskPayload{ProjectID: project.ID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交综合讲解批量转场任务失败"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message": "综合讲解批量转场任务已提交",
		"task_id": taskRecord.ID,
	})
}

func StartGeneralGuideProjectGenerateAllImagesVideosAndTransitions(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	if hasRunning, err := generalGuideProjectHasRunningGeneration(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查综合讲解项目生成状态失败"})
		return
	} else if hasRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "当前仍有综合讲解内容在生成中，请等待完成后再操作"})
		return
	}
	scenes, err := listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取综合讲解场景失败"})
		return
	}
	transitions, err := listGeneralGuideTransitionsForProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取综合讲解转场失败"})
		return
	}

	imageRunnable := 0
	videoRunnable := 0
	for _, scene := range scenes {
		if canQueueGeneralGuideSceneImage(scene, *project) {
			imageRunnable++
		}
		if canQueueGeneralGuideSceneVideo(scene) {
			videoRunnable++
		}
	}
	if imageRunnable == 0 && videoRunnable == 0 && len(transitions) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前没有可批量生成的图片、视频或转场"})
		return
	}
	var req generalGuideProjectBatchImageGenerateRequest
	_ = c.ShouldBindJSON(&req)
	useLightningLoRA := false
	if req.UseLightningLoRA != nil {
		useLightningLoRA = *req.UseLightningLoRA
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("batch_generate_general_guide_project_images_videos_and_transitions", generalGuideProjectBatchTaskPayload{
		ProjectID:        project.ID,
		UseLightningLoRA: useLightningLoRA,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交综合讲解批量图片、视频和转场任务失败"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message":            "综合讲解批量图片、视频和转场任务已提交",
		"task_id":            taskRecord.ID,
		"use_lightning_lora": useLightningLoRA,
	})
}

func StartGeneralGuideProjectGenerateAllImagesAndVideos(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	if hasRunning, err := generalGuideProjectHasRunningGeneration(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查综合讲解项目生成状态失败"})
		return
	} else if hasRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "当前仍有综合讲解内容在生成中，请等待完成后再操作"})
		return
	}
	scenes, err := listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取综合讲解场景失败"})
		return
	}
	imageRunnable := 0
	videoRunnable := 0
	for _, scene := range scenes {
		if canQueueGeneralGuideSceneImage(scene, *project) {
			imageRunnable++
		}
		if canQueueGeneralGuideSceneVideo(scene) {
			videoRunnable++
		}
	}
	if imageRunnable == 0 && videoRunnable == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前没有可批量生成的图片或视频"})
		return
	}
	var req generalGuideProjectBatchImageGenerateRequest
	_ = c.ShouldBindJSON(&req)
	useLightningLoRA := false
	if req.UseLightningLoRA != nil {
		useLightningLoRA = *req.UseLightningLoRA
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("batch_generate_general_guide_project_images_and_videos", generalGuideProjectBatchTaskPayload{
		ProjectID:        project.ID,
		UseLightningLoRA: useLightningLoRA,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交综合讲解批量图片和视频任务失败"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message":            "综合讲解批量图片和视频任务已提交",
		"task_id":            taskRecord.ID,
		"use_lightning_lora": useLightningLoRA,
	})
}

func ResetGeneralGuideProjectState(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	resetScenes, resetTransitions, err := resetGeneralGuideProjectAssetsAndState(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置综合讲解项目状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":           "已清空本项目所有场景、转场与对应资产",
		"reset_scenes":      resetScenes,
		"reset_transitions": resetTransitions,
	})
}

func ResetGeneralGuideProjectProcessingState(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	resetScenes, resetTransitions, err := resetGeneralGuideProjectGeneratedMediaAndStatuses(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置综合讲解项目状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":           "已重置本项目状态，保留场景行、参考图和文案",
		"reset_scenes":      resetScenes,
		"reset_transitions": resetTransitions,
	})
}

func BatchUpdateGeneralGuideVideoSize(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}

	var req generalGuideBatchVideoSizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式不正确"})
		return
	}
	if req.Width <= 0 || req.Height <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写正确的视频宽高"})
		return
	}

	now := time.Now()
	if err := db.DB.Model(&models.GeneralGuideScene{}).
		Where("project_id = ?", project.ID).
		Updates(map[string]interface{}{
			"video_width":  req.Width,
			"video_height": req.Height,
			"updated_at":   now,
		}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "批量替换视频尺寸失败"})
		return
	}

	var scenes []models.GeneralGuideScene
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&scenes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取更新后的场景失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "已批量替换所有场景的视频尺寸",
		"width":   req.Width,
		"height":  req.Height,
		"count":   len(scenes),
		"scenes":  scenes,
	})
}

func HandleBatchGenerateGeneralGuideProjectImagesTask(t *models.Task) (interface{}, error) {
	var payload generalGuideProjectBatchTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	project, err := loadGeneralGuideProjectOr404FromTask(payload.ProjectID)
	if err != nil {
		return nil, err
	}
	scenes, err := listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		return nil, err
	}
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 12, "批量提交综合讲解图片到 ComfyUI 队列")
	summary, err := queueAndRenderGeneralGuideProjectImages(t.ID, *project, scenes, payload.UseLightningLoRA)
	if err != nil {
		return nil, err
	}
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 100, "综合讲解全部图片生成完成")
	return gin.H{
		"project_id": project.ID,
		"type":       "images",
		"summary":    summary,
	}, nil
}

func HandleBatchGenerateGeneralGuideProjectVideosTask(t *models.Task) (interface{}, error) {
	var payload generalGuideProjectBatchTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	project, err := loadGeneralGuideProjectOr404FromTask(payload.ProjectID)
	if err != nil {
		return nil, err
	}
	scenes, err := listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		return nil, err
	}
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 12, "批量提交综合讲解视频到 ComfyUI 队列")
	summary, err := queueAndRenderGeneralGuideProjectVideos(t.ID, *project, scenes)
	if err != nil {
		return nil, err
	}
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 100, "综合讲解全部视频生成完成")
	return gin.H{
		"project_id": project.ID,
		"type":       "videos",
		"summary":    summary,
	}, nil
}

func HandleBatchGenerateGeneralGuideProjectTransitionsTask(t *models.Task) (interface{}, error) {
	var payload generalGuideProjectBatchTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	project, err := loadGeneralGuideProjectOr404FromTask(payload.ProjectID)
	if err != nil {
		return nil, err
	}
	scenes, err := listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		return nil, err
	}
	transitions, err := listGeneralGuideTransitionsForProject(project.ID)
	if err != nil {
		return nil, err
	}
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 12, "批量提交综合讲解转场到 ComfyUI 队列")
	summary, err := queueAndRenderGeneralGuideProjectTransitions(t.ID, *project, scenes, transitions)
	if err != nil {
		return nil, err
	}
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 100, "综合讲解全部转场生成完成")
	return gin.H{
		"project_id": project.ID,
		"type":       "transitions",
		"summary":    summary,
	}, nil
}

func HandleBatchGenerateGeneralGuideProjectImagesAndVideosTask(t *models.Task) (interface{}, error) {
	var payload generalGuideProjectBatchTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	project, err := loadGeneralGuideProjectOr404FromTask(payload.ProjectID)
	if err != nil {
		return nil, err
	}
	scenes, err := listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 10, "先批量提交综合讲解图片到 ComfyUI 队列")
	imageSummary, err := queueAndRenderGeneralGuideProjectImages(t.ID, *project, scenes, payload.UseLightningLoRA)
	if err != nil {
		return nil, err
	}

	scenes, err = listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		return nil, err
	}
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 62, "全部图片完成，开始批量提交综合讲解视频")
	videoSummary, err := queueAndRenderGeneralGuideProjectVideos(t.ID, *project, scenes)
	if err != nil {
		return nil, err
	}
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 100, "综合讲解图片和视频批量生成完成")
	return gin.H{
		"project_id":    project.ID,
		"type":          "images_and_videos",
		"image_summary": imageSummary,
		"video_summary": videoSummary,
	}, nil
}

func HandleBatchGenerateGeneralGuideProjectImagesVideosAndTransitionsTask(t *models.Task) (interface{}, error) {
	var payload generalGuideProjectBatchTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	project, err := loadGeneralGuideProjectOr404FromTask(payload.ProjectID)
	if err != nil {
		return nil, err
	}
	scenes, err := listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 8, "先批量提交综合讲解图片到 ComfyUI 队列")
	imageSummary, err := queueAndRenderGeneralGuideProjectImages(t.ID, *project, scenes, payload.UseLightningLoRA)
	if err != nil {
		return nil, err
	}

	scenes, err = listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		return nil, err
	}
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 42, "图片完成，开始批量提交综合讲解视频")
	videoSummary, err := queueAndRenderGeneralGuideProjectVideos(t.ID, *project, scenes)
	if err != nil {
		return nil, err
	}

	scenes, err = listGeneralGuideScenesForProject(project.ID)
	if err != nil {
		return nil, err
	}
	transitions, err := listGeneralGuideTransitionsForProject(project.ID)
	if err != nil {
		return nil, err
	}
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 76, "图片和视频完成，开始自动抽尾帧并批量提交转场")
	transitionSummary, err := queueAndRenderGeneralGuideProjectTransitions(t.ID, *project, scenes, transitions)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 100, "综合讲解图片、视频和转场批量生成完成")
	return gin.H{
		"project_id":         project.ID,
		"type":               "images_videos_and_transitions",
		"image_summary":      imageSummary,
		"video_summary":      videoSummary,
		"transition_summary": transitionSummary,
	}, nil
}

func loadGeneralGuideProjectOr404FromTask(projectID uint) (*models.GeneralGuideProject, error) {
	var project models.GeneralGuideProject
	if err := db.DB.First(&project, projectID).Error; err != nil {
		return nil, err
	}
	applyGeneralGuideProjectTagIDs(&project)
	return &project, nil
}
