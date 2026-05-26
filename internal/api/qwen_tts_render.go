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

type qwenTTSLineTaskPayload struct {
	ProjectID     uint   `json:"project_id"`
	LineID        uint   `json:"line_id"`
	Seed          int64  `json:"seed"`
	PromptID      string `json:"prompt_id,omitempty"`
	WorkflowLabel string `json:"workflow_label,omitempty"`
}

type qwenTTSReferenceRecognitionTaskPayload struct {
	ProjectID   uint  `json:"project_id"`
	CharacterID uint  `json:"character_id"`
	Seed        int64 `json:"seed"`
}

const (
	qwenTTSSeedMax      = int64(2147483647)
	qwenTTSFallbackSeed = int64(264590)
	qwenTTSTopP         = 0.6
)

func normalizeQwenTTSSeed(seed int64) int64 {
	if seed < 0 {
		seed = -seed
	}
	if seed > qwenTTSSeedMax {
		seed = seed % qwenTTSSeedMax
	}
	if seed <= 0 {
		return qwenTTSFallbackSeed
	}
	return seed
}

func randomQwenTTSSeed() int64 {
	return normalizeQwenTTSSeed(time.Now().UnixNano())
}

func startQwenTTSLineTask(line *models.QwenTTSLine, project *models.QwenTTSProject, seed int64, promptID string, workflowLabel string) (string, error) {
	payload := qwenTTSLineTaskPayload{
		ProjectID:     project.ID,
		LineID:        line.ID,
		Seed:          normalizeQwenTTSSeed(seed),
		PromptID:      strings.TrimSpace(promptID),
		WorkflowLabel: strings.TrimSpace(workflowLabel),
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("render_qwen_tts_line", payload)
	if err != nil {
		return "", err
	}
	return taskRecord.ID, nil
}

func startQwenTTSReferenceRecognitionTask(character *models.QwenTTSCharacter, project *models.QwenTTSProject, seed int64) (string, error) {
	payload := qwenTTSReferenceRecognitionTaskPayload{
		ProjectID:   project.ID,
		CharacterID: character.ID,
		Seed:        normalizeQwenTTSSeed(seed),
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("recognize_qwen_tts_character_reference", payload)
	if err != nil {
		return "", err
	}
	return taskRecord.ID, nil
}

func shouldApplyQwenTTSLineTaskResult(lineID uint, taskID string) bool {
	var line models.QwenTTSLine
	if err := db.DB.Select("id", "current_task_id", "status").First(&line, lineID).Error; err != nil {
		return false
	}
	if strings.TrimSpace(line.CurrentTaskID) == "" && line.Status == audioCloneLineStatusDraft {
		return true
	}
	return strings.TrimSpace(line.CurrentTaskID) == taskID && line.Status == audioCloneLineStatusGenerating
}

func shouldApplyQwenTTSReferenceRecognitionTaskResult(characterID uint, taskID string) bool {
	var character models.QwenTTSCharacter
	if err := db.DB.Select("id", "reference_text_current_task_id", "reference_text_status").First(&character, characterID).Error; err != nil {
		return false
	}
	if strings.TrimSpace(character.ReferenceTextCurrentTaskID) == "" && character.ReferenceTextStatus == audioCloneLineStatusDraft {
		return true
	}
	return strings.TrimSpace(character.ReferenceTextCurrentTaskID) == taskID && character.ReferenceTextStatus == audioCloneLineStatusGenerating
}

func buildQwenTTSWorkflow(template map[string]interface{}, referenceAudioName string, referenceTextOverride string, targetText string, project models.QwenTTSProject, line models.QwenTTSLine, seed int64) (map[string]interface{}, string, error) {
	workflowJSON, err := cloneStoreVisitWorkflow(template)
	if err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "6", "audio", referenceAudioName); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "39", "target_text", strings.TrimSpace(targetText)); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "35", "x_vector_only", project.XVectorOnly); err != nil {
		return nil, "", err
	}
	instruct := strings.TrimSpace(line.Instruct)
	if err := setStoreVisitWorkflowInput(workflowJSON, "39", "instruct", instruct); err != nil {
		return nil, "", err
	}
	temperature := normalizeQwenTTSTemperature(project.Temperature)
	if line.Temperature > 0 {
		temperature = normalizeQwenTTSTemperature(line.Temperature)
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "39", "temperature", temperature); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "39", "top_p", qwenTTSTopP); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "39", "seed", normalizeQwenTTSSeed(seed)); err != nil {
		return nil, "", err
	}
	if override := strings.TrimSpace(referenceTextOverride); override != "" {
		// Keep the original ASR path by default. Only replace ref_text when the
		// user explicitly provides an override because SenseVoice misheard it.
		if err := setStoreVisitWorkflowInput(workflowJSON, "35", "ref_text", override); err != nil {
			return nil, "", err
		}
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "8", "filename_prefix", fmt.Sprintf("audio/%s_line_%02d_%d", project.Code, line.SortOrder, line.ID)); err != nil {
		return nil, "", err
	}
	return workflowJSON, workflowDisplayNameFromPath(qwenTTSWorkflowPath), nil
}

