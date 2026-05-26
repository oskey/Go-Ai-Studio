package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"
	"kt-ai-studio/internal/workflow"

	"github.com/gin-gonic/gin"
)

type generalGuideSceneMediaTaskPayload struct {
	ProjectID        uint  `json:"project_id"`
	SceneID          uint  `json:"scene_id"`
	Seed             int64 `json:"seed"`
	UseLightningLoRA bool  `json:"use_lightning_lora"`
}

type generalGuideSceneImageGenerateRequest struct {
	UseLightningLoRA *bool `json:"use_lightning_lora" form:"use_lightning_lora"`
	RandomSeed       *bool `json:"random_seed" form:"random_seed"`
}

type generalGuideSceneVideoGenerateRequest struct {
	RandomSeed *bool `json:"random_seed" form:"random_seed"`
}

func getGeneralGuideSceneFileKey(scene models.GeneralGuideScene) string {
	return fmt.Sprintf("scene_%02d", scene.SortOrder)
}

func shouldApplyGeneralGuideSceneImageTaskResult(sceneID uint, taskID string) bool {
	var current models.GeneralGuideScene
	if err := db.DB.Select("image_current_task_id").First(&current, sceneID).Error; err != nil {
		return false
	}
	return strings.TrimSpace(current.ImageCurrentTaskID) == strings.TrimSpace(taskID)
}

func shouldApplyGeneralGuideSceneVideoTaskResult(sceneID uint, taskID string) bool {
	var current models.GeneralGuideScene
	if err := db.DB.Select("video_current_task_id").First(&current, sceneID).Error; err != nil {
		return false
	}
	return strings.TrimSpace(current.VideoCurrentTaskID) == strings.TrimSpace(taskID)
}

func buildGeneralGuideImageWorkflow(template map[string]interface{}, sceneImageName string, presenterImageName string, scene models.GeneralGuideScene, project models.GeneralGuideProject, seed int64, useLightningLoRA bool) (map[string]interface{}, string, error) {
	workflowJSON, err := cloneStoreVisitWorkflow(template)
	if err != nil {
		return nil, "", err
	}
	if seed <= 0 {
		seed = getConfiguredGlobalSeed()
	}
	positivePrompt := strings.TrimSpace(scene.ImagePositivePrompt)
	if normalizeGeneralGuideImagePreset(scene.ImagePreset) != generalGuideImagePresetMaterialOnly &&
		normalizeGeneralGuideEnvironmentType(scene.EnvironmentType) == generalGuideEnvironmentOutdoor {
		if normalizeGeneralGuideSceneType(scene.SceneType) == generalGuideSceneTypeClosing {
			if positivePrompt == "" {
				positivePrompt = generalGuideDefaultImagePrompt(scene.ImagePreset, scene.EnvironmentType, scene.SceneType, scene.SortOrder)
			}
		} else if scene.SortOrder == 1 && normalizeGeneralGuideSceneType(scene.SceneType) == generalGuideSceneTypePresenter {
			positivePrompt = buildGeneralGuideOutdoorOpeningPositivePrompt(seed)
		} else {
			positivePrompt = buildGeneralGuideOutdoorPositivePrompt(seed)
		}
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "143", "image", sceneImageName); err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(presenterImageName) != "" {
		if err := setStoreVisitWorkflowInput(workflowJSON, "172", "image", presenterImageName); err != nil {
			return nil, "", err
		}
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "167:118", "prompt", positivePrompt); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "167:117", "prompt", strings.TrimSpace(scene.ImageNegativePrompt)); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "167:130", "seed", seed); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "167:153", "value", useLightningLoRA); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "9", "filename_prefix", fmt.Sprintf("%s_%s_image_%d", project.Code, getGeneralGuideSceneFileKey(scene), scene.ID)); err != nil {
		return nil, "", err
	}
	label := workflowDisplayNameFromPath(generalGuideImageWorkflowPath)
	if useLightningLoRA {
		label += "（Lightning LoRA）"
	} else {
		label += "（No Lightning LoRA）"
	}
	return workflowJSON, label, nil
}

