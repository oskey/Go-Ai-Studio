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

// Global SSE channel
var messageChan = make(chan string)

// SSEHandler handles Server-Sent Events
func SSEHandler(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")

	c.Stream(func(w io.Writer) bool {
		// Wait for message
		if msg, ok := <-messageChan; ok {
			c.SSEvent("message", msg)
			return true
		}
		return false
	})
}

// BroadcastUpdate sends a notification to all connected clients
func BroadcastUpdate(entityType string, id uint) {
	// Non-blocking send
	select {
	case messageChan <- fmt.Sprintf(`{"type":"%s","id":%d}`, entityType, id):
	default:
		// No listeners or channel full, ignore
	}
}

// ListCharacters retrieves all characters for a project
func ListCharacters(c *gin.Context) {
	projectID := c.Param("id")
	var characters []models.Character
	if err := db.DB.Where("project_id = ?", projectID).Find(&characters).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch characters"})
		return
	}
	c.JSON(http.StatusOK, characters)
}

// AddCharacter adds a new character
func AddCharacter(c *gin.Context) {
	var char models.Character
	if err := c.ShouldBindJSON(&char); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	char.Name = strings.TrimSpace(char.Name)
	char.Gender = strings.TrimSpace(char.Gender)
	char.Age = strings.TrimSpace(char.Age)
	char.BodyHeight = strings.TrimSpace(char.BodyHeight)
	char.Era = strings.TrimSpace(char.Era)
	char.Country = strings.TrimSpace(char.Country)
	char.Appearance = strings.TrimSpace(char.Appearance)
	char.Description = strings.TrimSpace(char.Description)
	char.Width = 0
	char.Height = 0
	char.Seed = 0
	char.CreatedAt = time.Now()
	char.UpdatedAt = time.Now()

	if err := db.DB.Create(&char).Error; err != nil {
		Log(LogLevelError, "创建角色失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create character"})
		return
	}

	Log(LogLevelInfo, "创建角色", fmt.Sprintf("创建了新角色: %s", char.Name))
	c.JSON(http.StatusCreated, char)
}

// UpdateCharacter updates an existing character
func UpdateCharacter(c *gin.Context) {
	id := c.Param("charId")
	var char models.Character
	if err := db.DB.First(&char, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Character not found"})
		return
	}

	var updateData models.Character
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update fields
	char.Name = strings.TrimSpace(updateData.Name)
	char.Gender = strings.TrimSpace(updateData.Gender)
	char.Age = strings.TrimSpace(updateData.Age)
	char.BodyHeight = strings.TrimSpace(updateData.BodyHeight)
	char.Era = strings.TrimSpace(updateData.Era)
	char.Country = strings.TrimSpace(updateData.Country)
	char.Appearance = strings.TrimSpace(updateData.Appearance)
	char.Description = strings.TrimSpace(updateData.Description)
	char.IsLocked = updateData.IsLocked
	char.Fingerprint = updateData.Fingerprint
	char.PositivePrompt = updateData.PositivePrompt
	char.NegativePrompt = updateData.NegativePrompt
	char.Width = 0
	char.Height = 0
	char.Seed = 0
	char.OptimizeClothing = updateData.OptimizeClothing
	char.RefImage = updateData.RefImage
	char.UseRefImage = updateData.UseRefImage
	char.UpdatedAt = time.Now()

	if err := db.DB.Save(&char).Error; err != nil {
		Log(LogLevelError, "更新角色失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update character"})
		return
	}

	Log(LogLevelInfo, "更新角色", fmt.Sprintf("更新了角色: %s", char.Name))
	c.JSON(http.StatusOK, char)
}

// DeleteCharacter deletes a character
func DeleteCharacter(c *gin.Context) {
	id := c.Param("charId")
	if err := db.DB.Delete(&models.Character{}, id).Error; err != nil {
		Log(LogLevelError, "删除角色失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete character"})
		return
	}

	Log(LogLevelInfo, "删除角色", fmt.Sprintf("删除角色 ID: %s", id))
	c.JSON(http.StatusOK, gin.H{"message": "Character deleted successfully"})
}

// UploadFile handles file uploads
func UploadFile(c *gin.Context) {
	// Get project code from form data to organize files
	projectCode := c.PostForm("project_code")
	if projectCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_code is required"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	// Create project-specific directory in output folder
	// output/<project_code>/ref_images/
	uploadDir := filepath.Join("output", projectCode, "ref_images")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create upload directory"})
		return
	}

	// Generate unique filename
	filename := fmt.Sprintf("%d_%s", time.Now().UnixNano(), file.Filename)
	savePath := filepath.Join(uploadDir, filename)

	if err := c.SaveUploadedFile(file, savePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
		return
	}

	// Return path relative to root, e.g. "output/project_code/ref_images/filename.jpg"
	// Frontend will need to prepend the API base URL or static file server path
	// Assuming we serve "output" directory at "/output"
	// Normalize path separators to forward slashes for URL compatibility
	urlPath := filepath.ToSlash(savePath)

	// Add leading slash if needed by frontend
	if urlPath[0] != '/' {
		urlPath = "/" + urlPath
	}

	c.JSON(http.StatusOK, gin.H{"path": urlPath})
}

// AutoGenerateCharacterPrompt handles the request to generate prompt for a character
func AutoGenerateCharacterPrompt(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{
		"error": "character prompt regeneration via secondary LLM is disabled in lightweight story mode",
	})
}

// HandleAutoGenerateCharacterPromptTask processes the background task
func HandleAutoGenerateCharacterPromptTask(t *models.Task) (interface{}, error) {
	return nil, fmt.Errorf("character prompt regeneration via secondary llm is disabled in lightweight story mode")
}

// GenerateCharacterImage handles the request to generate image for a character (Image Only)
func GenerateCharacterImage(c *gin.Context) {
	projectID := c.Param("id")
	charID := c.Param("charId")

	// Verify existence
	var char models.Character
	if err := db.DB.First(&char, charID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Character not found"})
		return
	}
	if fmt.Sprintf("%d", char.ProjectID) != projectID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Character does not belong to this project"})
		return
	}

	if strings.TrimSpace(char.Appearance) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Character appearance is missing"})
		return
	}

	if err := resetCharacterImageState(&char); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset character before regeneration"})
		return
	}
	BroadcastUpdate("character", char.ID)

	// Trigger Image Generation
	promptID, err := triggerCharacterImageGeneration(char)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to queue image generation: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Image generation queued", "prompt_id": promptID})
}

