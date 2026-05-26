package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/workflow"

	"github.com/gin-gonic/gin"
)

// CheckModelStatus represents the status of a model file
type CheckModelStatus struct {
	FileName     string   `json:"file_name"`
	Type         string   `json:"type"`
	ExpectedPath string   `json:"expected_path"`
	Exists       bool     `json:"exists"`
	DownloadURL  []string `json:"download_urls"`
}

// CheckWorkflowModels checks if the models required by a workflow exist
func CheckWorkflowModels(c *gin.Context) {
	workflowName := c.Query("workflow")
	if workflowName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Workflow name is required"})
		return
	}

	// 1. Get ComfyUI Models Directory from settings
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyComfyUIModelsDir).First(&setting).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve ComfyUI Models Directory setting"})
		return
	}
	modelsDir := setting.Value
	if modelsDir == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先在系统设置中配置 ComfyUI Models 目录"})
		return
	}

	// 2. Find workflow file
	// Assuming workflows are in "workflows" directory and have .json extension
	// We need to find the file that matches the workflow name (which might be filename_prefix or filename)
	// Since we don't have a direct mapping from name to path in memory here without reparsing all,
	// let's try to match by filename first, then by reparsing if needed?
	// Or better, let frontend send the full filename or we scan "workflows/*.json" again.
	// Since we previously parsed workflows and frontend has the list, let's assume `workflowName` is the `WorkflowName` from metadata.
	// We need to find the file.

	files, err := filepath.Glob(filepath.Join("workflows", "*.json"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan workflows"})
		return
	}

	var targetFile string
	for _, file := range files {
		meta, err := workflow.ParseWorkflow(file)
		if err == nil && meta.WorkflowName == workflowName {
			targetFile = file
			break
		}
	}

	if targetFile == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workflow not found"})
		return
	}

	// 3. Get Dependencies
	deps, err := workflow.GetDependencies(targetFile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse workflow dependencies: " + err.Error()})
		return
	}

	// 4. Check existence
	var results []CheckModelStatus
	for _, dep := range deps {
		// Map dependency type to subfolder
		// parser.go types: checkpoints, loras, vae, controlnet, clip_vision, upscale_models, unet
		subDir := dep.Type

		// Handle UNET specific logic (unet or diffusion_models)
		var expectedPaths []string
		if dep.Type == "unet" {
			expectedPaths = []string{
				filepath.Join(modelsDir, "unet", dep.FileName),
				filepath.Join(modelsDir, "diffusion_models", dep.FileName),
			}
		} else {
			expectedPaths = []string{
				filepath.Join(modelsDir, subDir, dep.FileName),
			}
		}

		exists := false
		finalPath := ""

		for _, path := range expectedPaths {
			if _, err := os.Stat(path); err == nil {
				exists = true
				finalPath = path
				break
			}
		}

		// If not found, show the primary expected path (usually the first one)
		if !exists {
			finalPath = expectedPaths[0]
			if len(expectedPaths) > 1 {
				// Show both for clarity if multiple possible locations
				finalPath = fmt.Sprintf("%s 或 %s", expectedPaths[0], expectedPaths[1])
			}
		}

		results = append(results, CheckModelStatus{
			FileName:     dep.FileName,
			Type:         dep.Type,
			ExpectedPath: finalPath,
			Exists:       exists,
			DownloadURL: []string{
				fmt.Sprintf("https://huggingface.co/search/full-text?q=%s", dep.FileName),
				fmt.Sprintf("https://modelscope.cn/models?name=%s", dep.FileName),
			},
		})
	}

	c.JSON(http.StatusOK, results)
}

// GetComfyUIStatus checks if the ComfyUI server is reachable
func GetComfyUIStatus(c *gin.Context) {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyComfyUIAddress).First(&setting).Error; err != nil {
		// Default fallback if not set
		setting.Value = "127.0.0.1:8188"
	}

	address := setting.Value
	if !strings.HasPrefix(address, "http") {
		address = "http://" + address
	}

	client := http.Client{
		Timeout: 2 * time.Second,
	}

	resp, err := client.Get(address + "/system_stats")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "offline",
			"address": setting.Value,
			"error":   err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		c.JSON(http.StatusOK, gin.H{
			"status":  "online",
			"address": setting.Value,
		})
	} else {
		c.JSON(http.StatusOK, gin.H{
			"status":  "offline",
			"address": setting.Value,
			"code":    resp.StatusCode,
		})
	}
}