func buildGeneralGuideVideoWorkflow(scene models.GeneralGuideScene, project models.GeneralGuideProject, seed int64) (map[string]interface{}, string, error) {
	data, err := os.ReadFile(storeVisitVideoWorkflowPath)
	if err != nil {
		return nil, "", err
	}
	var workflowJSON map[string]interface{}
	if err := json.Unmarshal(data, &workflowJSON); err != nil {
		return nil, "", err
	}

	meta, err := workflow.ParseWorkflow(storeVisitVideoWorkflowPath)
	if err != nil {
		return nil, "", err
	}
	workflowLabel := workflowDisplayNameFromPath(storeVisitVideoWorkflowPath)
	setInput := func(nodeID string, key string, value interface{}) {
		if strings.TrimSpace(nodeID) == "" {
			return
		}
		if node, ok := workflowJSON[nodeID].(map[string]interface{}); ok {
			if inputs, ok := node["inputs"].(map[string]interface{}); ok {
				inputs[key] = value
			}
		}
	}

	setInput(meta.PositiveNodeID, meta.PositiveInputKey, strings.TrimSpace(scene.VideoPositivePrompt))
	setInput(meta.NegativeNodeID, meta.NegativeInputKey, buildSegmentNegativePrompt(scene.VideoNegativePrompt))
	if seed <= 0 {
		seed = getConfiguredGlobalSeed()
	}
	setInput(meta.SeedNodeID, meta.SeedInputKey, seed)

	width := scene.VideoWidth
	height := scene.VideoHeight
	if width <= 0 || height <= 0 {
		width, height = generalGuideDefaultVideoSize(scene.SceneType, scene.NeedPresenter)
	}
	setStoreVisitPrimitiveIntByTitle(workflowJSON, "Width", width)
	setStoreVisitPrimitiveIntByTitle(workflowJSON, "Height", height)
	setStoreVisitPrimitiveIntByTitle(workflowJSON, "Frame Rate", generalGuideVideoFPS)
	duration := scene.VideoDurationSeconds
	if duration <= 0 {
		duration = 8
	}
	// Keep the stored duration unchanged, but give ComfyUI one extra second of runway
	// so the final expression/ending is less likely to be cut off.
	duration++
	frameCount := generalGuideVideoFPS*duration + 1
	setStoreVisitPrimitiveIntByTitle(workflowJSON, "Length", frameCount)

	sourceImage := strings.TrimSpace(scene.GeneratedImage)
	if sourceImage == "" {
		sourceImage = strings.TrimSpace(scene.ReferenceImage)
	}
	if sourceImage == "" {
		return nil, "", fmt.Errorf("缺少可用于生成视频的首帧图片")
	}
	imageAbsPath, err := assetWebPathToAbs(sourceImage)
	if err != nil {
		return nil, "", err
	}
	uploadedName, err := UploadToComfyUIInput(imageAbsPath)
	if err != nil {
		return nil, "", err
	}

	var imageNodeID string
	for id, node := range workflowJSON {
		if nodeMap, ok := node.(map[string]interface{}); ok {
			if classType, ok := nodeMap["class_type"].(string); ok && classType == "LoadImage" {
				imageNodeID = id
				break
			}
		}
	}
	if imageNodeID == "" {
		return nil, "", fmt.Errorf("video workflow missing LoadImage node")
	}
	setInput(imageNodeID, "image", uploadedName)

	for _, node := range workflowJSON {
		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		classType, _ := nodeMap["class_type"].(string)
		if classType == "SaveVideo" || classType == "VHS_VideoCombine" {
			if inputs, ok := nodeMap["inputs"].(map[string]interface{}); ok {
				inputs["filename_prefix"] = fmt.Sprintf("%s_%s_video_%d", project.Code, getGeneralGuideSceneFileKey(scene), scene.ID)
			}
		}
	}
	return workflowJSON, workflowLabel, nil
}

func copyGeneralGuideReferenceAsGeneratedImage(scene models.GeneralGuideScene, project models.GeneralGuideProject) (string, error) {
	sourcePath, err := assetWebPathToAbs(scene.ReferenceImage)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(generalGuideImagesDir(project.Code), 0755); err != nil {
		return "", err
	}
	ext := filepath.Ext(sourcePath)
	if ext == "" {
		ext = ".png"
	}
	savePath := filepath.Join(generalGuideImagesDir(project.Code), fmt.Sprintf("%s_%d_%d%s", getGeneralGuideSceneFileKey(scene), scene.ID, time.Now().UnixNano(), ext))
	srcFile, err := os.Open(sourcePath)
	if err != nil {
		return "", err
	}
	defer srcFile.Close()
	dstFile, err := os.Create(savePath)
	if err != nil {
		return "", err
	}
	defer dstFile.Close()
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(savePath), nil
}

