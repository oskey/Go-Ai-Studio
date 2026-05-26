package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"
	"kt-ai-studio/internal/workflow"

	"github.com/gin-gonic/gin"
)

// ListScenes retrieves all scenes for a project
func ListScenes(c *gin.Context) {
	projectID := c.Param("id")
	summaryMode := strings.TrimSpace(c.Query("summary"))
	if summaryMode == "episodes" {
		type episodeSummary struct {
			Episode int   `json:"episode"`
			Count   int64 `json:"count"`
		}
		var summaries []episodeSummary
		if err := db.DB.Model(&models.Scene{}).
			Select("episode, COUNT(*) as count").
			Where("project_id = ?", projectID).
			Group("episode").
			Order("episode asc").
			Scan(&summaries).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch scene summaries"})
			return
		}
		c.JSON(http.StatusOK, summaries)
		return
	}

	episodeFilter := strings.TrimSpace(c.Query("episode"))
	offsetValue := strings.TrimSpace(c.Query("offset"))
	limitValue := strings.TrimSpace(c.Query("limit"))
	var project models.Project
	if err := db.DB.Select("id", "code").First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}
	var scenes []models.Scene
	query := db.DB.Preload("Characters").Where("project_id = ?", projectID)
	if episodeFilter != "" {
		episode, err := strconv.Atoi(episodeFilter)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid episode"})
			return
		}
		query = query.Where("episode = ?", episode)
	}
	if episodeFilter != "" && (offsetValue != "" || limitValue != "") {
		offset := 0
		if offsetValue != "" {
			parsedOffset, err := strconv.Atoi(offsetValue)
			if err != nil || parsedOffset < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset"})
				return
			}
			offset = parsedOffset
		}

		limit := 5
		if limitValue != "" {
			parsedLimit, err := strconv.Atoi(limitValue)
			if err != nil || parsedLimit <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
				return
			}
			limit = parsedLimit
		}

		var total int64
		if err := query.Model(&models.Scene{}).Count(&total).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count scenes"})
			return
		}
		if err := query.Order("episode asc, scene_number asc").Offset(offset).Limit(limit).Find(&scenes).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch scenes"})
			return
		}
		reconcileSceneImagesFromDisk(project.Code, scenes)
		c.JSON(http.StatusOK, gin.H{
			"items":    scenes,
			"total":    total,
			"offset":   offset,
			"limit":    limit,
			"has_more": int64(offset+len(scenes)) < total,
		})
		return
	}
	// Preload Characters to show bound characters in list
	if err := query.Order("episode asc, scene_number asc").Find(&scenes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch scenes"})
		return
	}
	reconcileSceneImagesFromDisk(project.Code, scenes)
	c.JSON(http.StatusOK, scenes)
}

func reconcileSceneImagesFromDisk(projectCode string, scenes []models.Scene) {
	if strings.TrimSpace(projectCode) == "" || len(scenes) == 0 {
		return
	}
	for i := range scenes {
		reconciledPath, ok := findLatestSceneImagePath(projectCode, scenes[i].ID)
		if !ok {
			continue
		}
		if strings.TrimSpace(scenes[i].GeneratedImage) == reconciledPath && strings.TrimSpace(scenes[i].Status) == "generated" {
			continue
		}
		scenes[i].GeneratedImage = reconciledPath
		scenes[i].Status = "generated"
		scenes[i].UpdatedAt = time.Now()
		if err := db.DB.Model(&models.Scene{}).
			Where("id = ?", scenes[i].ID).
			Updates(map[string]interface{}{
				"generated_image": reconciledPath,
				"image_status":    "generated",
				"updated_at":      scenes[i].UpdatedAt,
			}).Error; err == nil {
		}
	}
}

func resetSceneImageState(scene *models.Scene) error {
	if scene == nil {
		return fmt.Errorf("scene is nil")
	}
	if err := removeGeneratedAsset(scene.GeneratedImage); err != nil {
		return err
	}
	scene.GeneratedImage = ""
	scene.GeneratedWorkflow = ""
	scene.Status = "draft"
	scene.UpdatedAt = time.Now()
	return db.DB.Save(scene).Error
}

func findLatestSceneImagePath(projectCode string, sceneID uint) (string, bool) {
	pattern := filepath.Join("output", projectCode, "scenes", fmt.Sprintf("scene_%d_*.png", sceneID))
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return "", false
	}
	latestPath := ""
	var latestModTime time.Time
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil || info.IsDir() {
			continue
		}
		if latestPath == "" || info.ModTime().After(latestModTime) {
			latestPath = match
			latestModTime = info.ModTime()
		}
	}
	if latestPath == "" {
		return "", false
	}
	return "/" + filepath.ToSlash(latestPath), true
}

func applySceneImageSizeDefaults(scene *models.Scene) bool {
	if scene == nil {
		return false
	}

	defaultWidth, defaultHeight := getConfiguredSceneImageSize()
	updated := false
	if scene.Width <= 0 {
		scene.Width = defaultWidth
		updated = true
	}
	if scene.Height <= 0 {
		scene.Height = defaultHeight
		updated = true
	}
	return updated
}

