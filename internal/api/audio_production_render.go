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

type audioProductionLineTaskPayload struct {
	ProjectID uint  `json:"project_id"`
	LineID    uint  `json:"line_id"`
	Seed      int64 `json:"seed"`
}

const (
	audioProductionSeedMax      = int64(1125899906842624)
	audioProductionFallbackSeed = int64(264590)
)

func normalizeAudioProductionSeed(seed int64) int64 {
	if seed < 0 {
		seed = -seed
	}
	if seed > audioProductionSeedMax {
		seed = seed % audioProductionSeedMax
	}
	if seed <= 0 {
		return audioProductionFallbackSeed
	}
	return seed
}

func randomAudioProductionSeed() int64 {
	return normalizeAudioProductionSeed(time.Now().UnixNano())
}

func startAudioProductionLineTask(line *models.AudioProductionLine, project *models.AudioProductionProject, seed int64) (string, error) {
	payload := audioProductionLineTaskPayload{
		ProjectID: project.ID,
		LineID:    line.ID,
		Seed:      normalizeAudioProductionSeed(seed),
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("render_audio_production_line", payload)
	if err != nil {
		return "", err
	}
	return taskRecord.ID, nil
}

func shouldApplyAudioProductionLineTaskResult(lineID uint, taskID string) bool {
	var line models.AudioProductionLine
	if err := db.DB.Select("id", "current_task_id", "status").First(&line, lineID).Error; err != nil {
		return false
	}
	if strings.TrimSpace(line.CurrentTaskID) == "" && line.Status == audioCloneLineStatusDraft {
		return true
	}
	return strings.TrimSpace(line.CurrentTaskID) == taskID && line.Status == audioCloneLineStatusGenerating
}

func buildAudioProductionWorkflow(project models.AudioProductionProject, line models.AudioProductionLine, seed int64) (map[string]interface{}, string, error) {
	var workflowPath string
	switch project.Mode {
	case audioProductionModeCustomVoice:
		workflowPath = audioProductionCustomVoiceWorkflowPath
	case audioProductionModeVoicePrompt:
		workflowPath = audioProductionVoicePromptWorkflowPath
	default:
		return nil, "", fmt.Errorf("unsupported audio production mode: %s", project.Mode)
	}
	template, err := loadStoreVisitWorkflowTemplate(workflowPath)
	if err != nil {
		return nil, "", err
	}
	workflowJSON, err := cloneStoreVisitWorkflow(template)
	if err != nil {
		return nil, "", err
	}
	text := normalizeAudioProductionOneLine(line.Text)
	if text == "" {
		return nil, "", fmt.Errorf("生成文本不能为空")
	}
	temperature := normalizeAudioProductionTemperature(project.Temperature)
	if line.Temperature > 0 {
		temperature = normalizeAudioProductionTemperature(line.Temperature)
	}
	switch project.Mode {
	case audioProductionModeCustomVoice:
		speaker := strings.TrimSpace(project.Speaker)
		if speaker == "" {
			speaker = strings.TrimSpace(line.Speaker)
		}
		if speaker == "" {
			speaker = defaultAudioProductionCustomVoiceSpeaker
		}
		instruct := normalizeAudioProductionOneLine(project.Instruct)
		if instruct == "" {
			instruct = normalizeAudioProductionOneLine(line.Instruct)
		}
		if err := setStoreVisitWorkflowInput(workflowJSON, "41", "text", text); err != nil {
			return nil, "", err
		}
		if err := setStoreVisitWorkflowInput(workflowJSON, "41", "speaker", speaker); err != nil {
			return nil, "", err
		}
		if err := setStoreVisitWorkflowInput(workflowJSON, "41", "instruct", instruct); err != nil {
			return nil, "", err
		}
		if err := setStoreVisitWorkflowInput(workflowJSON, "41", "temperature", temperature); err != nil {
			return nil, "", err
		}
		if err := setStoreVisitWorkflowInput(workflowJSON, "41", "seed", normalizeAudioProductionSeed(seed)); err != nil {
			return nil, "", err
		}
		if err := setStoreVisitWorkflowInput(workflowJSON, "26", "filename_prefix", fmt.Sprintf("audio/%s_line_%02d_%d", project.Code, line.SortOrder, line.ID)); err != nil {
			return nil, "", err
		}
	case audioProductionModeVoicePrompt:
		voiceInstruction := normalizeAudioProductionOneLine(project.VoiceInstruction)
		if voiceInstruction == "" {
			voiceInstruction = normalizeAudioProductionOneLine(line.VoiceInstruction)
		}
		if voiceInstruction == "" {
			return nil, "", fmt.Errorf("请先填写声音提示词")
		}
		if err := setStoreVisitWorkflowInput(workflowJSON, "38", "text", text); err != nil {
			return nil, "", err
		}
		if err := setStoreVisitWorkflowInput(workflowJSON, "38", "voice_instruction", voiceInstruction); err != nil {
			return nil, "", err
		}
		if err := setStoreVisitWorkflowInput(workflowJSON, "38", "temperature", temperature); err != nil {
			return nil, "", err
		}
		if err := setStoreVisitWorkflowInput(workflowJSON, "38", "seed", normalizeAudioProductionSeed(seed)); err != nil {
			return nil, "", err
		}
		if err := setStoreVisitWorkflowInput(workflowJSON, "32", "filename_prefix", fmt.Sprintf("audio/%s_line_%02d_%d", project.Code, line.SortOrder, line.ID)); err != nil {
			return nil, "", err
		}
	}
	return workflowJSON, workflowDisplayNameFromPath(workflowPath), nil
}

func waitForAudioProductionOutput(promptID string, projectCode string, line models.AudioProductionLine, shouldContinue func() bool) (string, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if shouldContinue != nil && !shouldContinue() {
			return "", fmt.Errorf("audio production generation interrupted")
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
			saveDir := audioProductionGeneratedDir(projectCode)
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

func HandleRenderAudioProductionLineTask(t *models.Task) (interface{}, error) {
	var payload audioProductionLineTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	var project models.AudioProductionProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	var line models.AudioProductionLine
	if err := db.DB.First(&line, payload.LineID).Error; err != nil {
		return nil, fmt.Errorf("line not found: %w", err)
	}
	workflowJSON, workflowLabel, err := buildAudioProductionWorkflow(project, line, payload.Seed)
	if err != nil {
		if shouldApplyAudioProductionLineTaskResult(line.ID, t.ID) {
			_ = db.DB.Model(&models.AudioProductionLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":          audioCloneLineStatusFailed,
				"current_task_id": "",
				"last_error":      err.Error(),
				"updated_at":      time.Now(),
			}).Error
		}
		return nil, err
	}
	logComfyWorkflowPayload("音频生产 ComfyUI Payload", workflowLabel, workflowJSON)
	promptID, err := QueueComfyPrompt(workflowJSON)
	if err != nil {
		if shouldApplyAudioProductionLineTaskResult(line.ID, t.ID) {
			_ = db.DB.Model(&models.AudioProductionLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":          audioCloneLineStatusFailed,
				"current_task_id": "",
				"last_error":      err.Error(),
				"updated_at":      time.Now(),
			}).Error
		}
		return nil, err
	}
	Log(LogLevelInfo, "音频生产任务已提交到 ComfyUI 队列", fmt.Sprintf("ProjectID: %d\nLineID: %d\nPromptID: %s\nWorkflow: %s", project.ID, line.ID, promptID, workflowLabel))
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 40, "")
	webPath, err := waitForAudioProductionOutput(promptID, project.Code, line, func() bool {
		return shouldApplyAudioProductionLineTaskResult(line.ID, t.ID)
	})
	if err != nil {
		if shouldApplyAudioProductionLineTaskResult(line.ID, t.ID) {
			_ = db.DB.Model(&models.AudioProductionLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":          audioCloneLineStatusFailed,
				"current_task_id": "",
				"last_error":      err.Error(),
				"updated_at":      time.Now(),
			}).Error
		}
		return nil, err
	}
	if strings.TrimSpace(webPath) == "" {
		err = fmt.Errorf("未获取到音频生产输出")
		if shouldApplyAudioProductionLineTaskResult(line.ID, t.ID) {
			_ = db.DB.Model(&models.AudioProductionLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":          audioCloneLineStatusFailed,
				"current_task_id": "",
				"last_error":      err.Error(),
				"updated_at":      time.Now(),
			}).Error
		}
		return nil, err
	}
	if !shouldApplyAudioProductionLineTaskResult(line.ID, t.ID) {
		return gin.H{"skipped": true}, nil
	}
	if err := db.DB.Model(&models.AudioProductionLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
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