func queueQwenTTSLinePrompt(project models.QwenTTSProject, line models.QwenTTSLine, seed int64) (string, string, error) {
	var character models.QwenTTSCharacter
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
	template, err := loadStoreVisitWorkflowTemplate(qwenTTSWorkflowPath)
	if err != nil {
		return "", "", err
	}
	workflowJSON, workflowLabel, err := buildQwenTTSWorkflow(template, referenceAudioName, character.ReferenceText, line.Text, project, line, seed)
	if err != nil {
		return "", "", err
	}
	logComfyWorkflowPayload("Qwen3 TTS ComfyUI Payload", workflowLabel, workflowJSON)
	promptID, err := QueueComfyPrompt(workflowJSON)
	if err != nil {
		return "", "", err
	}
	return promptID, workflowLabel, nil
}

func buildQwenTTSReferenceRecognitionWorkflow(template map[string]interface{}, referenceAudioName string) (map[string]interface{}, string, error) {
	workflowJSON, err := cloneStoreVisitWorkflow(template)
	if err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "6", "audio", referenceAudioName); err != nil {
		return nil, "", err
	}
	return workflowJSON, workflowDisplayNameFromPath(qwenTTSASRWorkflowPath), nil
}

func cleanQwenTTSASRText(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, "ASR Result:"); idx >= 0 {
		text = strings.TrimSpace(text[idx+len("ASR Result:"):])
	}
	if idx := strings.Index(text, "| Emotion:"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	text = strings.Trim(text, "\"'` \t\r\n")
	lower := strings.ToLower(text)
	if text == "" || strings.Contains(lower, "filename") || strings.Contains(lower, "subfolder") || strings.HasPrefix(lower, "audio/") {
		return ""
	}
	return text
}

func extractQwenTTSASRTextFromValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return cleanQwenTTSASRText(v)
	case []interface{}:
		for _, item := range v {
			if text := extractQwenTTSASRTextFromValue(item); text != "" {
				return text
			}
		}
	case map[string]interface{}:
		for _, key := range []string{"text", "result", "transcription", "asr_result", "asr_text", "ref_text"} {
			if item, ok := v[key]; ok {
				if text := extractQwenTTSASRTextFromValue(item); text != "" {
					return text
				}
			}
		}
		for key, item := range v {
			if key == "audio" || key == "audios" || key == "images" || key == "gifs" {
				continue
			}
			if text := extractQwenTTSASRTextFromValue(item); text != "" {
				return text
			}
		}
	}
	return ""
}

func extractQwenTTSASRTextFromHistory(history map[string]interface{}) string {
	outputs, ok := history["outputs"].(map[string]interface{})
	if !ok {
		return ""
	}
	if node40, ok := outputs["40"]; ok {
		if text := extractQwenTTSASRTextFromValue(node40); text != "" {
			return text
		}
	}
	if node42, ok := outputs["42"]; ok {
		if text := extractQwenTTSASRTextFromValue(node42); text != "" {
			return text
		}
	}
	return ""
}