// AddScene adds a new scene
func AddScene(c *gin.Context) {
	var request struct {
		Scene        models.Scene `json:"scene"`
		CharacterIDs []uint       `json:"character_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		Log(LogLevelError, "绑定场景数据失败", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid data format: %v", err)})
		return
	}
	scene := request.Scene
	scene.Name = strings.TrimSpace(scene.Name)
	scene.Description = strings.TrimSpace(scene.Description)
	scene.ImagePrompt = strings.TrimSpace(scene.ImagePrompt)
	scene.VideoPrompt = strings.TrimSpace(scene.VideoPrompt)
	scene.Narration = strings.TrimSpace(scene.Narration)
	scene.Dialogue = ""
	if scene.SceneID <= 0 && scene.SceneNumber > 0 {
		scene.SceneID = scene.SceneNumber
	}
	if scene.SceneNumber <= 0 && scene.SceneID > 0 {
		scene.SceneNumber = scene.SceneID
	}
	scene.DurationSeconds = scene.DurationSeconds
	if scene.ImagePrompt != "" {
		scene.PositivePrompt = marshalLocalizedPromptText(scene.ImagePrompt, "")
		scene.NegativePrompt = ""
	}
	if scene.VideoPrompt != "" {
		if scene.DurationSeconds < minVideoTotalDurationSeconds {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("duration_seconds must be at least %d", minVideoTotalDurationSeconds)})
			return
		}
		videoFingerprint, err := buildLegacyVideoFingerprintFromPrompt(scene.VideoPrompt, scene.DurationSeconds)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid video_prompt: %v", err)})
			return
		}
		scene.VideoFingerprint = videoFingerprint
	} else {
		scene.VideoFingerprint = strings.TrimSpace(scene.VideoFingerprint)
	}
	scene.Width = 0
	scene.Height = 0
	scene.Seed = 0
	scene.CreatedAt = time.Now()
	scene.UpdatedAt = time.Now()

	// Handle many-to-many relationship
	if len(request.CharacterIDs) > 0 {
		var chars []models.Character
		db.DB.Find(&chars, request.CharacterIDs)
		scene.Characters = chars
	}

	if err := db.DB.Create(&scene).Error; err != nil {
		Log(LogLevelError, "创建场景失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create scene"})
		return
	}

	Log(LogLevelInfo, "创建场景", fmt.Sprintf("创建了新场景: %s (第%d集 第%d镜)", scene.Name, scene.Episode, scene.SceneNumber))
	c.JSON(http.StatusCreated, scene)
}

// UpdateScene updates an existing scene
func UpdateScene(c *gin.Context) {
	id := c.Param("sceneId")
	var scene models.Scene
	if err := db.DB.Preload("Characters").First(&scene, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Scene not found"})
		return
	}

	var request struct {
		Scene        models.Scene `json:"scene"`
		CharacterIDs []uint       `json:"character_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update fields
	updateData := request.Scene
	scene.Episode = updateData.Episode
	scene.SceneID = updateData.SceneID
	scene.SceneNumber = updateData.SceneNumber
	if scene.SceneID <= 0 && scene.SceneNumber > 0 {
		scene.SceneID = scene.SceneNumber
	}
	if scene.SceneNumber <= 0 && scene.SceneID > 0 {
		scene.SceneNumber = scene.SceneID
	}
	if updateData.DurationSeconds > 0 {
		scene.DurationSeconds = updateData.DurationSeconds
	}
	scene.Name = strings.TrimSpace(updateData.Name)
	scene.Description = strings.TrimSpace(updateData.Description)
	scene.ImagePrompt = strings.TrimSpace(updateData.ImagePrompt)
	scene.VideoPrompt = strings.TrimSpace(updateData.VideoPrompt)
	scene.Narration = strings.TrimSpace(updateData.Narration)
	scene.BackgroundAudio = updateData.BackgroundAudio
	scene.Dialogue = ""
	scene.Fingerprint = ""
	if scene.ImagePrompt != "" {
		scene.PositivePrompt = marshalLocalizedPromptText(scene.ImagePrompt, "")
		scene.NegativePrompt = ""
	} else {
		scene.PositivePrompt = updateData.PositivePrompt
		scene.NegativePrompt = updateData.NegativePrompt
	}
	if scene.VideoPrompt != "" {
		if scene.DurationSeconds < minVideoTotalDurationSeconds {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("duration_seconds must be at least %d", minVideoTotalDurationSeconds)})
			return
		}
		videoFingerprint, err := buildLegacyVideoFingerprintFromPrompt(scene.VideoPrompt, scene.DurationSeconds)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid video_prompt: %v", err)})
			return
		}
		scene.VideoFingerprint = videoFingerprint
	} else {
		scene.VideoFingerprint = strings.TrimSpace(updateData.VideoFingerprint)
	}
	scene.Width = 0
	scene.Height = 0
	scene.Seed = 0
	scene.UpdatedAt = time.Now()

	// Update associations
	if len(request.CharacterIDs) >= 0 { // Allow empty list to clear
		var chars []models.Character
		if len(request.CharacterIDs) > 0 {
			db.DB.Find(&chars, request.CharacterIDs)
		}
		if err := db.DB.Model(&scene).Association("Characters").Replace(chars); err != nil {
			Log(LogLevelError, "更新场景关联角色失败", err.Error())
		}
	}

	if err := db.DB.Save(&scene).Error; err != nil {
		Log(LogLevelError, "更新场景失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update scene"})
		return
	}

	Log(LogLevelInfo, "更新场景", fmt.Sprintf("更新了场景: %s", scene.Name))
	c.JSON(http.StatusOK, scene)
}

// DeleteScene deletes a scene
func DeleteScene(c *gin.Context) {
	id := c.Param("sceneId")
	var scene models.Scene
	if err := db.DB.First(&scene, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Scene not found"})
		return
	}

	if err := deleteShotWithAssets(scene.ID); err != nil {
		Log(LogLevelError, "删除场景失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete scene"})
		return
	}

	Log(LogLevelInfo, "删除场景", fmt.Sprintf("删除场景 ID: %s", id))
	c.JSON(http.StatusOK, gin.H{"message": "Scene deleted successfully"})
}

// AutoGenerateAllScenes handles the request to generate base images for all scenes in a project
func AutoGenerateAllScenes(c *gin.Context) {
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

	t, err := task.GlobalTaskManager.AddTask("batch_generate_scenes", taskPayload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to submit task"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Batch scene generation task submitted", "task_id": t.ID})
}

// DeleteAllSceneImages removes all generated scene images for a project
func DeleteAllSceneImages(c *gin.Context) {
	projectID := c.Param("id")

	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	var scenes []models.Scene
	if err := db.DB.Where("project_id = ?", project.ID).Find(&scenes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch scenes"})
		return
	}

	resetCount := 0
	for _, scene := range scenes {
		if strings.TrimSpace(scene.GeneratedImage) == "" &&
			strings.TrimSpace(scene.GeneratedWorkflow) == "" &&
			strings.TrimSpace(scene.Status) == "draft" {
			continue
		}
		if err := resetSceneImageState(&scene); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update scene status"})
			return
		}
		BroadcastUpdate("scene", scene.ID)
		resetCount++
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Scene images deleted successfully",
		"count":   resetCount,
	})
}