func removeGeneratedAsset(assetPath string) error {
	cleanPath := strings.TrimSpace(strings.TrimPrefix(assetPath, "/"))
	if cleanPath == "" {
		return nil
	}
	if err := os.Remove(cleanPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func resetCharacterImageState(char *models.Character) error {
	if char == nil {
		return fmt.Errorf("character is nil")
	}
	if err := removeGeneratedAsset(char.GeneratedImage); err != nil {
		return err
	}
	char.GeneratedImage = ""
	char.GeneratedWorkflow = ""
	char.Status = "draft"
	char.UpdatedAt = time.Now()
	return db.DB.Save(char).Error
}

func appendCharacterStylePrompt(prompt string, project models.Project) string {
	return appendProjectStylePrompt(prompt, project)
}

func buildCharacterPreviewPositivePrompt(char models.Character) (string, error) {
	appearance := strings.TrimSpace(char.Appearance)
	if appearance == "" {
		return "", fmt.Errorf("character appearance is missing")
	}

	identityParts := make([]string, 0, 4)
	if age := strings.TrimSpace(char.Age); age != "" {
		identityParts = append(identityParts, age)
	}
	if bodyHeight := strings.TrimSpace(char.BodyHeight); bodyHeight != "" {
		identityParts = append(identityParts, bodyHeight)
	}
	if country := strings.TrimSpace(char.Country); country != "" {
		identityParts = append(identityParts, country)
	}
	if gender := strings.TrimSpace(char.Gender); gender != "" {
		identityParts = append(identityParts, gender)
	}
	if era := strings.TrimSpace(char.Era); era != "" {
		identityParts = append(identityParts, era)
	}

	baseClothing := "服装采用简洁基础展示服装，不加入剧情动作，不手持物品"
	if strings.TrimSpace(char.Era) != "" || strings.TrimSpace(char.Country) != "" {
		baseClothing = fmt.Sprintf("服装采用符合%s%s的简洁基础展示服装，不加入剧情动作，不手持物品",
			strings.TrimSpace(char.Country),
			strings.TrimSpace(char.Era),
		)
	}

	parts := []string{
		"单人角色预览图",
		"单人全身像",
		"主体居中",
		"正面朝向镜头",
		"完整站立",
		"从头顶到鞋底完整保留在画面内",
		"鞋子完整可见",
		"脚部完整可见",
		"头顶和脚底保留明确留白",
		"左右两侧保留足够留白",
		"人物清晰对焦",
		"神态自然",
		"年龄与年龄感保持固定，不要擅自画老或画嫩",
		"清晰展示基础发型、发际线与鬓角特征",
		"禁止改动基础发型的分线、卷直程度和束发状态",
		"纯色纯净背景",
		"背景不要环境元素、地面、墙面、家具、道具、投影边缘或空间层次",
	}
	if len(identityParts) > 0 {
		parts = append(parts, strings.Join(identityParts, "，"))
	}
	parts = append(parts,
		appearance,
		baseClothing,
		"禁止字幕，禁止画面文字，禁止水印，禁止界面元素",
	)
	return strings.Join(parts, "，"), nil
}

func applyCharacterImageSizeDefaults(char *models.Character) bool {
	if char == nil {
		return false
	}

	defaultWidth, defaultHeight := getConfiguredCharacterImageSize()
	updated := false
	if char.Width <= 0 {
		char.Width = defaultWidth
		updated = true
	}
	if char.Height <= 0 {
		char.Height = defaultHeight
		updated = true
	}
	return updated
}

// triggerCharacterImageGeneration encapsulates the logic to prepare and queue ComfyUI workflow
func triggerCharacterImageGeneration(char models.Character) (string, error) {
	var project models.Project
	if err := db.DB.Preload("ArtStyle").First(&project, char.ProjectID).Error; err != nil {
		return "", fmt.Errorf("failed to load project: %v", err)
	}
	width, height := getConfiguredCharacterImageSize()
	seed := getConfiguredGlobalSeed()
	// Get Default Image Workflow
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyDefaultImageModel).First(&setting).Error; err != nil {
		return "", fmt.Errorf("failed to get default image workflow setting")
	}
	workflowName := setting.Value
	if workflowName == "" {
		return "", fmt.Errorf("default image workflow not set")
	}

	// Find workflow file
	files, _ := filepath.Glob(filepath.Join("workflows", "*.json"))
	var targetFile string
	for _, file := range files {
		meta, err := workflow.ParseWorkflow(file)
		if err == nil && meta.WorkflowName == workflowName {
			targetFile = file
			break
		}
	}
	if targetFile == "" {
		return "", fmt.Errorf("workflow file for '%s' not found", workflowName)
	}
	workflowLabel := workflowDisplayNameFromPath(targetFile)

	// Parse Metadata
	meta, err := workflow.ParseWorkflow(targetFile)
	if err != nil {
		return "", fmt.Errorf("failed to parse workflow: %v", err)
	}

	parserLog := fmt.Sprintf(
		"Workflow: %s\nPositiveNodeID: %s (Key: %s)\nNegativeNodeID: %s (Key: %s)\nSeedNodeID: %s (Key: %s, Value: %d)\nWidthNodeID: %s (Key: %s, Value: %d)\nHeightNodeID: %s (Key: %s, Value: %d)\n",
		meta.WorkflowName,
		meta.PositiveNodeID, meta.PositiveInputKey,
		meta.NegativeNodeID, meta.NegativeInputKey,
		meta.SeedNodeID, meta.SeedInputKey, seed,
		meta.WidthNodeID, meta.WidthInputKey, width,
		meta.HeightNodeID, meta.HeightInputKey, height,
	)
	db.DB.Create(&models.SystemLog{
		Level:     LogLevelInfo,
		Message:   "Workflow Parser Result",
		Details:   parserLog,
		CreatedAt: time.Now(),
	})

	// Load Raw JSON
	data, err := os.ReadFile(targetFile)
	if err != nil {
		return "", fmt.Errorf("failed to read workflow file: %v", err)
	}
	var wfJSON map[string]interface{}
	if err := json.Unmarshal(data, &wfJSON); err != nil {
		return "", fmt.Errorf("failed to unmarshal workflow json: %v", err)
	}

	// Inject Parameters
	setInput := func(nodeID string, key string, value interface{}) {
		if nodeID == "" {
			return
		}
		if node, ok := wfJSON[nodeID].(map[string]interface{}); ok {
			if inputs, ok := node["inputs"].(map[string]interface{}); ok {
				inputs[key] = value
			}
		}
	}

	basePositivePrompt, err := buildCharacterPreviewPositivePrompt(char)
	if err != nil {
		return "", err
	}

	finalPositivePrompt := appendCharacterStylePrompt(basePositivePrompt, project)
	setInput(meta.PositiveNodeID, meta.PositiveInputKey, finalPositivePrompt)

	if meta.NegativeNodeID != "" {
		setInput(meta.NegativeNodeID, meta.NegativeInputKey, "")
	}

	setInput(meta.SeedNodeID, meta.SeedInputKey, seed)

	// Set Dimensions
	setInput(meta.WidthNodeID, meta.WidthInputKey, width)
	setInput(meta.HeightNodeID, meta.HeightInputKey, height)

	// Inject Reference Image Path if enabled
	if char.UseRefImage && char.RefImage != "" {
		// Need to find the image input node for Qwen workflow
		// Assuming we don't have a specific metadata field for it yet, we search for LoadImage node?
		// Or hardcode if we know the workflow structure.
		// "a_qwen_Image_edit_subgraphed.json" likely has a LoadImage node.
		// Let's search for "LoadImage" node.

		// The path stored in char.RefImage might be relative "/output/..." or absolute.
		// ComfyUI needs absolute path or relative to its input directory.
		// Since we upload to "output/...", we need to provide the full absolute path or correct relative path.
		// Our app runs in "x:\Comfyui\Kt(Go)-Ai-Studio", uploads go to "output/...".
		// ComfyUI usually expects images in its "input" folder, OR full absolute path if "LoadImage" supports it (usually does).

		// Convert stored path to absolute OS path
		// stored: "/output/project/ref_images/xxx.png" or "output/..."
		cleanPath := strings.TrimPrefix(char.RefImage, "/")
		absPath, _ := filepath.Abs(cleanPath)

		// Find LoadImage node
		var imageNodeID string
		for id, node := range wfJSON {
			if nodeMap, ok := node.(map[string]interface{}); ok {
				if classType, ok := nodeMap["class_type"].(string); ok {
					if classType == "LoadImage" {
						imageNodeID = id
						break // Assume first LoadImage is the ref image
					}
				}
			}
		}

		// Upload to ComfyUI input directory
		uploadedName, err := UploadToComfyUIInput(absPath)
		if err != nil {
			Log(LogLevelError, "ComfyUI Upload Failed", fmt.Sprintf("Failed to upload ref image %s: %v", absPath, err))
			return "", fmt.Errorf("failed to upload reference image to comfyui input: %v", err)
		} else {
			if imageNodeID != "" {
				setInput(imageNodeID, "image", uploadedName)
			}
		}
	}

	logComfyWorkflowPayload("Character ComfyUI Payload", workflowLabel, wfJSON)

	// Call ComfyUI
	promptID, err := QueueComfyPrompt(wfJSON)
	if err != nil {
		// Log the error
		db.DB.Create(&models.SystemLog{
			Level:     LogLevelError,
			Message:   "QueueComfyPrompt Failed",
			Details:   fmt.Sprintf("Workflow: %s, Error: %v", meta.WorkflowName, err),
			CreatedAt: time.Now(),
		})
		return "", fmt.Errorf("failed to queue image generation: %v", err)
	}
	if workflowLabel != "" {
		if err := db.DB.Model(&models.Character{}).
			Where("id = ?", char.ID).
			Update("generated_workflow", workflowLabel).Error; err != nil {
			Log(LogLevelWarn, "Character Workflow Save Failed", fmt.Sprintf("character=%d workflow=%s err=%v", char.ID, workflowLabel, err))
		}
	}

	// 6. Handle Image Result in Background
	// Since we returned success, we need to poll for image completion or let another task handle it.
	// For simplicity in this architecture, we will spawn a goroutine to poll ComfyUI history.
	// In a robust system, this should be a separate Task type or a long-running task.
	go func(pid string, charID uint, projectCode string) {
		// Poll for completion (timeout 10 minutes)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		timeout := time.After(10 * time.Minute)

		for {
			select {
			case <-timeout:
				Log(LogLevelError, "Image Generation Timeout", fmt.Sprintf("Prompt ID: %s", pid))
				var c models.Character
				if err := db.DB.First(&c, charID).Error; err == nil {
					c.Status = "failed"
					c.UpdatedAt = time.Now()
					db.DB.Save(&c)
					BroadcastUpdate("character", charID)
				}
				return
			case <-ticker.C:
				history, err := GetComfyHistory(pid)
				if err == nil {
					// Check for outputs
					if outputs, ok := history["outputs"].(map[string]interface{}); ok {
						for _, nodeOutput := range outputs {
							if images, ok := nodeOutput.(map[string]interface{})["images"].([]interface{}); ok && len(images) > 0 {
								imgData := images[0].(map[string]interface{})
								filename := imgData["filename"].(string)
								subfolder := imgData["subfolder"].(string)
								typeStr := imgData["type"].(string)

								// Download Image
								// Save to output/<project_code>/characters/<char_id>_<timestamp>.png
								saveDir := filepath.Join("output", projectCode, "characters")
								if err := os.MkdirAll(saveDir, 0755); err != nil {
									Log(LogLevelError, "Create Dir Failed", err.Error())
									return
								}
								saveFilename := fmt.Sprintf("char_%d_%d.png", charID, time.Now().Unix())
								savePath := filepath.Join(saveDir, saveFilename)

								if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err == nil {
									// Update Character Record
									var c models.Character
									if err := db.DB.First(&c, charID).Error; err == nil {
										// Store relative path for frontend (e.g. /output/...)
										// Convert OS path separator to slash
										webPath := "/" + filepath.ToSlash(savePath)
										c.GeneratedImage = webPath
										c.Status = "generated"
										db.DB.Save(&c)
										Log(LogLevelInfo, fmt.Sprintf("Image Saved（%s）", c.Name), fmt.Sprintf("Saved character image: %s", webPath))
										// Notify frontend via SSE
										BroadcastUpdate("character", charID)
									}
								} else {
									Log(LogLevelError, "Image Download Failed", err.Error())
									var c models.Character
									if err := db.DB.First(&c, charID).Error; err == nil {
										c.Status = "failed"
										c.UpdatedAt = time.Now()
										db.DB.Save(&c)
										BroadcastUpdate("character", charID)
									}
								}
								return // Done
							}
						}
					}
				}
			}
		}
	}(promptID, char.ID, project.Code)

	return promptID, nil
}

func callLLMCharacterPrompt(provider models.LLMProvider, system, user string, taskID string) (*CharacterPromptResponse, error) {
	timeout := 10 * time.Minute
	content, err := requestLLMContent(provider, system, user, taskID, timeout, 5, "正在请求 LLM 生成人物提示词...", "人物提示词")
	if err != nil {
		return nil, err
	}

	db.DB.Create(&models.SystemLog{
		Level:     LogLevelInfo,
		Message:   llmLogMessage("LLM Character Prompt Response", provider),
		Details:   content,
		CreatedAt: time.Now(),
	})

	parseResponse := func(raw string) (*CharacterPromptResponse, error) {
		jsonContent := cleanupLLMJSON(raw)

		var result CharacterPromptResponse
		if err := json.Unmarshal([]byte(jsonContent), &result); err != nil {
			Log(LogLevelError, "LLM JSON Error", fmt.Sprintf("Failed to parse JSON: %v. Content: %s", err, jsonContent))
			return nil, fmt.Errorf("failed to parse JSON: %v", err)
		}
		if strings.TrimSpace(result.PromptPosZH) == "" || strings.TrimSpace(result.PlayerDesc) == "" {
			return nil, fmt.Errorf("character prompt response missing required fields")
		}
		return &result, nil
	}

	result, err := parseResponse(content)
	if err == nil {
		return result, nil
	}
	return nil, err
}

type CharacterPromptResponse struct {
	FaceFingerprint string `json:"face_fingerprint"`
	Description     string `json:"description"`
	Fingerprint     string `json:"fingerprint"`
	PromptPosZH     string `json:"prompt_pos_zh"`
	PromptNegZH     string `json:"prompt_neg_zh"`
	PromptPosEN     string `json:"prompt_pos_en"`
	PromptNegEN     string `json:"prompt_neg_en"`
	PlayerDesc      string `json:"player_desc"`
}

func isFemaleCharacter(gender string) bool {
	g := strings.TrimSpace(gender)
	return g == "Female" || g == "女性" || g == "女"
}

func isMinorOrYouthStageCharacter(char models.Character) bool {
	stageText := strings.TrimSpace(char.Name + " " + char.Description + " " + char.Fingerprint)
	if stageText == "" {
		return false
	}

	minorKeywords := []string{
		"孩童", "幼女", "幼年", "年幼",
		"12岁", "13岁", "14岁",
	}
	for _, keyword := range minorKeywords {
		if strings.Contains(stageText, keyword) {
			return true
		}
	}

	return false
}

func isSeniorStageCharacter(char models.Character) bool {
	stageText := strings.TrimSpace(char.Name + " " + char.Description + " " + char.Fingerprint)
	if stageText == "" {
		return false
	}

	seniorKeywords := []string{
		"老年", "晚年", "暮年", "年老",
		"老妇", "老妪", "老太", "老媪",
		"婆婆", "奶奶", "祖母", "外婆",
	}
	for _, keyword := range seniorKeywords {
		if strings.Contains(stageText, keyword) {
			return true
		}
	}

	return false
}

func isHeavyMiddleAgedMaleCharacter(char models.Character) bool {
	if isFemaleCharacter(char.Gender) {
		return false
	}

	text := strings.TrimSpace(char.Name + " " + char.Description + " " + char.Fingerprint)
	if text == "" {
		return false
	}

	heavyKeywords := []string{
		"微胖", "发福", "胸腹厚重", "腹部体积", "胸腹", "肚腩", "肚子", "腹部",
		"中年发福", "宽肩微胖", "肩宽微胖", "胸腹显厚", "腹部明显", "中段厚重",
	}
	for _, keyword := range heavyKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	return false
}

func ensurePromptPrefix(prompt, prefix string) string {
	trimmedPrompt := strings.TrimSpace(prompt)
	trimmedPrefix := strings.TrimSpace(prefix)
	if trimmedPrefix == "" {
		return trimmedPrompt
	}
	if strings.HasPrefix(trimmedPrompt, trimmedPrefix) {
		return trimmedPrompt
	}
	return trimmedPrefix + "\n" + trimmedPrompt
}

// AutoGenerateAllCharacters handles the request to generate base images for all characters in a project
func AutoGenerateAllCharacters(c *gin.Context) {
	projectID := c.Param("id")

	// Verify project existence
	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	// Submit task
	taskPayload := map[string]interface{}{
		"project_id": project.ID,
	}

	t, err := task.GlobalTaskManager.AddTask("batch_generate_characters", taskPayload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to submit task"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Batch generation task submitted", "task_id": t.ID})
}