func waitForQwenTTSOutput(promptID string, projectCode string, line models.QwenTTSLine, shouldContinue func() bool) (string, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if shouldContinue != nil && !shouldContinue() {
			return "", fmt.Errorf("qwen tts generation interrupted")
		}
		history, err := GetComfyHistory(promptID)
		if err != nil {
			continue
		}
		outputs, ok := history["outputs"].(map[string]interface{})
		if !ok {
			continue
		}
		if nodeOutput, ok := outputs["8"]; ok {
			outputMap, _ := nodeOutput.(map[string]interface{})
			if fileData := firstComfyOutputFile(outputMap); fileData != nil {
				filename, _ := fileData["filename"].(string)
				subfolder, _ := fileData["subfolder"].(string)
				typeStr, _ := fileData["type"].(string)
				if filename != "" {
					saveDir := qwenTTSGeneratedDir(projectCode)
					if err := os.MkdirAll(saveDir, 0755); err != nil {
						return "", err
					}
					ext := filepath.Ext(filename)
					if ext == "" {
						ext = ".wav"
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
			saveDir := qwenTTSGeneratedDir(projectCode)
			if err := os.MkdirAll(saveDir, 0755); err != nil {
				return "", err
			}
			ext := filepath.Ext(filename)
			if ext == "" {
				ext = ".wav"
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

func waitForQwenTTSReferenceRecognitionOutput(promptID string, projectCode string, character models.QwenTTSCharacter, shouldContinue func() bool) (string, string, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	startedAt := time.Now()

	for range ticker.C {
		if time.Since(startedAt) > 2*time.Minute {
			return "", "", fmt.Errorf("等待 Qwen3 ASR 识别结果超时")
		}
		if shouldContinue != nil && !shouldContinue() {
			return "", "", fmt.Errorf("qwen tts reference recognition interrupted")
		}
		history, err := GetComfyHistory(promptID)
		if err != nil {
			continue
		}
		asrText := extractQwenTTSASRTextFromHistory(history)
		if strings.TrimSpace(asrText) != "" {
			return strings.TrimSpace(asrText), "", nil
		}
	}
	return "", "", nil
}

func HandleRecognizeQwenTTSCharacterReferenceTask(t *models.Task) (interface{}, error) {
	var payload qwenTTSReferenceRecognitionTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	var project models.QwenTTSProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	var character models.QwenTTSCharacter
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
	workflowJSON, workflowLabel, err := buildQwenTTSReferenceRecognitionWorkflow(template, referenceAudioName)
	if err != nil {
		if shouldApplyQwenTTSReferenceRecognitionTaskResult(character.ID, t.ID) {
			_ = db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
				"reference_text_status":          audioCloneLineStatusFailed,
				"reference_text_current_task_id": "",
				"reference_text_error":           err.Error(),
				"updated_at":                     time.Now(),
			}).Error
		}
		return nil, err
	}
	logComfyWorkflowPayload("Qwen3 TTS 参考音频识别 Payload", workflowLabel, workflowJSON)
	promptID, err := QueueComfyPrompt(workflowJSON)
	if err != nil {
		if shouldApplyQwenTTSReferenceRecognitionTaskResult(character.ID, t.ID) {
			_ = db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
				"reference_text_status":          audioCloneLineStatusFailed,
				"reference_text_current_task_id": "",
				"reference_text_error":           err.Error(),
				"updated_at":                     time.Now(),
			}).Error
		}
		return nil, err
	}
	Log(LogLevelInfo, "Qwen3 TTS 参考音频识别已提交到 ComfyUI 队列", fmt.Sprintf("ProjectID: %d\nCharacterID: %d\nPromptID: %s\nWorkflow: %s", project.ID, character.ID, promptID, workflowLabel))
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 40, "")
	asrText, testAudioPath, err := waitForQwenTTSReferenceRecognitionOutput(promptID, project.Code, character, func() bool {
		return shouldApplyQwenTTSReferenceRecognitionTaskResult(character.ID, t.ID)
	})
	if err != nil {
		if shouldApplyQwenTTSReferenceRecognitionTaskResult(character.ID, t.ID) {
			_ = db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
				"reference_text_status":          audioCloneLineStatusFailed,
				"reference_text_current_task_id": "",
				"reference_text_error":           err.Error(),
				"reference_test_audio":           testAudioPath,
				"updated_at":                     time.Now(),
			}).Error
		}
		return nil, err
	}
	if strings.TrimSpace(asrText) == "" {
		err = fmt.Errorf("未获取到 Qwen3 参考音频识别结果")
		if shouldApplyQwenTTSReferenceRecognitionTaskResult(character.ID, t.ID) {
			_ = db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
				"reference_text_status":          audioCloneLineStatusFailed,
				"reference_text_current_task_id": "",
				"reference_text_error":           err.Error(),
				"reference_test_audio":           testAudioPath,
				"updated_at":                     time.Now(),
			}).Error
		}
		return nil, err
	}
	if !shouldApplyQwenTTSReferenceRecognitionTaskResult(character.ID, t.ID) {
		return gin.H{"skipped": true}, nil
	}
	updates := map[string]interface{}{
		"reference_text":                 strings.TrimSpace(asrText),
		"reference_text_status":          audioCloneLineStatusGenerated,
		"reference_text_current_task_id": "",
		"reference_text_error":           "",
		"updated_at":                     time.Now(),
	}
	if strings.TrimSpace(testAudioPath) != "" {
		updates["reference_test_audio"] = testAudioPath
	}
	if err := db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", character.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	return gin.H{"reference_text": strings.TrimSpace(asrText), "reference_test_audio": testAudioPath}, nil
}