// QueueComfyPrompt submits a workflow to ComfyUI for execution
func QueueComfyPrompt(workflowJSON map[string]interface{}) (string, error) {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyComfyUIAddress).First(&setting).Error; err != nil {
		setting.Value = "127.0.0.1:8188"
	}

	address := setting.Value
	if !strings.HasPrefix(address, "http") {
		address = "http://" + address
	}

	client := http.Client{
		Timeout: 10 * time.Second,
	}

	// Wrap in "prompt" key as required by ComfyUI API
	// Also sanitize any filenames in the workflow
	sanitizedWorkflow := SanitizeWorkflow(workflowJSON)
	payload := map[string]interface{}{
		"prompt": sanitizedWorkflow,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal workflow: %v", err)
	}

	resp, err := client.Post(address+"/prompt", "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", fmt.Errorf("failed to send prompt to ComfyUI: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("comfyui returned error %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}

	if promptID, ok := result["prompt_id"].(string); ok {
		return promptID, nil
	}

	return "", fmt.Errorf("no prompt_id in response")
}

// GetComfyHistory retrieves history for a specific prompt ID
func GetComfyHistory(promptID string) (map[string]interface{}, error) {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyComfyUIAddress).First(&setting).Error; err != nil {
		setting.Value = "127.0.0.1:8188"
	}

	address := setting.Value
	if !strings.HasPrefix(address, "http") {
		address = "http://" + address
	}

	client := http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(address + "/history/" + promptID)
	if err != nil {
		return nil, fmt.Errorf("failed to get history: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comfyui history returned error %d", resp.StatusCode)
	}

	var history map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		return nil, fmt.Errorf("failed to decode history: %v", err)
	}

	// History is keyed by prompt_id, get the inner object
	if data, ok := history[promptID].(map[string]interface{}); ok {
		return data, nil
	}

	return nil, fmt.Errorf("history for prompt %s not found or incomplete", promptID)
}

func IsComfyPromptActive(promptID string) (bool, error) {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyComfyUIAddress).First(&setting).Error; err != nil {
		setting.Value = "127.0.0.1:8188"
	}

	address := setting.Value
	if !strings.HasPrefix(address, "http") {
		address = "http://" + address
	}

	client := http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(address + "/queue")
	if err != nil {
		return false, fmt.Errorf("failed to get queue: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("comfyui queue returned error %d", resp.StatusCode)
	}

	var queue map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&queue); err != nil {
		return false, fmt.Errorf("failed to decode queue: %v", err)
	}

	return queueContainsPrompt(queue["queue_running"], promptID) || queueContainsPrompt(queue["queue_pending"], promptID), nil
}

func queueContainsPrompt(data interface{}, promptID string) bool {
	switch typed := data.(type) {
	case []interface{}:
		for _, item := range typed {
			if row, ok := item.([]interface{}); ok {
				if len(row) > 1 {
					if value, ok := row[1].(string); ok && value == promptID {
						return true
					}
				}
			}
			if queueContainsPrompt(item, promptID) {
				return true
			}
		}
	case map[string]interface{}:
		if value, ok := typed["prompt_id"].(string); ok && value == promptID {
			return true
		}
		for _, value := range typed {
			if queueContainsPrompt(value, promptID) {
				return true
			}
		}
	}
	return false
}

// DownloadComfyImage downloads an image from ComfyUI and saves it locally
func DownloadComfyImage(filename, subfolder, typeStr string, savePath string) error {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyComfyUIAddress).First(&setting).Error; err != nil {
		setting.Value = "127.0.0.1:8188"
	}

	address := setting.Value
	if !strings.HasPrefix(address, "http") {
		address = "http://" + address
	}

	// Construct view URL with encoded query parameters to handle non-ASCII filenames safely.
	query := url.Values{}
	query.Set("filename", filename)
	query.Set("subfolder", subfolder)
	query.Set("type", typeStr)
	url := fmt.Sprintf("%s/view?%s", address, query.Encode())

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download image: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned error %d", resp.StatusCode)
	}

	// Create directory if not exists
	if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// Create file
	out, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer out.Close()

	// Copy data
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to save image data: %v", err)
	}

	return nil
}

// StopComfyUI sends an interrupt command to ComfyUI to stop current generation
func StopComfyUI() error {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyComfyUIAddress).First(&setting).Error; err != nil {
		setting.Value = "127.0.0.1:8188"
	}

	address := setting.Value
	if !strings.HasPrefix(address, "http") {
		address = "http://" + address
	}

	client := http.Client{
		Timeout: 5 * time.Second,
	}

	// ComfyUI /interrupt endpoint
	resp, err := client.Post(address+"/interrupt", "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to send interrupt to ComfyUI: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("comfyui interrupt returned error %d", resp.StatusCode)
	}

	return nil
}