// DeleteAllCharacterImages removes all generated base images for characters in a project
func DeleteAllCharacterImages(c *gin.Context) {
	projectID := c.Param("id")

	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	var characters []models.Character
	if err := db.DB.Where("project_id = ?", project.ID).Find(&characters).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch characters"})
		return
	}

	resetCount := 0
	for _, char := range characters {
		if strings.TrimSpace(char.GeneratedImage) == "" &&
			strings.TrimSpace(char.GeneratedWorkflow) == "" &&
			strings.TrimSpace(char.Status) == "draft" {
			continue
		}
		if err := resetCharacterImageState(&char); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update character status"})
			return
		}
		BroadcastUpdate("character", char.ID)
		resetCount++
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Character images deleted successfully",
		"count":   resetCount,
	})
}

// ResetCharacterImage removes the generated base image for a single character
func ResetCharacterImage(c *gin.Context) {
	projectID := c.Param("id")
	charID := c.Param("charId")

	var char models.Character
	if err := db.DB.First(&char, charID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Character not found"})
		return
	}
	if fmt.Sprintf("%d", char.ProjectID) != projectID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Character does not belong to this project"})
		return
	}

	if err := resetCharacterImageState(&char); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset character status"})
		return
	}

	BroadcastUpdate("character", char.ID)
	c.JSON(http.StatusOK, gin.H{
		"message":   "Character image reset successfully",
		"character": char,
	})
}

