package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"

	"github.com/gin-gonic/gin"
)

type audioCloneLineTaskPayload struct {
	ProjectID     uint   `json:"project_id"`
	LineID        uint   `json:"line_id"`
	Seed          int64  `json:"seed"`
	PromptID      string `json:"prompt_id,omitempty"`
	WorkflowLabel string `json:"workflow_label,omitempty"`
}

type audioCloneReferenceRecognitionTaskPayload struct {
	ProjectID   uint `json:"project_id"`
	CharacterID uint `json:"character_id"`
}

const (
	audioCloneSeedMax      = int64(2147483647)
	audioCloneFallbackSeed = int64(264590)
)

func normalizeAudioCloneSeed(seed int64) int64 {
	if seed < 0 {
		seed = -seed
	}
	if seed > audioCloneSeedMax {
		seed = seed % audioCloneSeedMax
	}
	if seed <= 0 {
		return audioCloneFallbackSeed
	}
	return seed
}

func randomAudioCloneSeed() int64 {
	seed := time.Now().UnixNano()
	return normalizeAudioCloneSeed(seed)
}

func startAudioCloneLineTask(line *models.AudioCloneLine, project *models.AudioCloneProject, seed int64, promptID string, workflowLabel string) (string, error) {
	payload := audioCloneLineTaskPayload{
		ProjectID:     project.ID,
		LineID:        line.ID,
		Seed:          normalizeAudioCloneSeed(seed),
		PromptID:      strings.TrimSpace(promptID),
		WorkflowLabel: strings.TrimSpace(workflowLabel),
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("render_audio_clone_line", payload)
	if err != nil {
		return "", err
	}
	return taskRecord.ID, nil
}

func startAudioCloneReferenceRecognitionTask(character *models.AudioCloneCharacter, project *models.AudioCloneProject) (string, error) {
	payload := audioCloneReferenceRecognitionTaskPayload{
		ProjectID:   project.ID,
		CharacterID: character.ID,
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("recognize_audio_clone_character_reference", payload)
	if err != nil {
		return "", err
	}
	return taskRecord.ID, nil
}

func shouldApplyAudioCloneLineTaskResult(lineID uint, taskID string) bool {
	var line models.AudioCloneLine
	if err := db.DB.Select("id", "current_task_id", "status").First(&line, lineID).Error; err != nil {
		return false
	}
	// The render worker can pick up a newly queued task before the HTTP handler
	// finishes writing current_task_id. Let that fresh draft line continue; the
	// later final write will still be guarded by current_task_id once it is set.
	if strings.TrimSpace(line.CurrentTaskID) == "" && line.Status == audioCloneLineStatusDraft {
		return true
	}
	return strings.TrimSpace(line.CurrentTaskID) == taskID && line.Status == audioCloneLineStatusGenerating
}

func shouldApplyAudioCloneReferenceRecognitionTaskResult(characterID uint, taskID string) bool {
	var character models.AudioCloneCharacter
	if err := db.DB.Select("id", "reference_text_current_task_id", "reference_text_status").First(&character, characterID).Error; err != nil {
		return false
	}
	if strings.TrimSpace(character.ReferenceTextCurrentTaskID) == "" && character.ReferenceTextStatus == audioCloneLineStatusDraft {
		return true
	}
	return strings.TrimSpace(character.ReferenceTextCurrentTaskID) == taskID && character.ReferenceTextStatus == audioCloneLineStatusGenerating
}

func buildAudioCloneWorkflow(template map[string]interface{}, referenceAudioName string, referenceText string, targetText string, project models.AudioCloneProject, line models.AudioCloneLine, seed int64) (map[string]interface{}, string, error) {
	workflowJSON, err := cloneStoreVisitWorkflow(template)
	if err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "4", "audio", referenceAudioName); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "3", "prompt_text", strings.TrimSpace(referenceText)); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "3", "text", strings.TrimSpace(targetText)); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "3", "seed", seed); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "9", "filename_prefix", fmt.Sprintf("audio/%s_line_%02d_%d", project.Code, line.SortOrder, line.ID)); err != nil {
		return nil, "", err
	}
	return workflowJSON, workflowDisplayNameFromPath(audioCloneWorkflowPath), nil
}