func HandleRenderQwenTTSLineTask(t *models.Task) (interface{}, error) {
	var payload qwenTTSLineTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	var project models.QwenTTSProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	var line models.QwenTTSLine
	if err := db.DB.First(&line, payload.LineID).Error; err != nil {
		return nil, fmt.Errorf("line not found: %w", err)
	}
	promptID := strings.TrimSpace(payload.PromptID)
	workflowLabel := strings.TrimSpace(payload.WorkflowLabel)
	if promptID == "" {
		var err error
		promptID, workflowLabel, err = queueQwenTTSLinePrompt(project, line, payload.Seed)
		if err != nil {
			if shouldApplyQwenTTSLineTaskResult(line.ID, t.ID) {
				_ = db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
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
		err := fmt.Errorf("missing qwen tts prompt id")
		if shouldApplyQwenTTSLineTaskResult(line.ID, t.ID) {
			_ = db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":          audioCloneLineStatusFailed,
				"current_task_id": "",
				"last_error":      err.Error(),
				"updated_at":      time.Now(),
			}).Error
		}
		return nil, err
	}
	Log(LogLevelInfo, "Qwen3 TTS 任务已提交到 ComfyUI 队列", fmt.Sprintf("ProjectID: %d\nLineID: %d\nPromptID: %s\nWorkflow: %s", project.ID, line.ID, promptID, workflowLabel))
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 40, "")
	webPath, err := waitForQwenTTSOutput(promptID, project.Code, line, func() bool {
		return shouldApplyQwenTTSLineTaskResult(line.ID, t.ID)
	})
	if err != nil {
		if shouldApplyQwenTTSLineTaskResult(line.ID, t.ID) {
			_ = db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":          audioCloneLineStatusFailed,
				"current_task_id": "",
				"last_error":      err.Error(),
				"updated_at":      time.Now(),
			}).Error
		}
		return nil, err
	}
	if strings.TrimSpace(webPath) == "" {
		err = fmt.Errorf("未获取到 Qwen3 TTS 输出")
		if shouldApplyQwenTTSLineTaskResult(line.ID, t.ID) {
			_ = db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":          audioCloneLineStatusFailed,
				"current_task_id": "",
				"last_error":      err.Error(),
				"updated_at":      time.Now(),
			}).Error
		}
		return nil, err
	}
	if !shouldApplyQwenTTSLineTaskResult(line.ID, t.ID) {
		return gin.H{"skipped": true}, nil
	}
	if err := db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
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