// HandleBatchGenerateCharactersTask processes the batch generation task
func HandleBatchGenerateCharactersTask(t *models.Task) (interface{}, error) {
	var payload struct {
		ProjectID uint `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	var totalChars int64
	if err := db.DB.Model(&models.Character{}).Where("project_id = ?", payload.ProjectID).Count(&totalChars).Error; err != nil {
		return nil, fmt.Errorf("failed to count characters: %v", err)
	}
	if totalChars == 0 {
		return "No characters found in project", nil
	}

	result, err := queueProjectCharacterImages(payload.ProjectID, t.ID)
	if err != nil {
		return nil, err
	}

	return fmt.Sprintf("Batch generation queued successfully (%d queued, %d skipped, total %d)", result.QueuedCount, result.SkippedCount, totalChars), nil
}

// processCharacterGeneration handles the core logic for a single character
// Returns promptID and error
func processCharacterGeneration(charID, projectID uint, taskID string) (string, error) {
	// 1. Fetch Data
	var char models.Character
	if err := db.DB.First(&char, charID).Error; err != nil {
		return "", fmt.Errorf("character not found")
	}

	if err := removeGeneratedAsset(char.GeneratedImage); err != nil {
		return "", fmt.Errorf("failed to remove old character image: %v", err)
	}
	char.GeneratedImage = ""
	char.GeneratedWorkflow = ""
	char.Status = "draft"
	char.UpdatedAt = time.Now()
	if err := db.DB.Save(&char).Error; err != nil {
		return "", fmt.Errorf("failed to reset character before regeneration: %v", err)
	}
	BroadcastUpdate("character", char.ID)

	// 2. Construct Prompt
	promptLangInstruction := "无论输入是什么语言，你都必须让 description、fingerprint、player_desc、prompt_pos_zh 全部使用中文返回。"
	subjectTypePrompt := `
【主体类型判定规则 (CRITICAL)】
    - 在写任何字段前，先判断当前主体属于哪一类：A. 人类/类人角色；B. 非人类生物/动物/怪物；C. 器物/设备/产品/工具/家具/载具等非生命对象。
    - 绝不能默认所有主体都是人类，也不能把设备、器物、动物、怪物强行拟人化。
    - 只有 A 类主体，才允许使用“单人全身”“full body shot”“visible shoes”“feet fully visible”“正面站立”“肩线朝向镜头”“双手空出”“人种/族裔/国别面孔”这类人体专属规则。
    - B 类主体必须返回“单体完整生物基图”：主体完整可见、主体居中、头顶到足爪/尾端完整保留、纯白背景、无遮挡；不要写鞋子、肩线朝向镜头、双手空出、人物国别/族裔面孔。
    - C 类主体必须返回“单体产品/道具基图”：主体完整可见、主体居中、顶部完整露出且上方留白、底部完整露出且下方留白、左右留白、纯白背景、无地面无墙面无阴影；不要写 full body shot、visible shoes、feet fully visible、正面站立、肩线朝向镜头、双手空出、人物面孔或人种/族裔。
    - 若主体不是人类且没有生物学意义上的脸，face_fingerprint 字段改写为“主体前视识别锚点”，只写前面板、顶部结构、主轮廓、主要接口、灯带、纹理、标志性结构等长期稳定识别点，不要硬造五官。
    - description 对 A 类主体继续写稳定底模；对 B/C 类主体则写稳定外形、主轮廓、结构部件、材质、颜色、比例和识别性细节。
    - fingerprint 对 A 类主体继续写非脸部长期锚点；对 B/C 类主体改写成高密度结构化主体锚点，例如“主体轮廓：...；主体结构：...；稳定部件/接口：...；稳定材质/状态外化：...”。
    - 若主体不是人类/类人角色，gender 固定返回“其他”。
`
	referenceImageControlPrompt := `
    【参考图控制指令（CRITICAL）】
    - 如果主体是人类/类人角色：参考图只用于锁定脸部身份，不要复制参考图的服装、身体、姿态、背景、镜头角度。
    - 如果主体是非人类生物/动物/怪物：参考图只用于锁定该主体的外形识别、主轮廓和主要结构，不要复制参考图背景、支架、手持者、环境、临时姿势。
    - 如果主体是器物/设备/产品/工具/家具/载具：参考图只用于锁定主体外形、前视识别锚点、材质与关键识别部件，不要复制参考图背景、底座、支架、手持者、使用者、环境和临时摆拍姿势。
    - 一律忽略参考图中的背景、环境、地面、墙面、阴影、手、人物、镜头倾角、裁切方式和临时摆拍姿势。
    - 若主体不是人类，严禁因为参考图里存在人物或拟人描述而把主体改写成人形角色。
`
	localIdentityPrompt := `
【主体身份 / 背景推断规则 (CRITICAL)】
    - 只有当主体是人类或明确类人角色时，你才需要根据人物名字、剧情语境、职业身份、地点、时代、社会关系、文化语境和世界观，推断最可能的人种/族裔/国别背景，并把这种推断稳定落实到 face_fingerprint、description、fingerprint、prompt_pos_zh 中。
    - 若主体是非人类生物、设备、器物、产品、工具、家具或载具，不要硬加国别/族裔/本地面孔，也不要为了凑字段去造“男性面孔”“女性面孔”。
    - 若输入已经明确或强烈暗示具体人类背景，就必须照此执行；若证据不足，不要擅自写死为东亚、欧美、拉丁或混血，只需保持与故事发生地和人物关系一致的本地面孔。
    - 对人类/类人主体，prompt_pos_zh 里不能只写“本地面孔”“都市男性面孔”“都市女性面孔”这类模糊词，必须显式写出与故事证据一致的国别/地区/族裔面孔短语；若当前 description、face_fingerprint 或 fingerprint 已经存在这类明确短语，prompt_pos_zh 必须原样继承。
    - 禁止把人类/类人主体漂成通用图库脸、默认欧美脸、无依据的混血脸或与故事语境不符的陌生族裔特征。
    - 这些 prompt 会直接提交给 ComfyUI 图像模型，prompt_pos_zh 必须是模型可执行的视觉短语，不要出现软件术语、字段名、规则说明、因为/所以式解释，也不要出现角色名字。
`
	frontFacingPrompt := `
【主体朝向与构图规则 (CRITICAL)】
    - 若主体是人类/类人角色：prompt_pos_zh 必须把主体锁定为正对镜头、正面站立、肩线朝向镜头、脸部正朝镜头、头部端正不歪、双眼看向镜头或正前方。
    - 若主体是非人类生物/动物/怪物：prompt_pos_zh 必须给出最能稳定识别主体的正面或正前偏角完整构图，让头部、主体轮廓、四肢/足爪/尾部完整可见；不要默认写成人类站姿。
    - 若主体是器物/设备/产品/工具/家具/载具：prompt_pos_zh 必须给出单体产品正视图或最稳定的前视识别角度，确保前面板、顶部结构、底部轮廓与关键部件完整可见；不要使用“正面站立”“肩线朝向镜头”这类人物词。
`
	noHandheldPrompt := `
【主体与外部持有关系规则 (CRITICAL)】
    - 若主体是人类/类人角色：这次输出的是标准角色基图，不是剧情镜头；prompt_pos_zh 绝对禁止把任何物件写成手持、手拿、手提、端着、举着、拄着或正在操作中的状态，人物双手必须空出来。手表、戒指、项链、耳饰、腰链、玉佩、刀鞘、背包、入鞘武器等，只能以佩戴、腰悬、背负、挂于身侧、入鞘或收纳状态出现。
    - 若主体是器物/设备/产品/工具/家具/载具：prompt_pos_zh 绝对禁止把主体写成被人物手持、被人物操作、被人物佩戴、放在人物手上、顶在人头上，或与人物同屏展示；它必须作为独立单体主体存在。
    - 若主体是非人类生物/动物/怪物：不要让人物、手、绳索、支架、抱持动作或手持动作进入主体展示图。
`
	subjectTypeUserPrompt := `
	    - 先判断当前主体是人类/类人角色、非人类生物/动物/怪物，还是器物/设备/产品/工具/家具/载具等非生命对象；绝不能默认所有主体都按人物模板输出。
	    - 只有人类/类人角色才允许使用“单人全身、full body shot、visible shoes、feet fully visible、正面站立、肩线朝向镜头、双手空出、国别/地区/族裔面孔”这类人体专属规则。
	    - 若主体是非人类生物/动物/怪物，prompt_pos 必须改成“单体完整生物基图”：主体完整可见、主体居中、头顶到足爪/尾端完整保留、纯白背景、无遮挡；不要写鞋子、肩线朝向镜头、双手空出、人物国别/族裔面孔。
	    - 若主体是器物/设备/产品/工具/家具/载具等非生命对象，prompt_pos 必须改成“单体产品/道具图”：主体完整可见、主体居中、顶部完整露出且上方留白、底部完整露出且下方留白、左右留白、纯白背景、无地面无墙面无阴影；不要写 full body shot、visible shoes、feet fully visible、正面站立、肩线朝向镜头、双手空出、人物面孔或人种/族裔。
	    - 若主体没有生物学脸部，face_fingerprint 改写成主体前视识别锚点，只写前面板、顶部结构、主轮廓、关键部件、接口、灯带、纹理等稳定识别点，不要硬造五官。
	    - 若主体不是人类/类人角色，gender 返回“其他”。
`
	localIdentityUserPrompt := `
	    - 只有当主体是人类/类人角色时，你才需要根据人物名字、剧情语境、职业身份、地点、时代、社会关系、文化语境和世界观，推断最可能的人种/族裔/国别背景，并把这种推断落实到 face_fingerprint、description、fingerprint 与 prompt_pos_zh 中。若主体是设备、器物、产品、工具、家具、载具或非人类生物，不要硬加国别/族裔/本地面孔。
	    - 对人类/类人主体，prompt_pos 必须显式写出与背景一致的国别/地区/族裔面孔短语，不能只写“本地面孔”这类模糊词；若当前 description、face_fingerprint 或 fingerprint 已经存在明确短语，就必须原样带入 prompt_pos。
	    - prompt_pos 必须按主体类型显式写出完整取景约束：人类/类人主体要有人物垂直居中、完整站立、头顶留白、鞋底留白；非人类生物要有主体完整可见；设备/器物/产品要有顶部完整露出、底部完整露出、左右留白。
	    - prompt_pos 必须是 ComfyUI 图像模型可执行的视觉短语，不要出现软件术语、字段名、规则说明、因为/所以式解释，也不要出现角色名字。
`
	frontFacingUserPrompt := `
	    - 人类/类人主体：prompt_pos 必须显式锁定正对镜头、正面站立、肩线朝向镜头、脸部正朝镜头、头部端正不歪、双眼看向镜头或正前方。
	    - 非人类生物/动物/怪物：prompt_pos 必须给出最能稳定识别主体的正面或正前偏角完整构图，不要默认写成人类站姿。
	    - 设备/器物/产品/工具/家具/载具：prompt_pos 必须给出单体产品正视图或最稳定的前视识别角度，确保前面板、顶部结构、底部轮廓与关键部件完整可见；不要使用“正面站立”“肩线朝向镜头”这类人物词。
`
	noHandheldUserPrompt := `
	    - 人类/类人主体：prompt_pos 必须明确双手空出，不允许任何手持、手拿、手提、端着、举着、拄着或正在操作中的物件；长期稳定装备与长期道具只能写成佩戴、腰悬、背负、挂于身侧、入鞘或收纳状态。
	    - 设备/器物/产品/工具/家具/载具：prompt_pos 必须明确它是独立单体主体，不能被人物手持、不能被人物操作、不能出现手模、人体或人物头部。
	    - 非人类生物/动物/怪物：prompt_pos 不允许出现抱持者、手、绳索、支架或人物陪体。
`
	positiveDetailPrompt := `
【正向提示词细化规则（最高优先级）】
    - 这条链路主要服务 Z-Image 等只看正向提示词的图像模型，因此 prompt_pos_zh 必须像可执行的视觉规格书，不能把稳定信息留给负向提示词兜底。
    - 人类/类人主体的服装必须拆解到具体层级：外套、内搭、领型、驳头或门襟、扣合方式、肩线、袖型、袖口、腰线、前片/后片、裤型或裙摆、鞋型、鞋跟/鞋底、材质纹理、缝线、金属件、配饰位置。
    - 若服装存在黑白相间、拼色、撞色、条纹、图案、徽记、压纹、透明层、镂空或不对称设计，必须写清左/右、前/后、上/下各区域分别是什么颜色、什么材质、什么结构；不能只写“黑白相间”“有拼接”“设计感西装”。
    - 设备/器物/产品/工具/家具/载具主体必须写成独立物体规格：前面板、顶部、底部、主轮廓、圆角/直角、网孔/格栅、灯带、接口、按键、接缝、底座、比例和材质；不要只写“智能音箱”“电脑”“电视”这种类别名。
    - 正向提示词必须优先写“看得见且会漂移的稳定事实”，不能依赖模型自己补全。
`
	subjectTypePrompt += positiveDetailPrompt
	subjectTypeUserPrompt += positiveDetailPrompt

	femaleFeaturePrompt := ""
	minorOrYouthFemaleGuard := ""
	if isFemaleCharacter(char.Gender) {
		femaleFeaturePrompt = `
【女性角色继承规则 (CRITICAL)】
    - 如果人物基础描述、face_fingerprint 与 fingerprint 已经明确该角色是女性，并给出了女性化的脸部、体态、服装或轮廓特征，你必须忠实继承这些信息，不要把她中性化或男性化。
    - 不要额外进行“精致化、曲线化、性感化、成熟化、美型化”强化；女性角色到底长什么样、穿什么、身材比例如何，应优先来自当前最新的人物基础描述、face_fingerprint 与 fingerprint。
    - 若描述本身没有强调某些胸线、曲线、锁骨、肩颈或女性面部细节，不要凭空追加或夸张强化。
`
		if isMinorOrYouthStageCharacter(char) {
			minorOrYouthFemaleGuard = `
【未成年 / 少女阶段女性角色限制规则 (CRITICAL)】
    - 该角色处于未成年或少女阶段，禁止进行性感化服装强化。
    - 禁止使用深V、爆乳、露背、极高开衩、胸下镂空、侧腰大面积开口、透明薄纱内透、包臀短裙、丝袜网纱、腿环、吊带高跟、露出乳沟等成人性感设计。
    - 仅允许保留清晰女性化五官、自然胸线、锁骨、肩颈线条、收腰与符合年龄身份的少女或年轻女性轮廓。
    - 服装应以得体、利落、符合角色身份和年龄阶段为准，不得带成人化性暗示。
`
		}
	}

	// Clothing Optimization Prompt
	optimizePrompt := ""
	optimizeUserOverridePrompt := ""
	if char.OptimizeClothing && isFemaleCharacter(char.Gender) && !isMinorOrYouthStageCharacter(char) && !isSeniorStageCharacter(char) {
		optimizePrompt = ""
		optimizeUserOverridePrompt = `
	    【成年女性角色服装优化规则 (CRITICAL)】
	    - 该规则只适用于明确成年女性角色，不适用于幼女段角色。
	    - 目标不是单一暴露，而是让服装更有设计感、更成熟、更有视觉冲击、更有角色辨识度。
	    - 必须强化女性身材线条：纤细腰线、修长双腿、明确的肩颈线条、胸腰臀曲线、优雅或危险感并存的体态。
	    - 服装请优先从以下方向组合与扩写：
	      - 吊带、细肩带、挂脖、露肩、斜肩、露背、深V、收腰、束胸、贴身胸衣、胸下镂空、侧腰开口
	      - 高开衩长裙、修身旗袍、贴身皮裙、短外套配内搭、半透明披帛、轻纱外层、蕾丝内搭、薄纱袖、包臀长裙、短裙配长靴
	      - 腰链、腿环、臂环、锁骨链、耳坠、珠链、金属饰扣、胸前垂饰、束腿绑带、手套、丝袜、网纱、过膝靴、绑带高跟鞋
	      - 发簪、额饰、头纱、颈圈、宝石胸针、流苏耳饰、金属腰封、腿部绑带、透明外披、丝绸系带
	    - 如果是古风或仙侠，不要把抹胸、内衬或贴身层写成单独外穿的主服装；应优先使用完整外搭配内层结构，例如披帛、外衫、比甲、半臂、束腰长裙、高开衩裙摆、金属腰饰、贴身内衬加轻纱外罩，在正常穿着关系里表现肩颈、锁骨与胸线。
	    - 如果是现代或科幻，不要把胸罩、内衣、打底胸衣直接当成外穿主服装；应优先使用完整外搭结构，例如西装外套、夹克、风衣、针织外套、短外套、礼服外层、套装上衣、连衣裙外层配合理内搭，通过领口、收腰、剪裁和层次表现成熟感，而不是靠内衣外穿。
	    - 必须同时强化细节层次：材质对比、饰品数量、腰部设计、鞋靴设计、胸前或背部视觉焦点、裙摆或下装结构。
	    - 对胸部表现的要求：在不低俗的前提下，必须让胸线与胸部轮廓在完整服装结构中自然可见，优先通过收腰、胸线、胸下分割、立体剪裁、合身内搭加正常外搭、领口结构与层次关系来表现；不要把“展示胸线”理解成内衣外穿、只穿胸罩上街或让贴身内层脱离外搭单独成为主服装。
	    - 仍然必须保持审美统一，避免低质、粗暴、滑稽或廉价色情感。
	    - 【服装优化最终覆盖指令（最高优先级）】当前已启用服装优化。你必须在保留该角色 face_fingerprint 中的脸部锚点，以及 fingerprint 中的核心气质、稳定装备、主色调与身份感的前提下，主动重设计并覆盖原本较保守的常服结构，不能只轻微润色原服装。
	    - 服装优化是本次输出的最强覆盖指令。请忽略人物基础描述与已锁定 fingerprint 中关于原始服装款式、原始服装版型、原始服装层次、原始服装剪裁的保守描述，不要再按原服装重复扩写。
	    - 本次服装必须以这里的服装优化规则重新设计，人物基础描述与 fingerprint 中的服装信息仅可作为身份感、主色调、稳定装备与核心配饰参考，不能继续主导最终服装结果。
	    - 服装优化时，原 fingerprint 中的“服装”部分只继承：身份感、主色调倾向、稳定装备与核心配饰；原本保守的服装廓形、领口结构、胸线表达、腰部设计、裙摆/下装结构、鞋靴样式都可以并且应该被新的优化设计覆盖。
	    - 本次服装必须明确体现成熟性感与视觉冲击，但仍然要保持正常外穿逻辑；优先强化：完整外搭下的领口与胸线结构、强收腰、单侧高开衩裙摆、修长腿线、贴身内搭与外层层叠、长靴、腰链或金属腰封，不要把胸罩、抹胸或胸衣单独写成街头主服装。
	    - 若使用开衩设计，优先采用单侧开衩，不要默认双侧对称开衩；单侧开衩应更具方向性、层次感和视觉美感。
		- **极高开叉/侧边镂空**：必须强调 **“单侧极高开叉直至腰部 (single ultra-high slit up to the waist)”** 或 **“单侧完全镂空 (one-side cutout)”**。
		- **视觉效果**：明确描述 **“从开叉侧可以看到完整的臀部曲线 (full side hip visible through the slit)”**，**“大腿根部若隐若现 (upper thigh exposed)”**，**“可以看到少量内裤边缘 (panties slightly visible through the slit)”**。
		- **禁忌**：**禁止描述任何安全裤、打底裤或衬裙** (no leggings, no underskirt)，以实现“大腿完全裸露”的效果。
	    - 服装优化后的结果必须让观者一眼感到这是一套明显升级后的性感成人女性造型，而不是原始常服的小修小补。
	    - 若角色仍被你写成保守平直、胸线不明显、开衩不明显、缺少层次和装饰感，视为错误输出。
	`
	}

	var systemPrompt, userPrompt string
	var llmProvider models.LLMProvider
	if char.UseRefImage {
		if err := db.DB.Where("is_active = ?", true).First(&llmProvider).Error; err != nil {
			return "", fmt.Errorf("no active LLM provider found")
		}
		// Reference Image Mode
		systemPrompt = fmt.Sprintf(`
你是一个【图像生成提示词扩写器】。
【下游生成模型 Comfyui】
  你需要返回的是能直接用于 ComfyUI 的中文正向提示词与主体资产描述。ComfyUI 不能理解某些内容，如角色名字、规则说明或软件术语，所以你需要根据我给到的内容进行推理，我最终要的是你扩写后的可执行正向提示词。

 【返回字段约定】
 %s
 
【任务目标】
    你需要编写一段基于Comfyui Qwen Editor能用的提示词用于后续场景合成的【人物素材基图】。
    相当于你的任务是我会给comfyui qwen editor传入一张参考图（全身正面照片），然后你再按照人物基础描述编写提示词，实现生成出来的人物有被参考图使用到，比如我要的最终效果就是，人物身材比例脸的样貌还有都是参考图的样子，但是服装、道具、妆容按照人物基础描述来进行生成。
    这张图必须是“干净的、去背景的、高质量的人物立绘”。
    
    %s

    %s

    %s

    %s

    【角色名称处理 (Character Naming)】
    - 角色名字如果包含年龄阶段，例如 "宁姚 (少年)"，你只需要理解其年龄阶段。
    - 不要自行把临时伤势、情绪或动作当成角色名字的一部分。
    - 若人物当前受伤、衣物破损、沾血、疲惫，这些都应来自人物基础描述并写进提示词，而不是依赖名字。

%s

    【鞋子与全身要求（CRITICAL · 判错级别）】
    - **必须生成全身像 (Full Body)**：画面必须包含从头到脚的完整人物。
    - **必须明确描述鞋子 (Visible Shoes)**：提示词中必须包含对鞋子/靴子的具体描述。
    - **禁止截断脚部**：如果生成的提示词导致画面截断脚部，视为严重错误。
    - 必须包含：visible shoes, full body shot, feet fully visible。

    【比例与背景判定规则（CRITICAL · 判错级别）】
    - 必须根据角色当前的人物基础描述与已锁定 fingerprint 写清符合人物设定的自然比例，并尽量明确写出身高（Height: XXX cm）。
    - 不要额外套用统一的“长腿、短上身、超模比例、强收腰、夸张曲线”模板；人物到底长什么样、穿什么、什么身材比例，应优先来自最新的人物基础描述、face_fingerprint 与 fingerprint。
    - 若人物基础描述已经明确是偏壮、偏瘦、微胖、发福、少年体态、成熟体态、修长、娇小、厚重、含胸、挺拔等，必须如实继承，不要擅自美化、标准化或改造成另一种 archetype。
    - 必须保持明确身高感、完整站姿和不被压缩的下半身，避免短腿、五五身、头身比例混乱。

    【肢体完整性规则（CRITICAL · 判错级别）】
    - 必须把人物写成单一、完整、解剖结构正确的人体。
    - 必须避免多余的肢体、多只手、多只脚、多根手臂、手指数量错误、手脚错位、肢体粘连、关节方向错误、身体部位重复。
    - prompt_pos 中应体现“肢体完整、人体结构稳定、手脚数量正确、双臂双腿清晰可见”的约束。
    - 必须直接把“肢体完整、双臂双腿数量正确、手脚结构正常、不出现重复身体部位”翻译进 prompt_pos，而不是依赖负向提示词兜底。

    【纯白背景硬性规则（CRITICAL）】
    - 背景必须为：Pure White Background。
    - 禁止出现：地面、阴影、投影、渐变、纹理、空间感、环境光。
    - 若出现任何背景元素，视为错误输出。
    - 你必须额外在最终 prompt_pos 中加入一段明确的全身构图追加约束：人物位于画面正中央，缩小一些呈现完整全身，头顶到鞋底完整保留在画面内，头部上方和双脚下方都保留明确留白，左右两侧也保留足够空白，绝不允许贴边构图，绝不允许半身、近景或中近景裁切。
    - 你必须额外在最终 prompt_pos 中加入一段明确的纯白背景追加约束：背景必须是纯白纯色背景纸效果，整张背景只有单一干净白色，不允许出现任何灰底、脏污、墙面感、空间层次、落地面、台面、地平线、投影边缘、环境反光或背景过渡，人物像棚拍证件级纯白底全身素材图一样清楚独立。

    你的任务：
    1) 基于当前最新的人物基础描述、已锁定 face_fingerprint 与已锁定 fingerprint，做结构化翻译与补全：description 负责完整稳定外观底模，face_fingerprint 负责脸部与基础发型，fingerprint 负责身形体态、固定服装、稳定装备与稳定气质；请忠实继承这些信息，不要主动重设计。
    2) **背景控制 (CRITICAL)**：生成的图片**必须是纯色背景（Pure White Background）**。禁止生成任何环境、光影背景、复杂的场景元素。
       - 原因：这张图后续会被抠图，背景越干净越好。
       - 不要把任何环境场景描述带入到这张图中。
    3) 输出必须详细，适合图像模型理解。
    4) 不要出现人物名字。
	    5) 严格执行语言约定：%s
	    6) 输出格式【必须是合法 JSON】。
	    7) 必须同时继承已锁定 face_fingerprint 中的脸部与基础发型锚点，以及已锁定 fingerprint 中的身形体态、服装主轮廓、稳定装备与饰品，不要把角色写成模板化路人。
	    7.1) 若人物基础描述或已锁定 fingerprint 已经被人工改写过服装、鞋靴、外搭、内搭、饰品、主副配色、版型或层次，你必须直接按最新内容重写提示词与角色锚点；不要因为当前未启用服装优化，就把服装强行退回旧版本。
	    7.2) 不要对人物样貌、脸型、体型、服装进行审美升级，只做结构化翻译与补全。
	    7.3) 长期稳定装备与长期道具只能呈现为佩戴、腰悬、背负、挂于身侧、入鞘或收纳状态；人物基图阶段禁止任何手持、手拿、手提、端着、举着、拄着或正在操作中的道具，不要把佩剑、长剑、飞剑、书卷、法器、包袋等写成握在手里。
	    8) 必须确保人物是单一、完整、肢体正确的人体，不允许出现多余肢体、手脚数量错误、手指异常、肢体错位。
	    9) 即使使用参考图锁脸，你仍必须强化角色脸部辨识逻辑：要让提示词明确保留并强调至少 5 项稳定脸部锚点，例如脸型轮廓、额头宽窄、眉形与眉尾走势、眼裂长度与眼距、眼尾方向、鼻梁与鼻头形态、唇形与唇峰、下颌线、肤色冷暖、发际线与鬓角处理；禁止只写“清秀、好看、冷艳、俊朗”这类模板化审美词。
	    10) 必须主动拉开与常见模板脸的差异，不要把人物写成统一的窄脸、长眼、薄唇、黑发默认美型脸；要根据已锁定 face_fingerprint，把五官结构差异写清写稳。
	    11) description、fingerprint、prompt_pos_zh 都不要主动加入“写实、超写实、真实皮肤、照片质感、摄影棚质感、写真感、photorealistic、realistic skin、电影级皮肤细节”这类整体风格词；整体风格会在下游 ComfyUI 最终提交时单独追加，不属于本轮 LLM 输入。

    **注意：不要输出人物名字。**
    prompt_pos 请直接写成适合图像模型理解的自然连续提示词，优先使用紧凑短句或逗号分隔视觉短语，不要强制分段标签。
    使用参考图时，不要重复长篇脸部说明，但仍要保留与角色身份、体型、服装、纯白背景、全身构图相关的关键锚点。