func waitForGeneralGuideImageOutput(promptID string, projectCode string, sceneKey string, sceneID uint, shouldContinue func() bool) (string, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if shouldContinue != nil && !shouldContinue() {
			return "", fmt.Errorf("general guide image generation interrupted")
		}
		history, err := GetComfyHistory(promptID)
		if err != nil {
			continue
		}
		outputs, ok := history["outputs"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, nodeOutput := range outputs {
			outputMap, ok := nodeOutput.(map[string]interface{})
			if !ok {
				continue
			}
			images, ok := outputMap["images"].([]interface{})
			if !ok || len(images) == 0 {
				continue
			}
			imgData, ok := images[0].(map[string]interface{})
			if !ok {
				continue
			}
			filename, _ := imgData["filename"].(string)
			subfolder, _ := imgData["subfolder"].(string)
			typeStr, _ := imgData["type"].(string)
			if filename == "" {
				continue
			}
			saveDir := generalGuideImagesDir(projectCode)
			if err := os.MkdirAll(saveDir, 0755); err != nil {
				return "", err
			}
			ext := filepath.Ext(filename)
			if ext == "" {
				ext = ".png"
			}
			savePath := filepath.Join(saveDir, fmt.Sprintf("%s_%d_%d%s", sceneKey, sceneID, time.Now().UnixNano(), ext))
			if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
				return "", err
			}
			return "/" + filepath.ToSlash(savePath), nil
		}
	}
	return "", nil
}

func waitForGeneralGuideVideoOutput(promptID string, projectCode string, sceneKey string, sceneID uint, shouldContinue func() bool) (string, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if shouldContinue != nil && !shouldContinue() {
			return "", fmt.Errorf("general guide video generation interrupted")
		}
		history, err := GetComfyHistory(promptID)
		if err != nil {
			continue
		}
		outputs, ok := history["outputs"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, nodeOutput := range outputs {
			outputMap, ok := nodeOutput.(map[string]interface{})
			if !ok {
				continue
			}
			var fileData map[string]interface{}
			if gifs, ok := outputMap["gifs"].([]interface{}); ok && len(gifs) > 0 {
				fileData, _ = gifs[0].(map[string]interface{})
			} else if images, ok := outputMap["images"].([]interface{}); ok && len(images) > 0 {
				fileData, _ = images[0].(map[string]interface{})
			}
			if fileData == nil {
				continue
			}
			filename, _ := fileData["filename"].(string)
			subfolder, _ := fileData["subfolder"].(string)
			typeStr, _ := fileData["type"].(string)
			if filename == "" {
				continue
			}
			saveDir := generalGuideVideosDir(projectCode)
			if err := os.MkdirAll(saveDir, 0755); err != nil {
				return "", err
			}
			ext := filepath.Ext(filename)
			if ext == "" {
				ext = ".mp4"
			}
			savePath := filepath.Join(saveDir, fmt.Sprintf("%s_%d_%d%s", sceneKey, sceneID, time.Now().UnixNano(), ext))
			if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
				return "", err
			}
			return "/" + filepath.ToSlash(savePath), nil
		}
	}
	return "", nil
}

func startGeneralGuideSceneImageTask(scene *models.GeneralGuideScene, project *models.GeneralGuideProject, seed int64, useLightningLoRA bool) (string, error) {
	payload := generalGuideSceneMediaTaskPayload{
		ProjectID:        project.ID,
		SceneID:          scene.ID,
		Seed:             seed,
		UseLightningLoRA: useLightningLoRA,
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("render_general_guide_scene_image", payload)
	if err != nil {
		return "", err
	}
	now := time.Now()
	updates := map[string]interface{}{
		"image_status":          "generating",
		"image_current_task_id": taskRecord.ID,
		"image_last_error":      "",
		"updated_at":            now,
	}
	if err := db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(updates).Error; err != nil {
		return "", err
	}
	return taskRecord.ID, nil
}

func queueGeneralGuideSceneImageTask(c *gin.Context, scene *models.GeneralGuideScene, project *models.GeneralGuideProject, seed int64, useLightningLoRA bool, message string) {
	taskID, err := startGeneralGuideSceneImageTask(scene, project, seed, useLightningLoRA)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交综合讲解图片生成任务失败"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message":            message,
		"task_id":            taskID,
		"use_lightning_lora": useLightningLoRA,
	})
}