func queueAudioCloneLinePrompt(project models.AudioCloneProject, line models.AudioCloneLine, seed int64) (string, string, error) {
	var character models.AudioCloneCharacter
	if err := db.DB.Where("project_id = ? AND name = ?", project.ID, line.CharacterName).First(&character).Error; err != nil {
		return "", "", fmt.Errorf("character %s not found: %w", line.CharacterName, err)
	}
	referenceAudioAbs, err := assetWebPathToAbs(character.ReferenceAudio)
	if err != nil {
		return "", "", err
	}
	referenceAudioName, err := UploadToComfyUIInput(referenceAudioAbs)
	if err != nil {
		return "", "", err
	}
	template, err := loadStoreVisitWorkflowTemplate(audioCloneWorkflowPath)
	if err != nil {
		return "", "", err
	}
	workflowJSON, workflowLabel, err := buildAudioCloneWorkflow(template, referenceAudioName, character.ReferenceText, line.Text, project, line, seed)
	if err != nil {
		return "", "", err
	}
	logComfyWorkflowPayload("Audio Clone ComfyUI Payload", workflowLabel, workflowJSON)
	promptID, err := QueueComfyPrompt(workflowJSON)
	if err != nil {
		return "", "", err
	}
	return promptID, workflowLabel, nil
}

func buildAudioCloneReferenceRecognitionWorkflow(template map[string]interface{}, referenceAudioName string) (map[string]interface{}, string, error) {
	workflowJSON, err := cloneStoreVisitWorkflow(template)
	if err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "6", "audio", referenceAudioName); err != nil {
		return nil, "", err
	}
	return workflowJSON, workflowDisplayNameFromPath(qwenTTSASRWorkflowPath), nil
}

func waitForAudioCloneOutput(promptID string, projectCode string, line models.AudioCloneLine, shouldContinue func() bool) (string, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if shouldContinue != nil && !shouldContinue() {
			return "", fmt.Errorf("audio clone generation interrupted")
		}
		history, err := GetComfyHistory(promptID)
		if err != nil {
			continue
		}
		outputs, ok := history["outputs"].(map[string]interface{})
		if !ok {
			continue
		}
		if nodeOutput, ok := outputs["9"]; ok {
			outputMap, _ := nodeOutput.(map[string]interface{})
			if fileData := firstComfyOutputFile(outputMap); fileData != nil {
				filename, _ := fileData["filename"].(string)
				subfolder, _ := fileData["subfolder"].(string)
				typeStr, _ := fileData["type"].(string)
				if filename != "" {
					saveDir := audioCloneGeneratedDir(projectCode)
					if err := os.MkdirAll(saveDir, 0755); err != nil {
						return "", err
					}
					ext := filepath.Ext(filename)
					if ext == "" {
						ext = ".mp3"
					}
					savePath := filepath.Join(saveDir, fmt.Sprintf("line_%02d_%d_%d%s", line.SortOrder, line.ID, time.Now().UnixNano(), ext))
					if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
						return "", err
					}
					return "/" + filepath.ToSlash(savePath), nil
				}
			}
		}
		for _, nodeOutput := range outputs {
			outputMap, ok := nodeOutput.(map[string]interface{})
			if !ok {
				continue
			}
			fileData := firstComfyOutputFile(outputMap)
			if fileData == nil {
				continue
			}
			filename, _ := fileData["filename"].(string)
			subfolder, _ := fileData["subfolder"].(string)
			typeStr, _ := fileData["type"].(string)
			if filename == "" {
				continue
			}
			saveDir := audioCloneGeneratedDir(projectCode)
			if err := os.MkdirAll(saveDir, 0755); err != nil {
				return "", err
			}
			ext := filepath.Ext(filename)
			if ext == "" {
				ext = ".mp3"
			}
			savePath := filepath.Join(saveDir, fmt.Sprintf("line_%02d_%d_%d%s", line.SortOrder, line.ID, time.Now().UnixNano(), ext))
			if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
				return "", err
			}
			return "/" + filepath.ToSlash(savePath), nil
		}
	}
	return "", nil
}