`, promptLangInstruction, subjectTypePrompt+femaleFeaturePrompt+minorOrYouthFemaleGuard+optimizePrompt, localIdentityPrompt, frontFacingPrompt, noHandheldPrompt, referenceImageControlPrompt, promptLangInstruction)

		userPrompt = fmt.Sprintf(`
    人物基础描述：
    %s
    已锁定脸部锚点（face_fingerprint，后续场景与视频都会继续复用，必须继承，不得改写）：
    %s
    已锁定角色非脸部指纹（fingerprint，后续场景与视频都会继续复用，必须继承，不得改写）：
    %s
    (姓名：%s，性别：%s)
    
    生成要求：
    - 人物基图
    - 全身像 (Full Body)，必须包含脚部和鞋子 (Must include feet and shoes)
    - **纯白背景 (Pure White Background)**，无任何杂物
	    - 必须同时继承上面的 face_fingerprint 与 fingerprint：face_fingerprint 负责脸型、眉眼、鼻梁、嘴唇、肤色、发际线与基础发型，fingerprint 负责身形体态、固定服装、稳定装备与稳定气质；请围绕这两份锚点扩写画面提示词，不要重新定义人物
	    - 如果人物基础描述或已锁定 fingerprint 里的服装、外搭、鞋靴、饰品、主副配色已经被人工修改，必须按最新版本直接生成，不要自动退回历史服装
	    - 服装应忠实继承当前最新的人物基础描述与已锁定 fingerprint；若原始输入缺少局部信息，可做结构化补全，但不要做审美升级
	    - 若角色身份天然带有长期装备或稳定道具，例如佩剑、刀鞘、法器、玉佩、香囊、书卷、耳饰、腰链等，必须写进提示词中，但只能写成佩戴、腰悬、背负、挂于身侧、入鞘或收纳状态；人物基图阶段禁止写成手里拿着
	    - 面部必须强化辨识度：眉形、眼型、鼻梁、唇形、肤色、发型、发饰至少覆盖其中四项
	    - 即使使用参考图，你也要明确强化脸部锚点，不要让人物落回模板脸；要尽量覆盖脸型、眉形、眼型、眼距、鼻梁鼻头、唇形、下颌线、肤色、发际线或鬓角中的至少五项
	    - 如果人物基础描述、face_fingerprint 与 fingerprint 已明确该角色为女性，必须忠实继承其中已有的女性化五官、体态和服装特征；不要中性化，也不要额外强化成与描述不符的夸张性感造型
	    - 不要对人物样貌、脸型、体型、服装进行审美升级，只做结构化翻译与补全
	%s
	%s
	%s
	%s
	%s
    
    请输出 JSON：
    {
      "face_fingerprint": "如果主体是人类/类人角色，返回脸部与基础发型长期锚点；如果主体没有生物学脸部，则改写为主体前视识别锚点，只写前面板、顶部结构、主轮廓、关键部件、接口、灯带、纹理等稳定识别点，请使用中文输出。",
      "description": "返回更新后的稳定主体描述：人类/类人角色写稳定底模；非人类生物或设备/器物/产品则写稳定外形、主轮廓、结构部件、材质、颜色、比例和识别性细节，不写临时状态，请使用中文输出。",
      "fingerprint": "返回更新后的高密度长期指纹：人类/类人角色写非脸部长期锚点；非人类生物或设备/器物/产品则改写成主体轮廓、主体结构、稳定部件/接口、稳定材质/状态外化，不要重复 face_fingerprint 内容，请使用中文输出。",
      "prompt_pos_zh": "请根据主体类型返回适合图像模型理解的中文自然连续提示词：如果是人类/类人角色，必须包含单人全身、人物垂直居中、头顶留白、脚底留白、可见鞋子、纯白背景纸、比例与身高、正对镜头、双手空出和与故事证据一致的国别/地区/族裔面孔短语；如果是非人类生物/动物/怪物，必须返回单体完整生物基图，主体完整可见、主体居中、头顶到足爪/尾端完整保留、纯白背景、无遮挡，不要写鞋子、肩线朝向镜头、双手空出或国别/族裔面孔；如果是设备/器物/产品/工具/家具/载具等非生命对象，必须返回单体产品/道具图，主体完整可见、主体居中、顶部完整露出且上方留白、底部完整露出且下方留白、左右留白、纯白背景、无地面无墙面无阴影，不要写 full body shot、visible shoes、feet fully visible、正面站立、肩线朝向镜头、双手空出、人物面孔、人种/族裔。",
      "player_desc": "返回与当前最新 description、face_fingerprint、fingerprint 一致的客观外观特征摘要，不得改写人物身份"
    }