func ResetSceneImage(c *gin.Context) {
	projectID := c.Param("id")
	sceneID := c.Param("sceneId")

	var scene models.Scene
	if err := db.DB.First(&scene, sceneID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Scene not found"})
		return
	}
	if fmt.Sprintf("%d", scene.ProjectID) != projectID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Scene does not belong to this project"})
		return
	}

	if err := resetSceneImageState(&scene); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset scene status"})
		return
	}

	BroadcastUpdate("scene", scene.ID)
	c.JSON(http.StatusOK, gin.H{
		"message": "Scene image reset successfully",
		"scene":   scene,
	})
}

// HandleBatchGenerateScenesTask processes the batch scene generation task
func HandleBatchGenerateScenesTask(t *models.Task) (interface{}, error) {
	var payload struct {
		ProjectID uint `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	var totalScenes int64
	if err := db.DB.Model(&models.Scene{}).Where("project_id = ?", payload.ProjectID).Count(&totalScenes).Error; err != nil {
		return nil, fmt.Errorf("failed to count scenes: %v", err)
	}
	if totalScenes == 0 {
		return "No scenes found in project", nil
	}

	result, err := queueProjectSceneImages(payload.ProjectID, t.ID)
	if err != nil {
		return nil, err
	}

	return fmt.Sprintf("Batch scene image generation queued successfully (%d queued, %d skipped, total %d)", result.QueuedCount, result.SkippedCount, totalScenes), nil
}

func orderedSceneCharacters(scene models.Scene) []models.Character {
	if len(scene.Characters) <= 1 {
		return append([]models.Character(nil), scene.Characters...)
	}

	ordered := append([]models.Character(nil), scene.Characters...)
	context := strings.Join([]string{
		strings.TrimSpace(scene.Description),
	}, "\n")
	hasNonLivingFormalRole := false
	hasLivingFormalRole := false
	for _, char := range ordered {
		if isNonLivingFormalRole(char) {
			hasNonLivingFormalRole = true
			continue
		}
		hasLivingFormalRole = true
	}
	mixedLivingAndNonLiving := hasNonLivingFormalRole && hasLivingFormalRole

	type charOrderKey struct {
		matched   bool
		index     int
		id        uint
		name      string
		nonLiving bool
	}

	buildKey := func(char models.Character) charOrderKey {
		idx := -1
		for _, candidate := range []string{strings.TrimSpace(char.Name)} {
			if candidate == "" {
				continue
			}
			if pos := strings.Index(context, candidate); pos >= 0 && (idx == -1 || pos < idx) {
				idx = pos
			}
		}
		return charOrderKey{
			matched:   idx >= 0,
			index:     idx,
			id:        char.ID,
			name:      strings.TrimSpace(char.Name),
			nonLiving: isNonLivingFormalRole(char),
		}
	}

	sort.SliceStable(ordered, func(i, j int) bool {
		a := buildKey(ordered[i])
		b := buildKey(ordered[j])
		if mixedLivingAndNonLiving && a.nonLiving != b.nonLiving {
			return !a.nonLiving
		}
		if a.matched != b.matched {
			return a.matched
		}
		if a.matched && b.matched && a.index != b.index {
			return a.index < b.index
		}
		if a.id != 0 && b.id != 0 && a.id != b.id {
			return a.id < b.id
		}
		return a.name < b.name
	})

	return ordered
}

func isNonLivingFormalRole(char models.Character) bool {
	if strings.TrimSpace(char.Gender) != "其他" {
		return false
	}
	combined := strings.Join([]string{
		strings.TrimSpace(char.FaceFingerprint),
		strings.TrimSpace(char.Description),
		strings.TrimSpace(char.Fingerprint),
		strings.TrimSpace(char.PositivePrompt),
	}, " ")
	if strings.TrimSpace(combined) == "" {
		return false
	}
	nonLivingMarkers := []string{
		"设备", "器物", "产品", "工具", "家具", "载具", "机器", "机械", "终端", "智能音箱", "音箱", "扬声器",
		"屏幕", "显示器", "平板", "电脑", "电视", "手机", "灯带", "顶盖", "面板", "机身", "网孔", "底座",
	}
	for _, marker := range nonLivingMarkers {
		if marker != "" && strings.Contains(combined, marker) {
			return true
		}
	}
	return false
}

func sceneCharacterReferenceContext(scene models.Scene, char models.Character) string {
	texts := []string{
		strings.TrimSpace(scene.Description),
	}
	candidates := []string{
		strings.TrimSpace(char.Name),
	}

	matched := make([]string, 0, len(texts))
	for _, text := range texts {
		if text == "" {
			continue
		}
		for _, candidate := range candidates {
			if candidate != "" && strings.Contains(text, candidate) {
				matched = append(matched, text)
				break
			}
		}
	}

	if len(matched) > 0 {
		return strings.Join(matched, "\n")
	}

	return strings.Join(texts, "\n")
}

func containsAnySceneMarker(text string, markers []string) bool {
	for _, marker := range markers {
		if marker != "" && strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isWeakSceneReferenceAnchor(scene models.Scene, char models.Character) bool {
	context := sceneCharacterReferenceContext(scene, char)
	if strings.TrimSpace(context) == "" {
		return false
	}

	weakMarkers := []string{
		"只露", "仅露", "只剩", "仅以", "虚入", "边缘", "角落", "一小段", "一小块",
		"模糊", "剪影", "轮廓", "后脑", "肩背", "肩线", "袖口", "帽檐后脑",
		"侧背", "背影", "不露清晰正脸", "不露正脸", "右下角虚入", "右缘虚入", "前景遮挡",
	}
	strongMarkers := []string{
		"正脸", "侧脸", "三分之二侧脸", "三分之四侧脸", "下半张脸", "半张脸",
		"脸部", "面容", "眉", "眼", "鼻梁", "嘴唇", "唇", "下颌", "眼神", "眼眶",
		"清晰露出", "中景主位", "主位", "张嘴", "说话",
	}

	return containsAnySceneMarker(context, weakMarkers) && !containsAnySceneMarker(context, strongMarkers)
}

func selectSceneReferenceCharacters(scene models.Scene, ordered []models.Character) ([]models.Character, []string) {
	if len(ordered) == 0 {
		return nil, nil
	}

	selected := make([]models.Character, 0, len(ordered))
	excluded := make([]string, 0)
	for _, char := range ordered {
		if isWeakSceneReferenceAnchor(scene, char) {
			excluded = append(excluded, char.Name)
			continue
		}
		selected = append(selected, char)
	}

	return selected, excluded
}

func loadSceneCharactersForGeneration(scene models.Scene) ([]models.Character, error) {
	if len(scene.Characters) > 0 {
		return orderedSceneCharacters(scene), nil
	}
	if scene.ID == 0 {
		return nil, nil
	}

	var chars []models.Character
	if err := db.DB.Model(&scene).Association("Characters").Find(&chars); err != nil {
		return nil, err
	}

	scene.Characters = chars
	return orderedSceneCharacters(scene), nil
}

func sceneCharactersMissingReferenceImages(chars []models.Character) []string {
	if len(chars) == 0 {
		return nil
	}

	missing := make([]string, 0)
	for _, char := range chars {
		if strings.TrimSpace(char.GeneratedImage) != "" {
			continue
		}
		name := strings.TrimSpace(char.Name)
		if name == "" {
			name = fmt.Sprintf("character_%d", char.ID)
		}
		missing = append(missing, name)
	}
	return missing
}

// constructScenePrompt builds the LLM prompt for scene generation
// It centralizes the logic to ensure consistency across single and batch generation
func constructScenePrompt(scene models.Scene, project models.Project, lang string) (string, string) {
	_ = project
	_ = lang
	promptLangInstruction := "无论输入是什么语言，你都必须让 prompt_pos_screen_zh 全部使用中文返回，不要返回 prompt_pos_screen_en 或 prompt_neg_screen_en。"
	orderedChars := orderedSceneCharacters(scene)

	// Character Info Construction
	charInfo := ""
	for _, char := range orderedChars {
		charInfo += fmt.Sprintf(
			"- Name: %s\n  FaceFingerprint: %s\n  Fingerprint: %s\n",
			char.Name,
			strings.TrimSpace(char.FaceFingerprint),
			strings.TrimSpace(char.Fingerprint),
		)
	}

	sceneContext := scene.Description

	sceneModeInstruction := buildScenePromptModeInstruction(project.SceneMode, len(orderedChars), project.DisableReferenceImages)
	resolutionInstruction := buildSceneImageResolutionInstruction()
	specializedContextBlock := ""
	if specializedMatch := buildScenePromptSpecializedRuleMatch(scene); specializedMatch.Block != "" {
		specializedContextBlock = fmt.Sprintf("\n%s\n", specializedMatch.Block)
	}

	systemPrompt := fmt.Sprintf(`
你是一位专业的【影视场景概念图提示词专家】（AI Art Director）。
你的任务是根据剧本上下文，编写用于 ComfyUI 文生图的高质量提示词，生成一张极具电影感的场景概念图（Keyframe）。

【核心指令】
当前场景图目标画幅约束：
%s

1. **推理与构建**:
   - **环境构建 (Detail & Atmosphere)**: 
     - 必须包含：光影、材质纹理、空气状态、地面与建筑细节、街道/室内陈设、色调、景别、机位、空间层次。
     - 描述必须具体，禁止空泛的“高质量”“像仙境一样”“类似上一幕”。
     - 地名、镇名、巷名、洞天名、门派名等专有名词只用于你的内部理解，不能直接拿来充当画面提示词。
     - 例如“红烛镇街面”“泥瓶巷小屋”“骊珠洞天牌坊下”这类表达，最终都必须展开成 ComfyUI 能理解的具体视觉内容：街道宽窄、摊位结构、木楼或土墙、青石板或泥地、灯笼、牌坊材质、屋梁、窗纸、湿痕、裂纹、摊架、器物、光线方向、空气状态。
     - 如果输入里出现地名但视觉细节仍不够，你必须主动补全成完整可见场景，不能只重复地名本身。
   - **角色演绎**: 根据【角色对话】、face_fingerprint 和 fingerprint 推理人物当下的面部表情、肢体语言、衣物状态和伤势变化；其中脸部与基础发型优先来自 face_fingerprint，体态、服装、装备与身份关系优先来自 fingerprint。
   - **站位布局**: 根据人物关系和剧情，合理安排人物在画面中的位置。
   - **背景固定站位规则**: 若背景人物在剧情里属于朝堂班列、会议座次、课堂座位、宴席席位、门侧守位、廊下守位、仪仗位置、侍卫护位、柜台内外、窗口排队、队伍队列或其它固定站位系统，你必须把这些人写成稳定留在各自位置上的背景层，只允许原地弱反应；除非剧情明确要求出列、换位、围拢、让路、追赶、奔逃或穿越前景，不要把背景人物写成到处乱走。
   - **方向性空间朝向规则**: 如果人物正在沿楼梯、台阶、走廊、门洞、过道、坡道、长廊、桥面、楼道平台等有明确前进方向的空间移动，你必须把人物朝向写成与路径一致的背侧、三分之二侧面、三分之四背侧或只露部分侧脸的构图；不要把“正在上楼/下楼/朝门口走”这类镜头写成人物正对镜头的摆拍。人物目光应落在前方台阶、转角、门洞、扶手或脚下路径上，而不是无理由直视镜头。
   - **临时状态**: 如果人物受伤、衣服破损、沾血、潮湿、灰尘、疲惫、哭泣，这些必须写进 prompt_pos_screen，不得依赖角色名字。
   - **泪痕与湿痕克制规则**: 如果人物只是刚哭过、忍泪、眼角湿润、睫毛挂泪、泪痕未干、嘴角带血或局部湿痕，你必须把这些状态写成小范围、局部、顺着皮肤或衣料自然延展的细节，例如眼角微湿、下睫毛有一点泪光、脸颊上一小道泪痕、嘴角一点血色；不要把它写成大面积泪水糊脸、整张脸湿透、厚重泪线或像特效妆一样夸张的液体痕迹，除非剧情明确要求失控痛哭或大量出血。
   - **肢体完整性**: 你必须把画面中的人物写成单一、完整、解剖结构正确的人体，禁止出现多余肢体、多只手、多只脚、多手指、少手指、肢体错位、关节方向错误、身体部位重复等问题；这些约束必须直接体现在返回的提示词中。
   - **高风险遮挡手部姿态**: 如果人物是双手背后、单手捂脸、抱胸、蜷缩抱膝、手臂压在沙发/桌沿/门框后、手藏在口袋或袖内这类高风险遮挡姿态，你必须直接写清“当前哪只手可见、哪只手被什么遮住、遮住后不再从别处伸出”，不要让模型把被挡住的手补成第三只手或在家具后方、肩后、脸旁额外冒出一只手。
   - **女性角色特征保持**: 如果角色是女性，必须保持明显女性化的脸部与身体特征；若场景存在胸口、肩部、腰侧等部位的衣物破损，应优先描述内层衣物、裹胸、抹胸、内衬、肩带、蕾丝边、里衬边角等可见细节，而不是直接写成大片空白裸露皮肤。
   - **女性胸口破损规则 (CRITICAL)**: 如果女性角色胸口或胸侧外层衣物被划破、撕裂或浸血，返回的 prompt_pos_screen 必须明确写出“外层破损但内层深色里衬/束胸式内衬/抹胸结构仍完整覆盖胸口与胸线”，必须表现为“裂口下仍有完整内层包覆”，禁止把伤口周围写成直接裸露胸口皮肤、大片露肤或像衣服下面什么都没有。
   - **禁止角色名出现在输出中**: 角色名字只用于你的内部推理，最终返回的 prompt_pos_screen 中严禁出现任何人物名字，必须改写成可供 ComfyUI 理解的人物外观描述、站位描述和动作描述。

2. **镜头规则**:
   - 每个场景都必须是完整可独立理解的画面，严禁使用“同上”“隔壁”“上一幕那个地方”“继续刚才场景”这类词。
   - 场景细节必须足够长，足够具体，足以让 ComfyUI 直接理解画面。
   - 若是连续对话镜头，请保留场景连续性，只改变镜头角度、景别或人物朝向，不要无故跳场。

3. **场景模式补充**:
%s

4. **绝对禁忌 (Critical Prohibitions)**:
   - **严禁出现任何文字**: 禁止字幕 (subtitles)、气泡 (speech bubbles)、对话框、水印、UI元素。
   - **严禁分镜/边框**: 必须是单幅完整画面，禁止漫画分格 (comic panels)、边框 (borders)、分屏。
   - 这些禁止项必须直接翻译进正向提示词的稳定结构描述里，不要把关键连续性信息留给负向提示词兜底。

5. **输出要求**:
   - **prompt_pos_screen_zh**: 返回中文场景图正向提示词，内容必须包含环境细节、镜头信息、人物表情动作、服装状态、临时伤势变化。必须直接描述“谁在前景、谁在中景、谁在远处、人物在做什么”，但不能出现角色名字。
   - 需要出现多少正式角色，就按当前镜头实际可见关系写多少，但必须保证主次清楚、站位清楚、遮挡清楚，不要把多名正式角色压成模糊模板。
【返回的正向提示词语言约定】
%s

请直接输出 JSON 格式。
`, resolutionInstruction, sceneModeInstruction, promptLangInstruction)

	userPrompt := fmt.Sprintf(`
场景基础描述（核心视觉上下文）：
%s

关联角色信息（请确保这些角色合理出现在场景中）：
%s
%s

额外要求：
- 必须优先继承【场景基础描述】中的机位、人物站位、前后关系、遮挡关系和视觉重心。
- 分辨率与画幅数字只用于你内部推理构图，不得把“宽1344高768”“1344x768”“横幅1344x768”这类尺寸字样写回 prompt_pos_screen_zh；若确实需要表达画幅，只能概括成“横向宽画幅”“竖向窄画幅”或“方形画幅”。
- face_fingerprint 与 fingerprint 是硬约束，不是灵感来源，更不是允许你脑补、审美化重写或自行合理化改写的素材。只要当前镜头里对应部位可见，你就必须优先忠实转写这些锚点，不得自行换脸、改发型、改体型、改内搭颜色、改装备位置或改主副配色。
- 若剧情没有明确写出衰老、胡茬、法令纹、眼袋、皱纹、发线后移、发量变化、肤色暗沉或明显消瘦，不得擅自把人物画老、画沧桑、画病态或改成另一年龄层。
- 若当前镜头里仍看得到领口、内搭、衬衫、毛衣、袖口、肩线、外套内层、裤装、鞋靴、配饰或装备，就必须把具体颜色与结构写清，不能只写“内搭”“里层衣物”“一件上衣”这种泛词后让模型自己猜。
- 如果当前镜头里出现菜单、账单、简历、笔记本、工牌、文件夹、照片、画册、杂志、茶杯、酒杯等易串类道具，必须写清真实用途与材质；餐厅菜单不得漂成人物画册、写真集、插画书、杂志或海报册。
- 如果当前画面里有多名正式角色，你必须把每名正式角色按当前镜头真实可见程度写清，不要让后排角色抢走主位，也不要把主位角色压成模糊背景。
- 只要当前镜头有对白并需要口型，当前主说话者就必须同时成为当前画面最可读的口型承载位；优先让说话者占据清晰正脸、三分之四侧脸或清晰半侧脸主位。静态场景图本身也必须把说话者放在构图主位、视觉中心或最清晰主脸位置，不要让听者因为更靠近镜头、更居中、更亮或更大面积正脸而接管首帧主位。若当前是过肩镜头，也只能让听者肩背或后脑做前景遮挡，让说话者处于中景焦点位；不能把说话者压成前景肩背而让听者占据正对镜头的清晰主位。若听者反而是更大的清晰正脸，而说话者只剩前景肩背、边缘半脸、背侧、虚焦层或过小比例层，你必须把场景图改成说话者主位，不要把静态图主位与口型风险留给下游。
- 听者即使清楚入画，也必须保持闭口，只承担听位与弱反应；不要因为听者更靠近镜头、更清晰或更居中，就让他接管错误口型。
- 如果场景基础描述里已经明确写出伤势、裂口、凝血、衣物破损、浸血、硬化布料、内层衣物、束胸式内衬、肩带、里衬边角等具体视觉细节，你必须把这些细节直接写进最终返回的 prompt_pos_screen，不能弱化、不能省略、不能只保留“受伤”这种泛化说法。
- 如果当前镜头的临时状态涉及眼角湿润、睫毛挂泪、泪痕未干、嘴角血迹、局部湿痕或刚擦过眼泪的痕迹，你必须把这些细节写成克制、局部、符合皮肤和衣料表面路径的小范围痕迹，例如一小道泪痕、眼角微湿、少量血色、浅浅水痕；不要让模型理解成大片泪水糊脸、整张脸湿透、粗重流痕或夸张特效化液体。
- 如果女性角色胸口或胸侧被划破，你必须直接写出“外层裂口下仍有深色里衬、束胸式内衬、抹胸或内层布料完整覆盖胸口”，并把“破损处不能是裸露胸口皮肤”表达清楚，不能让模型自行理解成衣物破了就直接露肤。
- 你返回的 prompt_pos_screen_zh 必须明确包含避免多余肢体、多只手、多只脚、多手指、少手指、畸形手脚、肢体错位、关节错误、身体部位重复的正向约束，确保 ComfyUI 不会生成三只手之类的错误。
- 若当前人物姿态存在背手、捂脸、抱胸、蜷缩、手被沙发靠背/桌沿/门框/被褥挡住等遮挡关系，你必须把“哪只手可见、哪只手被遮住、被什么遮住、遮住后不再从其他位置冒出”直接写进 prompt_pos_screen_zh，不要把这类结构信息留给负向提示词兜底。
- 如果当前没有绑定角色，请把它当成纯场景镜头来写，不要强行加入人物。
	- 如果当前镜头与前后镜头共享同一辆车、同一间办公室、同一间卧室、同一间病房、同一间教室、同一间店铺或其它重复出现的固定地点/大型常驻道具，你必须继承它的稳定拓扑常量与关键外观常量，例如车身主色、车型轮廓、驾驶区/座位/过道/车窗/车门位置，或门窗朝向、桌床柜摆位、主灯位置。不要让同一辆红车下一镜变成绿车，也不要让同一辆公交车的驾驶区、方向盘、前挡风玻璃、过道和座位关系反复变化。
	- 在公交车、汽车驾驶舱、出租车、驾驶室、电梯、小房间、窄走廊、驾驶台、吧台内侧等紧凑空间里，人物站位必须物理成立：司机固定在驾驶座，方向盘和前挡风玻璃始终在司机正前方，乘客或对话者只能位于驾驶区后方过道、邻座、车门附近或空间允许的位置；不要把人物写到挡风玻璃外、司机跑到后排并坐、隔着不可能的结构对视、或同时占据同一狭小位置。
	- 如果窗外、门外、雨棚下、站台外、街对面、远处楼上或车外只存在未命名、匿名、遮脸、打伞、背光、模糊、极远的人影或剪影，这些对象必须保持匿名、遮挡或轮廓级存在，不得长出绑定正式角色的脸，也不得被误补成清晰主角。
	- face_fingerprint 只负责锁定人物脸部与基础发型等长期稳定脸部锚点；fingerprint 只负责补充人物的体态、固定服装、稳定装备、整体身份与当前可见关系这类非脸部长期锚点。若两者都存在，脸部优先服从 face_fingerprint，服装与装备优先服从 fingerprint，且不要在 fingerprint 里重复 face_fingerprint 已经锁定的脸部内容。
	- 如果当前有 2 名角色，请优先写导演化双人构图，不要写成呆板并排站立。
	- 如果当前有 2 名角色，你必须把两个人分别写开：每个人都必须按当前取景范围把当前可见的脸部识别点、基础发型或发饰、体态轮廓、固定服装结构/主副配色、稳定装备位置完整写全；不要让第二个人只剩“听者、对面那人、陪体”这类弱描述。
	- 双人镜头里，不要把其中一人的身份只写成模糊关系词后交给模型自己猜；返回的 prompt_pos_screen 必须让两个人各自都能被单独识别，而不是把第二个人弱化成第一人的平均化变体。
	- 双人镜头里，每名角色都要按当前取景范围把可见身份锚点完整写出：如果是全身或半身，就必须把当前看得到的脸部、基础发型、肩颈、服装结构、主副配色、鞋靴、持物和稳定装备写进去；如果是局部镜头，就必须把当前局部能认出身份的锚点写扎实。只要当前镜头里可见，就必须写；只有当前构图下确实看不见，才不写。不要把人物压缩成几笔弱描述，也不要把当前构图看不见的装备硬写出来。
- 不要把对白原文直接作为画面文字输出，只根据场景基础描述和角色状态去推理情绪、口型和动作。
	- 角色名字只用于你内部推理，返回给我的 prompt_pos_screen 必须全部改写成可视外观、服装、站位、动作、景深与环境描述，不能出现任何角色名字。
	- 同理，地名和场景专有名词也只用于你内部定位；返回给我的 prompt_pos 必须把这些地名改写成具体可见的环境、建筑、地面、摊位、器物、光线和空间描述，不能只写“红烛镇街面”“泥瓶巷小屋”这种抽象地点名词。
	- 如果画面里需要路人、背景人物、摊贩、行人、守卫、店家或远景人影，你必须根据当前剧情场景的时代与身份系统去推理他们的服装和外形；古风/仙侠场景中的背景人物只能是古装、布衣、袍服、发髻、布鞋或靴履等对应时代装束，绝不允许出现现代服装、现代发型、现代鞋帽、现代包具或现代街头元素。
	- 背景人物只能作为弱化的环境层次存在，必须适度虚化、降低细节、弱化存在感，不能抢主体，也不能因为省略细节而变成时代错误的现代人。
	- 若背景人物本来就绑定席位、班列、门侧守位、桌边座位、殿下站班、课堂座位、会场座区、宴席席位、仪仗位置或其他固定站位，必须让他们留在各自位置上作为稳定背景层；不要无缘由把他们写成前后乱走、互换位置、穿越主体前景或突然冲到镜头中心。
`, sceneContext, charInfo, specializedContextBlock)

	return systemPrompt, userPrompt
}

func validateNoFormalCharacterNamesInScenePromptText(scene models.Scene, text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	for _, char := range scene.Characters {
		name := strings.TrimSpace(char.Name)
		if name != "" && strings.Contains(trimmed, name) {
			return fmt.Errorf("scene prompt contains forbidden formal character name: %s", name)
		}
	}
	return nil
}

// GenerateSceneImage handles manual image generation for a scene
func GenerateSceneImage(c *gin.Context) {
	projectID := c.Param("id")
	sceneID := c.Param("sceneId")

	var scene models.Scene
	if err := db.DB.Preload("Characters").First(&scene, sceneID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Scene not found"})
		return
	}
	if fmt.Sprintf("%d", scene.ProjectID) != projectID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Scene does not belong to this project"})
		return
	}

	// Check prompts
	if strings.TrimSpace(scene.ImagePrompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Scene image_prompt is missing"})
		return
	}

	if err := resetSceneImageState(&scene); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset scene before regeneration"})
		return
	}
	BroadcastUpdate("scene", scene.ID)

	// Trigger Image Generation
	promptID, err := triggerSceneImageGeneration(scene)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to queue image generation: %v", err)})
		return
	}

	// Start background polling to save image (reusing character logic or similar)
	var project models.Project
	db.DB.First(&project, scene.ProjectID)
	go pollSceneImage(promptID, scene.ID, project.Code, "")

	c.JSON(http.StatusOK, gin.H{"message": "Image generation queued", "prompt_id": promptID})
}

func stripCharacterNamesForImagePrompt(content string, characterNames []string) string {
	output := strings.TrimSpace(content)
	for _, name := range characterNames {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			continue
		}
		pattern := regexp.MustCompile(regexp.QuoteMeta(trimmedName) + `\s+`)
		output = pattern.ReplaceAllString(output, "")
	}
	output = strings.ReplaceAll(output, "当前状态：", "")
	return strings.TrimSpace(output)
}

func parseSceneImagePromptForRuntime(prompt string) ([]string, error) {
	lines := nonEmptyTrimmedLines(prompt)
	if len(lines) == 0 {
		return nil, fmt.Errorf("image_prompt is empty")
	}

	merged := make([]string, 0, len(lines))
	for _, line := range lines {
		hasStructuredPrefix := strings.HasPrefix(line, "主体：") ||
			strings.HasPrefix(line, "场景：") ||
			strings.HasPrefix(line, "构图：") ||
			strings.HasPrefix(line, "光影：") ||
			strings.HasPrefix(line, "风格：") ||
			strings.HasPrefix(line, "约束：")
		if hasStructuredPrefix || len(merged) == 0 {
			merged = append(merged, line)
			continue
		}
		merged[len(merged)-1] = strings.TrimSpace(merged[len(merged)-1] + " " + line)
	}
	return merged, nil
}

func buildSceneImageRuntimePrompt(imagePrompt string, characterNames []string) (string, error) {
	lines, err := parseSceneImagePromptForRuntime(imagePrompt)
	if err != nil {
		return "", err
	}

	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "风格：") {
			continue
		}
		idx := strings.Index(line, "：")
		if idx < 0 {
			continue
		}
		content := strings.TrimSpace(line[idx+len("："):])
		if content == "" {
			continue
		}
		content = stripCharacterNamesForImagePrompt(content, characterNames)
		if content == "" {
			continue
		}
		parts = append(parts, content)
	}

	if len(parts) == 0 {
		fallback := stripCharacterNamesForImagePrompt(strings.TrimSpace(imagePrompt), characterNames)
		if fallback == "" {
			return "", fmt.Errorf("scene image_prompt could not be converted into runtime prompt")
		}
		return fallback, nil
	}
	return strings.Join(parts, "\n"), nil
}

func triggerSceneImageGeneration(scene models.Scene) (string, error) {
	var project models.Project
	if err := db.DB.Preload("ArtStyle").First(&project, scene.ProjectID).Error; err != nil {
		return "", fmt.Errorf("failed to load project: %v", err)
	}
	var characters []models.Character
	if err := db.DB.Where("project_id = ?", scene.ProjectID).Find(&characters).Error; err != nil {
		return "", fmt.Errorf("failed to load project characters: %v", err)
	}
	width, height := getConfiguredSceneImageSize()
	seed := getConfiguredGlobalSeed()
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyDefaultImageModel).First(&setting).Error; err != nil {
		return "", fmt.Errorf("default image workflow is not configured")
	}
	workflowName := strings.TrimSpace(setting.Value)
	if workflowName == "" {
		return "", fmt.Errorf("default image workflow is not configured")
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

	// Parse Metadata & Load JSON
	meta, err := workflow.ParseWorkflow(targetFile)
	if err != nil {
		return "", fmt.Errorf("failed to parse workflow: %v", err)
	}
	data, err := os.ReadFile(targetFile)
	if err != nil {
		return "", fmt.Errorf("failed to read workflow file: %v", err)
	}
	var wfJSON map[string]interface{}
	if err := json.Unmarshal(data, &wfJSON); err != nil {
		return "", fmt.Errorf("failed to unmarshal workflow json: %v", err)
	}

	// Helper to set input
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

	imagePrompt := strings.TrimSpace(scene.ImagePrompt)
	if imagePrompt == "" {
		return "", fmt.Errorf("scene image_prompt is missing")
	}
	characterNames := make([]string, 0, len(characters))
	for _, char := range characters {
		name := strings.TrimSpace(char.Name)
		if name == "" {
			continue
		}
		characterNames = append(characterNames, name)
	}
	runtimeImagePrompt, err := buildSceneImageRuntimePrompt(imagePrompt, characterNames)
	if err != nil {
		return "", err
	}
	finalImagePrompt := appendProjectStylePrompt(runtimeImagePrompt, project)

	setInput(meta.PositiveNodeID, meta.PositiveInputKey, finalImagePrompt)
	if meta.NegativeNodeID != "" {
		setInput(meta.NegativeNodeID, meta.NegativeInputKey, "")
	}

	// Inject Seed & Dims
	setInput(meta.SeedNodeID, meta.SeedInputKey, seed)
	setInput(meta.WidthNodeID, meta.WidthInputKey, width)
	setInput(meta.HeightNodeID, meta.HeightInputKey, height)
	logComfyWorkflowPayload("Scene ComfyUI Payload", workflowLabel, wfJSON)

	// Queue
	promptID, err := QueueComfyPrompt(wfJSON)
	if err != nil {
		return "", err
	}
	if workflowLabel != "" {
		if err := db.DB.Model(&models.Scene{}).
			Where("id = ?", scene.ID).
			Update("image_generated_workflow", workflowLabel).Error; err != nil {
			Log(LogLevelWarn, "Scene Workflow Save Failed", fmt.Sprintf("scene=%d workflow=%s err=%v", scene.ID, workflowLabel, err))
		}
	}
	return promptID, nil
}

func waitForSceneImageOutput(promptID string, sceneID uint, projectCode string) (string, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timeout := time.After(20 * time.Minute)

	for {
		select {
		case <-timeout:
			return "", fmt.Errorf("image generation timed out")
		case <-ticker.C:
			history, err := GetComfyHistory(promptID)
			if err != nil {
				continue
			}
			outputs, ok := history["outputs"].(map[string]interface{})
			if !ok {
				continue
			}
			for _, nodeOutput := range outputs {
				imageOutputs, ok := nodeOutput.(map[string]interface{})
				if !ok {
					continue
				}
				images, ok := imageOutputs["images"].([]interface{})
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

				saveDir := filepath.Join("output", projectCode, "scenes")
				if err := os.MkdirAll(saveDir, 0755); err != nil {
					return "", err
				}
				saveFilename := fmt.Sprintf("scene_%d_%d.png", sceneID, time.Now().Unix())
				savePath := filepath.Join(saveDir, saveFilename)
				if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
					return "", err
				}
				return "/" + filepath.ToSlash(savePath), nil
			}
		}
	}
}

func pollSceneImage(pid string, sceneID uint, projectCode string, taskID string) {
	webPath, err := waitForSceneImageOutput(pid, sceneID, projectCode)
	if err != nil {
		Log(LogLevelError, "Scene Image Timeout", fmt.Sprintf("Prompt ID: %s err=%v", pid, err))
		var s models.Scene
		if loadErr := db.DB.First(&s, sceneID).Error; loadErr == nil {
			s.Status = "failed"
			s.UpdatedAt = time.Now()
			db.DB.Save(&s)
			BroadcastUpdate("scene", sceneID)
		}
		if taskID != "" {
			task.GlobalTaskManager.UpdateTaskStatus(taskID, "failed", 0, err.Error())
		}
		return
	}

	var s models.Scene
	if err := db.DB.First(&s, sceneID).Error; err != nil {
		if taskID != "" {
			task.GlobalTaskManager.UpdateTaskStatus(taskID, "failed", 0, "Scene not found after image generation")
		}
		return
	}

	s.GeneratedImage = webPath
	s.Status = "generated"
	s.UpdatedAt = time.Now()
	db.DB.Save(&s)
	Log(LogLevelInfo, "Scene Image Saved", webPath)

	BroadcastUpdate("scene", sceneID)
	if taskID != "" {
		task.GlobalTaskManager.UpdateTaskStatus(taskID, "completed", 100, "Image generation completed")
	}
}

type ScenePromptResponse struct {
	PromptPosScreenZH          string `json:"prompt_pos_screen_zh"`
	PromptNegScreenZH          string `json:"prompt_neg_screen_zh"`
	PromptPosScreenEN          string `json:"prompt_pos_screen_en"`
	PromptNegScreenEN          string `json:"prompt_neg_screen_en"`
	PromptPosZH                string `json:"prompt_pos_zh"`
	PromptNegZH                string `json:"prompt_neg_zh"`
	PromptPosEN                string `json:"prompt_pos_en"`
	PromptNegEN                string `json:"prompt_neg_en"`
	PlayerDesc                 string `json:"player_desc"`
	RecommendedFPS             int    `json:"recommended_fps"`
	RecommendedDurationSeconds int    `json:"recommended_duration_seconds"`
}

func callLLMScenePrompt(scene models.Scene, provider models.LLMProvider, system, user string, taskID string, stageLabel string) (*ScenePromptResponse, error) {
	timeout := 10 * time.Minute
	progressMessage := "正在请求 LLM 生成提示词..."
	streamLabel := "提示词"
	if stageLabel != "" {
		progressMessage = fmt.Sprintf("正在请求 LLM 生成%s...", stageLabel)
		streamLabel = stageLabel
	}
	content, err := requestLLMContent(provider, system, user, taskID, timeout, 5, progressMessage, streamLabel)
	if err != nil {
		return nil, err
	}

	db.DB.Create(&models.SystemLog{
		Level:     LogLevelInfo,
		Message:   llmLogMessage(fmt.Sprintf("LLM 完整返回(%s)", streamLabel), provider),
		Details:   content,
		CreatedAt: time.Now(),
	})

	jsonContent := cleanupLLMJSON(content)
	var result ScenePromptResponse
	if err := json.Unmarshal([]byte(jsonContent), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}
	normalizeScenePromptResponse(&result)
	if strings.TrimSpace(result.PromptPosZH) == "" {
		return nil, fmt.Errorf("scene prompt response missing required fields")
	}
	if err := validateNoFormalCharacterNamesInScenePromptText(scene, result.PromptPosZH); err != nil {
		return nil, err
	}

	return &result, nil
}

func normalizeScenePromptResponse(result *ScenePromptResponse) {
	result.PromptPosZH = strings.TrimSpace(result.PromptPosZH)
	result.PromptNegZH = strings.TrimSpace(result.PromptNegZH)
	result.PromptPosEN = strings.TrimSpace(result.PromptPosEN)
	result.PromptNegEN = strings.TrimSpace(result.PromptNegEN)
	result.PromptPosScreenZH = strings.TrimSpace(result.PromptPosScreenZH)
	result.PromptNegScreenZH = strings.TrimSpace(result.PromptNegScreenZH)
	result.PromptPosScreenEN = strings.TrimSpace(result.PromptPosScreenEN)
	result.PromptNegScreenEN = strings.TrimSpace(result.PromptNegScreenEN)
}