// CopyFile copies a file from src to dst
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// UploadToComfyUIInput copies a local file to the ComfyUI input directory
// Returns the filename relative to the input directory, or error
func UploadToComfyUIInput(localPath string) (string, error) {
	// 1. Get ComfyUI base directory from settings (or assume default relative path if running locally)
	// For now, we'll try to find "ComfyUI/input" relative to our executable or use a configured path.
	// Since the user is running "x:\Comfyui\Kt(Go)-Ai-Studio", and ComfyUI is likely at "x:\Comfyui" or similar.
	// Let's assume a standard structure or require configuration.
	// A robust way is to use the API "upload/image" endpoint of ComfyUI, but copying is faster for local.
	// Given the user error "Invalid char in url query", it seems we are passing a path that ComfyUI doesn't like in the URL?
	// Actually, the error `Invalid char in url query` for `filename` suggests we are passing a full path or weird chars to `/view`?
	// Wait, the error `GET /view?filename=Qwen-Editor-1...` is when ComfyUI TRIES TO SERVE the image back?
	// Or when we try to fetch it?
	// "Invalid char in url query: ... filename=Qwen-Editor-1（内部节点..."
	// The filename has Chinese characters and parentheses. This breaks the URL when we (or ComfyUI UI) try to fetch it.
	// We need to Sanitize filenames in workflows!

	// But for INPUT images (LoadImage), we should also copy them to ComfyUI input folder for safety.
	// However, LoadImage node supports absolute paths in many environments.
	// If we want to be safe, we should copy to `ComfyUI/input` and pass just the filename.

	// Let's implement the copy logic first.
	// We need to know where ComfyUI 'input' folder is.
	// We can try to infer it from the ComfyUI address if it's local, or ask user to config.
	// For this environment "x:\Comfyui\Kt(Go)-Ai-Studio", maybe "x:\Comfyui\ComfyUI\input"?
	// Or "D:\Comfyui\ComfyUI\input"?
	// Without configuration, we can't be sure.
	// BUT, we can use the ComfyUI API to upload the image!
	// POST /upload/image

	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyComfyUIAddress).First(&setting).Error; err != nil {
		setting.Value = "127.0.0.1:8188"
	}
	address := setting.Value
	if !strings.HasPrefix(address, "http") {
		address = "http://" + address
	}

	// Prepare multipart form
	file, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to open local file: %v", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("image", filepath.Base(localPath))
	if err != nil {
		return "", err
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return "", err
	}
	writer.Close()

	// Upload to ComfyUI
	resp, err := http.Post(fmt.Sprintf("%s/upload/image", address), writer.FormDataContentType(), body)
	if err != nil {
		return "", fmt.Errorf("failed to upload image to ComfyUI: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("comfyui upload failed: %s", string(bodyBytes))
	}

	// Parse response to get the name ComfyUI saved it as (it handles duplicates)
	var result struct {
		Name      string `json:"name"`
		Subfolder string `json:"subfolder"`
		Type      string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode upload response: %v", err)
	}

	return result.Name, nil
}

// SanitizeWorkflow removes or replaces special characters in filenames within the workflow
// to prevent URL encoding issues in ComfyUI API
func SanitizeWorkflow(workflow map[string]interface{}) map[string]interface{} {
	// Deep copy to avoid modifying original if needed (though map pass by reference)
	// Simple JSON roundtrip is easiest for deep copy if performance isn't critical
	// Or just iterate.

	// We specifically look for "SaveImage" nodes and their "filename_prefix" input
	for _, node := range workflow {
		if nodeMap, ok := node.(map[string]interface{}); ok {
			if classType, ok := nodeMap["class_type"].(string); ok {
				if classType == "SaveImage" || classType == "VideoCombine" || classType == "VHS_VideoCombine" {
					if inputs, ok := nodeMap["inputs"].(map[string]interface{}); ok {
						if prefix, ok := inputs["filename_prefix"].(string); ok {
							// Replace special chars with underscore or safe chars
							// Aggressively replace anything that is not alphanumeric, underscore, hyphen or dot
							// This handles Chinese characters, spaces, parentheses, etc.
							var safePrefix string
							reg, err := regexp.Compile("[^a-zA-Z0-9_\\-\\.]+")
							if err != nil {
								// Fallback to simple replacement if regex fails (unlikely)
								safePrefix = strings.ReplaceAll(prefix, " ", "_")
							} else {
								safePrefix = reg.ReplaceAllString(prefix, "_")
							}

							// Ensure it doesn't start with special chars if ComfyUI dislikes that, but usually fine.
							// Trim leading/trailing underscores
							safePrefix = strings.Trim(safePrefix, "_")

							if safePrefix == "" {
								safePrefix = "output" // Fallback if empty after sanitization
							}

							// Ensure prefix doesn't end with a dot if ComfyUI appends extensions
							safePrefix = strings.TrimSuffix(safePrefix, ".")

							inputs["filename_prefix"] = safePrefix
						}
					}
				}
			}
		}
	}
	return workflow
}