`, char.Description, char.FaceFingerprint, char.Fingerprint, char.Name, char.Gender, subjectTypeUserPrompt, localIdentityUserPrompt, frontFacingUserPrompt, noHandheldUserPrompt, optimizeUserOverridePrompt)

	} else if char.OptimizeClothing {
		if err := db.DB.Where("is_active = ?", true).First(&llmProvider).Error; err != nil {
			return "", fmt.Errorf("no active LLM provider found")
		}
		systemPrompt = fmt.Sprintf(`
你是一个【图像生成提示词扩写器】。
你的任务是基于“自动剧情人物生成”的同一套角色设计标准，返回可直接用于 ComfyUI Qwen Image / Z-Image 的人物基图提示词。

【返回字段约定】
%s

【核心原则】
1. 以自动剧情人物生成规则为准：当前角色只服务这一套固定造型锚点，不要重新发明新角色。
2. 这次任务是“人物二次推理（服装优化）”，基础人物锚点、脸部结构、体型、稳定装备、稳定气质必须继续继承自动剧情的人物设定；只有服装结构允许在规则内优化。
3. prompt_pos_zh 只服务“单人、全身、纯白背景、可抠图复用的人物素材基图”，不要写剧情、地名、对白、临时伤势、临时情绪。
4. 优先写可见锚点，不要用抽象词收尾。重点写：脸型、眉眼、鼻梁、嘴唇、肤色、发型、服装结构、主副配色、腰部结构、鞋靴、武器、饰品、站姿与身体轮廓。
5. 这张图必须是干净的纯白背景全身人物素材图，后续用于场景合成与参考图复用。
6. description、fingerprint、prompt_pos_zh 都不要主动加入“写实、超写实、真实皮肤、照片质感、摄影棚质感、写真感、photorealistic、realistic skin、电影级皮肤细节”这类整体风格词；整体风格会在下游 ComfyUI 最终提交时单独追加，不属于本轮 LLM 输入。