func startGeneralGuideSceneVideoTask(scene *models.GeneralGuideScene, project *models.GeneralGuideProject, seed int64) (string, error) {
	payload := generalGuideSceneMediaTaskPayload{
		ProjectID: project.ID,
		SceneID:   scene.ID,
		Seed:      seed,
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("render_general_guide_scene_video", payload)
	if err != nil {
		return "", err
	}
	if err := db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
		"video_status":          "generating",
		"video_current_task_id": taskRecord.ID,
		"video_last_error":      "",
		"updated_at":            time.Now(),
	}).Error; err != nil {
		return "", err
	}
	return taskRecord.ID, nil
}

func queueGeneralGuideSceneVideoTask(c *gin.Context, scene *models.GeneralGuideScene, project *models.GeneralGuideProject, seed int64, message string) {
	taskID, err := startGeneralGuideSceneVideoTask(scene, project, seed)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交综合讲解视频生成任务失败"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message": message,
		"task_id": taskID,
	})
}

func resetGeneralGuideTransitionAssetsBySceneID(sceneID uint) error {
	var transitions []models.GeneralGuideTransition
	if err := db.DB.Where("from_scene_id = ? OR to_scene_id = ?", sceneID, sceneID).Find(&transitions).Error; err != nil {
		return err
	}
	for _, transition := range transitions {
		if err := removeGeneralGuideAsset(transition.TailFrameImage); err != nil {
			return err
		}
		if err := removeGeneralGuideAsset(transition.GeneratedVideo); err != nil {
			return err
		}
		if err := db.DB.Model(&models.GeneralGuideTransition{}).Where("id = ?", transition.ID).Updates(map[string]interface{}{
			"tail_frame_image":         "",
			"tail_frame_source_video":  "",
			"video_status":             "draft",
			"video_current_task_id":    "",
			"video_last_error":         "",
			"generated_video":          "",
			"video_generated_workflow": "",
			"updated_at":               time.Now(),
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func resetGeneralGuideSceneAssetsAndState(scene *models.GeneralGuideScene) error {
	if scene == nil {
		return fmt.Errorf("讲解场景不存在")
	}
	if err := removeGeneralGuideAsset(scene.GeneratedImage); err != nil {
		return err
	}
	if err := removeGeneralGuideAsset(scene.GeneratedVideo); err != nil {
		return err
	}
	if err := db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
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
		"updated_at":               time.Now(),
	}).Error; err != nil {
		return err
	}
	return resetGeneralGuideTransitionAssetsBySceneID(scene.ID)
}

func ResetGeneralGuideSceneState(c *gin.Context) {
	scene, err := loadGeneralGuideSceneOr404(c)
	if err != nil {
		return
	}
	if err := resetGeneralGuideSceneAssetsAndState(scene); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置这一行失败"})
		return
	}
	var refreshed models.GeneralGuideScene
	if err := db.DB.First(&refreshed, scene.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取重置后的场景失败"})
		return
	}
	c.JSON(http.StatusOK, refreshed)
}

func GenerateGeneralGuideSceneImage(c *gin.Context) {
	scene, err := loadGeneralGuideSceneOr404(c)
	if err != nil {
		return
	}
	if scene.ImageStatus == "generating" || scene.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前场景仍在生成中，请等待完成后再操作"})
		return
	}
	if strings.TrimSpace(scene.ReferenceImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先上传这一行的场景图片"})
		return
	}
	var project models.GeneralGuideProject
	if err := db.DB.First(&project, scene.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	if generalGuideDefaultNeedPresenter(scene.ImagePreset, scene.SceneType) && normalizeGeneralGuideImagePreset(scene.ImagePreset) != generalGuideImagePresetMaterialOnly && strings.TrimSpace(project.PresenterReferenceImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先上传或选择讲解人参考图"})
		return
	}
	var req generalGuideSceneImageGenerateRequest
	_ = c.ShouldBind(&req)
	useLightningLoRA := false
	if req.UseLightningLoRA != nil {
		useLightningLoRA = *req.UseLightningLoRA
	}
	seed := getConfiguredGlobalSeed()
	if req.RandomSeed != nil && *req.RandomSeed {
		seed = time.Now().UnixNano()
	}
	message := "综合讲解图片生成任务已提交"
	if req.RandomSeed != nil && *req.RandomSeed {
		message = "综合讲解图片抽卡任务已提交"
	}
	queueGeneralGuideSceneImageTask(c, scene, &project, seed, useLightningLoRA, message)
}

func GenerateGeneralGuideSceneVideo(c *gin.Context) {
	scene, err := loadGeneralGuideSceneOr404(c)
	if err != nil {
		return
	}
	if scene.ImageStatus == "generating" || scene.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前场景仍在生成中，请等待完成后再操作"})
		return
	}
	if strings.TrimSpace(scene.VideoPositivePrompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先填写视频提示词"})
		return
	}
	if strings.TrimSpace(scene.GeneratedImage) == "" && strings.TrimSpace(scene.ReferenceImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先上传场景图片，或先生成合成图片"})
		return
	}
	if normalizeGeneralGuideImagePreset(scene.ImagePreset) != generalGuideImagePresetMaterialOnly && scene.NeedPresenter && strings.TrimSpace(scene.GeneratedImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先生成这一行的合成图片"})
		return
	}
	var project models.GeneralGuideProject
	if err := db.DB.First(&project, scene.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	var req generalGuideSceneVideoGenerateRequest
	_ = c.ShouldBind(&req)
	seed := getConfiguredGlobalSeed()
	if req.RandomSeed != nil && *req.RandomSeed {
		seed = time.Now().UnixNano()
	}
	message := "综合讲解视频生成任务已提交"
	if req.RandomSeed != nil && *req.RandomSeed {
		message = "综合讲解视频抽卡任务已提交"
	}
	queueGeneralGuideSceneVideoTask(c, scene, &project, seed, message)
}

func HandleRenderGeneralGuideSceneImageTask(t *models.Task) (interface{}, error) {
	var payload generalGuideSceneMediaTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	var project models.GeneralGuideProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	var scene models.GeneralGuideScene
	if err := db.DB.First(&scene, payload.SceneID).Error; err != nil {
		return nil, fmt.Errorf("scene not found: %w", err)
	}
	sceneKey := getGeneralGuideSceneFileKey(scene)

	if normalizeGeneralGuideImagePreset(scene.ImagePreset) == generalGuideImagePresetMaterialOnly || !scene.NeedPresenter {
		webPath, err := copyGeneralGuideReferenceAsGeneratedImage(scene, project)
		if err != nil {
			if shouldApplyGeneralGuideSceneImageTaskResult(scene.ID, t.ID) {
				_ = db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
					"image_status":          "failed",
					"image_current_task_id": "",
					"image_last_error":      err.Error(),
					"updated_at":            time.Now(),
				}).Error
			}
			return nil, err
		}
		if !shouldApplyGeneralGuideSceneImageTaskResult(scene.ID, t.ID) {
			return gin.H{"skipped": true}, nil
		}
		if err := db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
			"generated_image":          webPath,
			"image_status":             "generated",
			"image_current_task_id":    "",
			"image_last_error":         "",
			"image_generated_workflow": "纯素材直出",
			"updated_at":               time.Now(),
		}).Error; err != nil {
			return nil, err
		}
		return gin.H{"generated_image": webPath}, nil
	}

	template, err := loadStoreVisitWorkflowTemplate(generalGuideImageWorkflowPath)
	if err != nil {
		if shouldApplyGeneralGuideSceneImageTaskResult(scene.ID, t.ID) {
			_ = db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
				"image_status":          "failed",
				"image_current_task_id": "",
				"image_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	sceneRefAbs, err := assetWebPathToAbs(scene.ReferenceImage)
	if err != nil {
		return nil, err
	}
	presenterRefAbs, err := assetWebPathToAbs(project.PresenterReferenceImage)
	if err != nil {
		return nil, err
	}
	sceneImageName, err := UploadToComfyUIInput(sceneRefAbs)
	if err != nil {
		return nil, err
	}
	presenterImageName, err := UploadToComfyUIInput(presenterRefAbs)
	if err != nil {
		return nil, err
	}
	workflowJSON, workflowLabel, err := buildGeneralGuideImageWorkflow(template, sceneImageName, presenterImageName, scene, project, payload.Seed, payload.UseLightningLoRA)
	if err != nil {
		if shouldApplyGeneralGuideSceneImageTaskResult(scene.ID, t.ID) {
			_ = db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
				"image_status":          "failed",
				"image_current_task_id": "",
				"image_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	logComfyWorkflowPayload("General Guide Image ComfyUI Payload", workflowLabel, workflowJSON)
	promptID, err := QueueComfyPrompt(workflowJSON)
	if err != nil {
		if shouldApplyGeneralGuideSceneImageTaskResult(scene.ID, t.ID) {
			_ = db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
				"image_status":          "failed",
				"image_current_task_id": "",
				"image_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	Log(LogLevelInfo, "综合讲解图片已提交到 ComfyUI 队列", fmt.Sprintf("ProjectID: %d\nSceneID: %d\nPromptID: %s\nWorkflow: %s", project.ID, scene.ID, promptID, workflowLabel))
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 40, "")
	webPath, err := waitForGeneralGuideImageOutput(promptID, project.Code, sceneKey, scene.ID, func() bool {
		return shouldApplyGeneralGuideSceneImageTaskResult(scene.ID, t.ID)
	})
	if err != nil {
		if shouldApplyGeneralGuideSceneImageTaskResult(scene.ID, t.ID) {
			_ = db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
				"image_status":          "failed",
				"image_current_task_id": "",
				"image_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	if strings.TrimSpace(webPath) == "" {
		err = fmt.Errorf("未获取到综合讲解图片输出")
		if shouldApplyGeneralGuideSceneImageTaskResult(scene.ID, t.ID) {
			_ = db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
				"image_status":          "failed",
				"image_current_task_id": "",
				"image_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	if !shouldApplyGeneralGuideSceneImageTaskResult(scene.ID, t.ID) {
		return gin.H{"skipped": true}, nil
	}
	if err := db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
		"generated_image":          webPath,
		"image_status":             "generated",
		"image_current_task_id":    "",
		"image_last_error":         "",
		"image_generated_workflow": workflowLabel,
		"updated_at":               time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	return gin.H{"generated_image": webPath}, nil
}

func HandleRenderGeneralGuideSceneVideoTask(t *models.Task) (interface{}, error) {
	var payload generalGuideSceneMediaTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	var project models.GeneralGuideProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	var scene models.GeneralGuideScene
	if err := db.DB.First(&scene, payload.SceneID).Error; err != nil {
		return nil, fmt.Errorf("scene not found: %w", err)
	}
	sceneKey := getGeneralGuideSceneFileKey(scene)
	workflowJSON, workflowLabel, err := buildGeneralGuideVideoWorkflow(scene, project, payload.Seed)
	if err != nil {
		if shouldApplyGeneralGuideSceneVideoTaskResult(scene.ID, t.ID) {
			_ = db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	logComfyWorkflowPayload("General Guide Video ComfyUI Payload", workflowLabel, workflowJSON)
	promptID, err := QueueComfyPrompt(workflowJSON)
	if err != nil {
		if shouldApplyGeneralGuideSceneVideoTaskResult(scene.ID, t.ID) {
			_ = db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	Log(LogLevelInfo, "综合讲解视频已提交到 ComfyUI 队列", fmt.Sprintf("ProjectID: %d\nSceneID: %d\nPromptID: %s\nWorkflow: %s", project.ID, scene.ID, promptID, workflowLabel))
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 40, "")
	webPath, err := waitForGeneralGuideVideoOutput(promptID, project.Code, sceneKey, scene.ID, func() bool {
		return shouldApplyGeneralGuideSceneVideoTaskResult(scene.ID, t.ID)
	})
	if err != nil {
		if shouldApplyGeneralGuideSceneVideoTaskResult(scene.ID, t.ID) {
			_ = db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	if strings.TrimSpace(webPath) == "" {
		err = fmt.Errorf("未获取到综合讲解视频输出")
		if shouldApplyGeneralGuideSceneVideoTaskResult(scene.ID, t.ID) {
			_ = db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	if !shouldApplyGeneralGuideSceneVideoTaskResult(scene.ID, t.ID) {
		return gin.H{"skipped": true}, nil
	}
	if err := db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
		"generated_video":          webPath,
		"video_status":             "generated",
		"video_current_task_id":    "",
		"video_last_error":         "",
		"video_generated_workflow": workflowLabel,
		"updated_at":               time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	return gin.H{"generated_video": webPath}, nil
}