func waitForAudioCloneReferenceRecognitionOutput(promptID string, shouldContinue func() bool) (string, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	startedAt := time.Now()

	for range ticker.C {
		if time.Since(startedAt) > 2*time.Minute {
			return "", fmt.Errorf("等待 LongChat ASR 识别结果超时")
		}
		if shouldContinue != nil && !shouldContinue() {
			return "", fmt.Errorf("audio clone reference recognition interrupted")
		}
		history, err := GetComfyHistory(promptID)
		if err != nil {
			continue
		}
		asrText := extractQwenTTSASRTextFromHistory(history)
		if strings.TrimSpace(asrText) != "" {
			return strings.TrimSpace(asrText), nil
		}
	}
	return "", nil
}

func firstComfyOutputFile(outputMap map[string]interface{}) map[string]interface{} {
	for _, key := range []string{"audio", "audios", "gifs", "images"} {
		items, ok := outputMap[key].([]interface{})
		if !ok || len(items) == 0 {
			continue
		}
		fileData, _ := items[0].(map[string]interface{})
		if fileData != nil {
			return fileData
		}
	}
	return nil
}

func HandleRecognizeAudioCloneCharacterReferenceTask(t *models.Task) (interface{}, error) {
	var payload audioCloneReferenceRecognitionTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	var project models.AudioCloneProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	var character models.AudioCloneCharacter
	if err := db.DB.First(&character, payload.CharacterID).Error; err != nil {
		return nil, fmt.Errorf("character not found: %w", err)
	}
	referenceAudioAbs, err := assetWebPathToAbs(character.ReferenceAudio)
	if err != nil {
		return nil, err
	}
	referenceAudioName, err := UploadToComfyUIInput(referenceAudioAbs)
	if err != nil {
		return nil, err
	}
	template, err := loadStoreVisitWorkflowTemplate(qwenTTSASRWorkflowPath)
	if err != nil {
		return nil, err
	}
	workflowJSON, workflowLabel, err := buildAudioCloneReferenceRecognitionWorkflow(template, referenceAudioName)
	if err != nil {
		if shouldApplyAudioCloneReferenceRecognitionTaskResult(character.ID, t.ID) {
			_ = db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
				"reference_text_status":          audioCloneLineStatusFailed,
				"reference_text_current_task_id": "",
				"reference_text_error":           err.Error(),
				"updated_at":                     time.Now(),
			}).Error
		}
		return nil, err
	}
	logComfyWorkflowPayload("LongChat 参考音频识别 Payload", workflowLabel, workflowJSON)
	promptID, err := QueueComfyPrompt(workflowJSON)
	if err != nil {
		if shouldApplyAudioCloneReferenceRecognitionTaskResult(character.ID, t.ID) {
			_ = db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
				"reference_text_status":          audioCloneLineStatusFailed,
				"reference_text_current_task_id": "",
				"reference_text_error":           err.Error(),
				"updated_at":                     time.Now(),
			}).Error
		}
		return nil, err
	}
	Log(LogLevelInfo, "LongChat 参考音频识别已提交到 ComfyUI 队列", fmt.Sprintf("ProjectID: %d\nCharacterID: %d\nPromptID: %s\nWorkflow: %s", project.ID, character.ID, promptID, workflowLabel))
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 40, "")
	asrText, err := waitForAudioCloneReferenceRecognitionOutput(promptID, func() bool {
		return shouldApplyAudioCloneReferenceRecognitionTaskResult(character.ID, t.ID)
	})
	if err != nil {
		if shouldApplyAudioCloneReferenceRecognitionTaskResult(character.ID, t.ID) {
			_ = db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
				"reference_text_status":          audioCloneLineStatusFailed,
				"reference_text_current_task_id": "",
				"reference_text_error":           err.Error(),
				"updated_at":                     time.Now(),
			}).Error
		}
		return nil, err
	}
	if strings.TrimSpace(asrText) == "" {
		err = fmt.Errorf("未获取到 LongChat 参考音频识别结果")
		if shouldApplyAudioCloneReferenceRecognitionTaskResult(character.ID, t.ID) {
			_ = db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
				"reference_text_status":          audioCloneLineStatusFailed,
				"reference_text_current_task_id": "",
				"reference_text_error":           err.Error(),
				"updated_at":                     time.Now(),
			}).Error
		}
		return nil, err
	}
	if !shouldApplyAudioCloneReferenceRecognitionTaskResult(character.ID, t.ID) {
		return gin.H{"skipped": true}, nil
	}
	if err := db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
		"reference_text":                 strings.TrimSpace(asrText),
		"reference_text_status":          audioCloneLineStatusGenerated,
		"reference_text_current_task_id": "",
		"reference_text_error":           "",
		"updated_at":                     time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	return gin.H{"reference_text": strings.TrimSpace(asrText)}, nil
}