【人物基图硬性规则】
1. 必须是单人、全身、纯白背景、无遮挡、肢体完整、鞋子清晰可见、脚部完整可见。
2. 必须明确写出完整站立姿态，优先自然站姿，可写“一只脚略向前、身体比例清晰可见、方便观察整体比例”。
3. 必须避免多余肢体、多只手、多只脚、多手指、少手指、手脚畸形、肢体错位、身体部位重复。
4. 背景必须是纯白纯色背景纸效果，不允许灰底、渐变、空间层次、地面、台面、墙面、地平线、投影边缘、环境反光。
5. 人物必须位于画面正中央，缩小一些呈现完整全身，头顶到鞋底完整保留在画面内，头部上方和双脚下方都保留明确留白，左右两侧也保留足够空白，绝不允许贴边构图，绝不允许半身、近景或中近景裁切。

【比例规则】
1. 必须根据角色当前的人物基础描述与已锁定 fingerprint 写清符合人物设定的自然比例，并尽量明确写出身高（Height: XXX cm）。
2. 不要额外套用统一的“长腿、短上身、超模比例、强收腰、夸张曲线”模板；人物到底长什么样、穿什么、什么身材比例，应优先来自最新的人物基础描述、face_fingerprint 与 fingerprint。
3. 若人物基础描述已经明确是偏壮、偏瘦、微胖、发福、少年体态、成熟体态、修长、娇小、厚重、含胸、挺拔等，必须如实继承，不要擅自美化、标准化或改造成另一种 archetype。
4. 必须保持明确身高感、完整站姿和不被压缩的下半身，避免短腿、五五身、头身比例混乱。

【脸部与造型继承规则】
1. 必须同时从已锁定 face_fingerprint 中继承脸部与基础发型辨识点，并从已锁定 fingerprint 中继承身形体态、固定服装主轮廓、稳定装备与饰品。
1.1 长期稳定装备与长期道具只能呈现为佩戴、腰悬、背负、挂于身侧、入鞘或收纳状态；人物基图阶段禁止任何手持、手拿、手提、端着、举着、拄着或正在操作中的道具，不要把佩剑、长剑、飞剑、书卷、法器、包袋等写成握在手里。
2. 人物脸部必须强化辨识度，稳定写清 5 到 7 项脸部锚点，例如：脸型轮廓、额头宽窄、眉形与眉尾走势、眼裂长度、眼距、眼尾方向、鼻梁高低、鼻头形状、唇形与唇峰、下颌线、肤色冷暖、发际线与鬓角。
3. 禁止把人物写成统一的窄脸、长眼、薄唇、黑发默认美型脸；重点是五官结构差异，而不是泛泛气质词。
4. 不要把 prompt 的主体信息停留在“压场感强、守城气质、精英感、强者感、核心感”这类抽象判断上；如果需要表达气质，必须翻译成可直接画出来的视觉特征。
5. description、fingerprint、prompt_pos_zh 都不要主动加入“写实、超写实、真实皮肤、照片质感、摄影棚质感、写真感、photorealistic、realistic skin、电影级皮肤细节”这类整体风格词；整体风格会在下游 ComfyUI 最终提交时单独追加，不属于本轮 LLM 输入。

%s

%s

%s

%s

【输出要求】
1. 输出必须是合法 JSON。
2. 你生成的 face_fingerprint、description 与 fingerprint 必须继续服务后续场景与视频复用：face_fingerprint 负责脸部与基础发型稳定一致，fingerprint 负责非脸部长期锚点稳定一致。
3. prompt_pos 请直接写成适合图像模型理解的自然连续提示词，优先使用紧凑短句或逗号分隔视觉短语，不要强制使用标签分段结构。
`, promptLangInstruction, subjectTypePrompt+femaleFeaturePrompt+minorOrYouthFemaleGuard+optimizeUserOverridePrompt, localIdentityPrompt, frontFacingPrompt, noHandheldPrompt)

		userPrompt = fmt.Sprintf(`
人物基础描述：
%s

已锁定脸部锚点（face_fingerprint，后续场景与视频都会继续复用，必须继承，不得改写）：
%s

已锁定角色非脸部指纹（fingerprint，后续场景与视频都会继续复用，必须继承，不得改写）：
%s

(姓名：%s，性别：%s)

本次任务是：在“自动剧情人物生成”的同一套规则下，重新输出一版更适合人物基图生成的提示词；只有服装部分允许按服装优化规则升级，其他角色锚点必须稳定继承。

请严格遵守：
1. 角色仍然是当前这一个角色，不要重新定义身份，不要改脸，不要改体型方向，不要把他/她改成另一种 archetype。
2. description 与 fingerprint 继续只保留长期稳定底模，不写临时状态。
3. prompt_pos 必须优先写可见锚点，而不是抽象气质词。
4. prompt_pos 必须是单人、全身、纯白背景、完整站姿、鞋子完整可见、脚部完整可见、肢体完整。
5. 比例必须符合角色体型，并直接继承当前最新人物基础描述与已锁定 fingerprint 里的身材方向；不要额外套用统一长腿模板、统一精英模板或统一女性曲线模板。
6. 如果有稳定装备、饰品、武器或长期道具，必须继续保留。
6.1 这些长期稳定装备与长期道具，只能写成佩戴、腰悬、背负、挂于身侧、入鞘或收纳状态；人物基图阶段禁止写成手里拿着。
7. 如果是女性角色，本次只是在自动剧情人物规则末尾追加服装优化，不允许把旧服装优化 prompt 里的过时逻辑反过来主导整个人物。
%s
%s
%s
%s
%s

请输出 JSON：
{
  "face_fingerprint": "如果主体是人类/类人角色，返回脸部与基础发型长期锚点；如果主体没有生物学脸部，则改写为主体前视识别锚点，只写前面板、顶部结构、主轮廓、关键部件、接口、灯带、纹理等稳定识别点，请使用中文输出。",
  "description": "返回更新后的稳定主体描述：人类/类人角色写稳定底模；非人类生物或设备/器物/产品则写稳定外形、主轮廓、结构部件、材质、颜色、比例和识别性细节，不写临时状态，请使用中文输出。",
  "fingerprint": "返回更新后的高密度长期指纹：人类/类人角色写非脸部长期锚点；非人类生物或设备/器物/产品则改写成主体轮廓、主体结构、稳定部件/接口、稳定材质/状态外化，不要重复 face_fingerprint 内容，请使用中文输出。",
  "prompt_pos_zh": "请根据主体类型返回适合图像模型理解的中文自然连续提示词：如果是人类/类人角色，必须包含单人全身、人物垂直居中、头顶留白、脚底留白、可见鞋子、纯白背景纸、比例与身高、正对镜头、双手空出和与故事证据一致的国别/地区/族裔面孔短语；如果是非人类生物/动物/怪物，必须返回单体完整生物基图，主体完整可见、主体居中、头顶到足爪/尾端完整保留、纯白背景、无遮挡，不要写鞋子、肩线朝向镜头、双手空出或国别/族裔面孔；如果是设备/器物/产品/工具/家具/载具等非生命对象，必须返回单体产品/道具图，主体完整可见、主体居中、顶部完整露出且上方留白、底部完整露出且下方留白、左右留白、纯白背景、无地面无墙面无阴影，不要写 full body shot、visible shoes、feet fully visible、正面站立、肩线朝向镜头、双手空出、人物面孔、人种/族裔。",
  "player_desc": "返回与当前最新 description、face_fingerprint、fingerprint 一致的客观外观特征摘要，不得改写人物身份"
}
`, char.Description, char.FaceFingerprint, char.Fingerprint, char.Name, char.Gender, subjectTypeUserPrompt, localIdentityUserPrompt, frontFacingUserPrompt, noHandheldUserPrompt, optimizeUserOverridePrompt)
	} else {
		if err := db.DB.Where("is_active = ?", true).First(&llmProvider).Error; err != nil {
			return "", fmt.Errorf("no active LLM provider found")
		}
		systemPrompt = fmt.Sprintf(`
你是一个【图像生成提示词扩写器】。
你的任务是基于当前角色最新的人物基础描述、已锁定 face_fingerprint 与已锁定 fingerprint，返回可直接用于 ComfyUI Qwen Image / Z-Image 的人物基图提示词。

【返回字段约定】
%s

【核心原则】
1. 当前角色仍然是同一个 name，不要重新发明新角色。
2. 这次任务是“人物二次推理（标准强化）”，你必须根据当前最新的人物基础描述、已锁定 face_fingerprint 与已锁定 fingerprint，重新输出更适合人物基图生成的 description、fingerprint 与 prompt_pos。
3. 如果人物基础描述或已锁定 fingerprint 里的服装、外搭、内搭、鞋靴、饰品、主副配色、版型或层次已经被人工修改，你必须按最新版本直接生成，不要退回历史服装。
3.1 不要对人物样貌、脸型、体型、服装进行审美升级，只做结构化翻译与补全。
4. prompt_pos_zh 只服务单人、全身、纯白背景、可抠图复用的人物素材基图，不要写剧情、地名、对白、临时伤势、临时动作。
5. 优先写可见锚点，不要用抽象词收尾。重点写：脸型、眉眼、鼻梁、嘴唇、肤色、发型、服装结构、主副配色、腰部结构、鞋靴、武器、饰品、站姿与身体轮廓。
6. description、fingerprint、prompt_pos_zh 都不要主动加入“写实、超写实、真实皮肤、照片质感、摄影棚质感、写真感、photorealistic、realistic skin、电影级皮肤细节”这类整体风格词；整体风格会在下游 ComfyUI 最终提交时单独追加，不属于本轮 LLM 输入。

