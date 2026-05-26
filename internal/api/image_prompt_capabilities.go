package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/workflow"
)

func defaultImageWorkflowUsesIndependentNegativePrompt() bool {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyDefaultImageModel).First(&setting).Error; err != nil {
		return true
	}
	workflowName := strings.TrimSpace(setting.Value)
	if workflowName == "" {
		return true
	}
	return workflowNameUsesIndependentNegativePrompt(workflowName)
}

func workflowNameUsesIndependentNegativePrompt(workflowName string) bool {
	files, _ := filepath.Glob(filepath.Join("workflows", "*.json"))
	for _, file := range files {
		meta, err := workflow.ParseWorkflow(file)
		if err != nil {
			continue
		}
		baseName := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		if meta.WorkflowName != workflowName && baseName != workflowName {
			continue
		}
		return workflowFileUsesIndependentNegativePrompt(file, meta)
	}
	return true
}

func workflowFileUsesIndependentNegativePrompt(filePath string, meta *models.WorkflowMetadata) bool {
	if meta == nil || strings.TrimSpace(meta.NegativeNodeID) == "" {
		return false
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return true
	}
	var wfJSON map[string]interface{}
	if err := json.Unmarshal(data, &wfJSON); err != nil {
		return true
	}
	return workflowJSONUsesIndependentNegativePrompt(meta, wfJSON)
}

func workflowJSONUsesIndependentNegativePrompt(meta *models.WorkflowMetadata, wfJSON map[string]interface{}) bool {
	if meta == nil || strings.TrimSpace(meta.NegativeNodeID) == "" {
		return false
	}
	node, ok := wfJSON[meta.NegativeNodeID].(map[string]interface{})
	if !ok {
		return true
	}
	classType, _ := node["class_type"].(string)
	return strings.TrimSpace(classType) != "ConditioningZeroOut"
}