func HandleRenderAudioCloneLineTask(t *models.Task) (interface{}, error) {
	var payload audioCloneLineTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	var project models.AudioCloneProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	var line models.AudioCloneLine
	if err := db.DB.First(&line, payload.LineID).Error; err != nil {
		return nil, fmt.Errorf("line not found: %w", err)
	}
	promptID := strings.TrimSpace(payload.PromptID)
	workflowLabel := strings.TrimSpace(payload.WorkflowLabel)
	if promptID == "" {
		var err error
		promptID, workflowLabel, err = queueAudioCloneLinePrompt(project, line, payload.Seed)
		if err != nil {
			if shouldApplyAudioCloneLineTaskResult(line.ID, t.ID) {
				_ = db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
					"status":          audioCloneLineStatusFailed,
					"current_task_id": "",
					"last_error":      err.Error(),
					"updated_at":      time.Now(),
				}).Error
			}
			return nil, err
		}
	}
	if promptID == "" {
		err := fmt.Errorf("missing audio clone prompt id")
		if shouldApplyAudioCloneLineTaskResult(line.ID, t.ID) {
			_ = db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":          audioCloneLineStatusFailed,
				"current_task_id": "",
				"last_error":      err.Error(),
				"updated_at":      time.Now(),
			}).Error
		}
		return nil, err
	}
	Log(LogLevelInfo, "音频复制任务已提交到 ComfyUI 队列", fmt.Sprintf("ProjectID: %d\nLineID: %d\nPromptID: %s\nWorkflow: %s", project.ID, line.ID, promptID, workflowLabel))
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 40, "")
	webPath, err := waitForAudioCloneOutput(promptID, project.Code, line, func() bool {
		return shouldApplyAudioCloneLineTaskResult(line.ID, t.ID)
	})
	if err != nil {
		if shouldApplyAudioCloneLineTaskResult(line.ID, t.ID) {
			_ = db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":          audioCloneLineStatusFailed,
				"current_task_id": "",
				"last_error":      err.Error(),
				"updated_at":      time.Now(),
			}).Error
		}
		return nil, err
	}
	if strings.TrimSpace(webPath) == "" {
		err = fmt.Errorf("未获取到音频复制输出")
		if shouldApplyAudioCloneLineTaskResult(line.ID, t.ID) {
			_ = db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":          audioCloneLineStatusFailed,
				"current_task_id": "",
				"last_error":      err.Error(),
				"updated_at":      time.Now(),
			}).Error
		}
		return nil, err
	}
	if !shouldApplyAudioCloneLineTaskResult(line.ID, t.ID) {
		return gin.H{"skipped": true}, nil
	}
	if err := db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
		"generated_audio":    webPath,
		"status":             audioCloneLineStatusGenerated,
		"current_task_id":    "",
		"last_error":         "",
		"generated_workflow": workflowLabel,
		"updated_at":         time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	return gin.H{"generated_audio": webPath}, nil
}