【人物基图硬性规则】
1. 必须是单人、全身、纯白背景、无遮挡、肢体完整、鞋子清晰可见、脚部完整可见。
2. 必须明确写出完整站立姿态，优先自然站姿，可写“一只脚略向前、身体比例清晰可见、方便观察整体比例”。
3. 必须避免多余肢体、多只手、多只脚、多手指、少手指、手脚畸形、肢体错位、身体部位重复。
4. 背景必须是纯白纯色背景纸效果，不允许灰底、渐变、空间层次、地面、台面、墙面、地平线、投影边缘、环境反光。
5. 人物必须位于画面正中央，缩小一些呈现完整全身，头顶到鞋底完整保留在画面内，头部上方和双脚下方都保留明确留白，左右两侧也保留足够空白，绝不允许贴边构图，绝不允许半身、近景或中近景裁切。

【比例规则】
1. 必须根据角色当前的人物基础描述与已锁定 fingerprint 写清符合人物设定的自然比例，并尽量明确写出身高（Height: XXX cm）。
2. 不要额外套用统一的“长腿、短上身、超模比例、强收腰、夸张曲线”模板；人物到底长什么样、穿什么、什么身材比例，应优先来自最新的人物基础描述、face_fingerprint 与 fingerprint。
3. 若人物基础描述已经明确是偏壮、偏瘦、微胖、发福、少年体态、成熟体态、修长、娇小、厚重、含胸、挺拔等，必须如实继承，不要擅自美化、标准化或改造成另一种 archetype。
4. 必须保持明确身高感、完整站姿和不被压缩的下半身，避免短腿、五五身、头身比例混乱。

【脸部与造型继承规则】
1. 必须同时从已锁定 face_fingerprint 中继承脸部与基础发型辨识点，并从已锁定 fingerprint 中继承身形体态、固定服装主轮廓、稳定装备与饰品。
1.1 长期稳定装备与长期道具只能呈现为佩戴、腰悬、背负、挂于身侧、入鞘或收纳状态；人物基图阶段禁止任何手持、手拿、手提、端着、举着、拄着或正在操作中的道具，不要把佩剑、长剑、飞剑、书卷、法器、包袋等写成握在手里。
2. 人物脸部必须强化辨识度，稳定写清 5 到 7 项脸部锚点，例如：脸型轮廓、额头宽窄、眉形与眉尾走势、眼裂长度、眼距、眼尾方向、鼻梁高低、鼻头形状、唇形与唇峰、下颌线、肤色冷暖、发际线与鬓角。
3. 禁止把人物写成统一的窄脸、长眼、薄唇、黑发默认美型脸；重点是五官结构差异，而不是泛泛气质词。
4. 不要把 prompt 的主体信息停留在“精英感、压场感、强者感、核心感”这类抽象判断上；如果需要表达气质，必须翻译成可直接画出来的视觉特征。

%s

%s

%s

%s

【输出要求】
1. 输出必须是合法 JSON。
2. 你生成的 face_fingerprint、description 与 fingerprint 必须继续服务后续场景与视频复用：face_fingerprint 负责脸部与基础发型稳定一致，fingerprint 负责非脸部长期锚点稳定一致。
3. prompt_pos 请直接写成适合图像模型理解的自然连续提示词，优先使用紧凑短句或逗号分隔视觉短语，不要强制使用标签分段结构。
`, promptLangInstruction, subjectTypePrompt+femaleFeaturePrompt+minorOrYouthFemaleGuard, localIdentityPrompt, frontFacingPrompt, noHandheldPrompt)

		userPrompt = fmt.Sprintf(`
人物基础描述：
%s

已锁定脸部锚点（face_fingerprint，后续场景与视频都会继续复用，必须继承，不得改写）：
%s

已锁定角色非脸部指纹（fingerprint，后续场景与视频都会继续复用，必须继承，不得改写）：
%s

(姓名：%s，性别：%s)

本次任务是：根据当前最新的人物基础描述、已锁定 face_fingerprint 与已锁定 fingerprint，重新输出一版更适合人物基图生成的提示词。只要当前描述或 fingerprint 里已经改过服装、鞋靴、外搭、内搭、饰品、主副配色或版型，就直接按最新版本生成，不要退回旧服装。

请严格遵守：
1. 角色仍然是当前这一个角色，不要重新定义身份，不要改脸，不要改体型方向，不要把他/她改成另一种 archetype。
2. description 与 fingerprint 继续只保留长期稳定底模，不写临时状态。
3. prompt_pos 必须优先写可见锚点，而不是抽象气质词。
4. prompt_pos 必须是单人、全身、纯白背景、完整站姿、鞋子完整可见、脚部完整可见、肢体完整。
5. 比例必须符合角色体型，并直接继承当前最新人物基础描述与已锁定 fingerprint 里的身材方向；不要额外套用统一长腿模板、统一精英模板或统一女性曲线模板。
6. 如果有稳定装备、饰品、武器或长期道具，必须继续保留。
6.1 这些长期稳定装备与长期道具，只能写成佩戴、腰悬、背负、挂于身侧、入鞘或收纳状态；人物基图阶段禁止写成手里拿着。
7. 若当前最新描述里已经调整了服装设计，你必须把新的服装直接写进 prompt_pos、description 和 fingerprint，不要认为只有启用服装优化才可以改服装。
%s
%s
%s
%s

请输出 JSON：
{
  "face_fingerprint": "如果主体是人类/类人角色，返回脸部与基础发型长期锚点；如果主体没有生物学脸部，则改写为主体前视识别锚点，只写前面板、顶部结构、主轮廓、关键部件、接口、灯带、纹理等稳定识别点，请使用中文输出。",
  "description": "返回更新后的稳定主体描述：人类/类人角色写稳定底模；非人类生物或设备/器物/产品则写稳定外形、主轮廓、结构部件、材质、颜色、比例和识别性细节，不写临时状态，请使用中文输出。",
  "fingerprint": "返回更新后的高密度长期指纹：人类/类人角色写非脸部长期锚点；非人类生物或设备/器物/产品则改写成主体轮廓、主体结构、稳定部件/接口、稳定材质/状态外化，不要重复 face_fingerprint 内容，请使用中文输出。",
  "prompt_pos_zh": "请根据主体类型返回适合图像模型理解的中文自然连续提示词：如果是人类/类人角色，必须包含单人全身、人物垂直居中、头顶留白、脚底留白、可见鞋子、纯白背景纸、比例与身高、正对镜头、双手空出和与故事证据一致的国别/地区/族裔面孔短语；如果是非人类生物/动物/怪物，必须返回单体完整生物基图，主体完整可见、主体居中、头顶到足爪/尾端完整保留、纯白背景、无遮挡，不要写鞋子、肩线朝向镜头、双手空出或国别/族裔面孔；如果是设备/器物/产品/工具/家具/载具等非生命对象，必须返回单体产品/道具图，主体完整可见、主体居中、顶部完整露出且上方留白、底部完整露出且下方留白、左右留白、纯白背景、无地面无墙面无阴影，不要写 full body shot、visible shoes、feet fully visible、正面站立、肩线朝向镜头、双手空出、人物面孔、人种/族裔。",
  "player_desc": "返回与当前最新 description、face_fingerprint、fingerprint 一致的客观外观特征摘要，不得改写人物身份"
}
`, char.Description, char.FaceFingerprint, char.Fingerprint, char.Name, char.Gender, subjectTypeUserPrompt, localIdentityUserPrompt, frontFacingUserPrompt, noHandheldUserPrompt)
	}

	// 3. Call LLM
	Log(LogLevelInfo, llmLogMessage("LLM Request", llmProvider), fmt.Sprintf("Starting prompt generation for character: %s", char.Name))

	// Debug: Log the actual prompt sent to LLM
	Log(LogLevelInfo, llmLogMessage("LLM Request Prompt", llmProvider), fmt.Sprintf("System: %s\n\nUser: %s", systemPrompt, userPrompt))

	llmResponse, err := callLLMCharacterPrompt(llmProvider, systemPrompt, userPrompt, taskID)
	if err != nil {
		Log(LogLevelError, llmLogMessage("LLM Error", llmProvider), fmt.Sprintf("LLM call failed: %v", err))
		return "", fmt.Errorf("LLM调用失败: %v", err)
	}
	Log(LogLevelInfo, llmLogMessage("LLM Success", llmProvider), "Prompt generated successfully")

	// 4. Update Database
	// Character fingerprint is locked by auto-generate and should not be overwritten here.
	// This stage only generates prompts around the existing character anchor.
	if strings.TrimSpace(llmResponse.FaceFingerprint) != "" {
		char.FaceFingerprint = strings.TrimSpace(llmResponse.FaceFingerprint)
	}
	if strings.TrimSpace(llmResponse.Description) != "" {
		char.Description = strings.TrimSpace(llmResponse.Description)
	}
	if strings.TrimSpace(llmResponse.Fingerprint) != "" {
		char.Fingerprint = strings.TrimSpace(llmResponse.Fingerprint)
	}
	char.PositivePrompt = marshalLocalizedPromptText(llmResponse.PromptPosZH, "")
	char.NegativePrompt = marshalLocalizedPromptText(llmResponse.PromptNegZH, "")
	char.Status = "generated" // Mark as generated (prompt ready)

	if err := db.DB.Save(&char).Error; err != nil {
		return "", fmt.Errorf("failed to update character: %v", err)
	}
	promptID, err := triggerCharacterImageGeneration(char)
	if err != nil {
		return "", fmt.Errorf("failed to queue image generation: %v", err)
	}

	return promptID, nil
}

// waitForImageCompletion waits until the ComfyUI task is finished (used for batch processing)
func waitForImageCompletion(promptID string) error {
	// Timeout after 20 minutes
	timeout := time.After(20 * time.Minute)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("image generation timeout")
		case <-ticker.C:
			history, err := GetComfyHistory(promptID)
			if err == nil && history != nil {
				// Task finished (successful or not, history exists means it's done or failed)
				// We check if outputs exist to be sure
				if _, ok := history["outputs"]; ok {
					return nil
				}
				// If status is failed/cancelled, history might be different?
				// For now, existence of history entry usually means completion.
				// Let's assume done.
				return nil
			}
			// If error, it might mean not found yet (still running or queued)
		}
	}
}
