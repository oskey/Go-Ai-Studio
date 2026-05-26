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

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	audioProductionOutputRoot = "output/audio_production"

	audioProductionModeCustomVoice = "custom_voice"
	audioProductionModeVoicePrompt = "voice_prompt"

	audioProductionCustomVoiceWorkflowPath = "workflows/Qwen3-TTs-Custom-Voice.json"
	audioProductionVoicePromptWorkflowPath = "workflows/Qwen3-TTS Voice-Prompt.json"

	defaultAudioProductionCustomVoiceSpeaker = "Vivian (Chinese - Bright, Sharp, Young Female)"
)

type audioProductionPresetOption struct {
	Label       string  `json:"label"`
	Value       string  `json:"value"`
	Description string  `json:"description,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
}

type audioProductionParsedLine struct {
	SortOrder int    `json:"sort_order"`
	Text      string `json:"text"`
}

func normalizeAudioProductionMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case audioProductionModeVoicePrompt:
		return audioProductionModeVoicePrompt
	case audioProductionModeCustomVoice, "":
		return audioProductionModeCustomVoice
	default:
		return ""
	}
}

func audioProductionProjectDir(code string) string {
	return filepath.Join(audioProductionOutputRoot, code)
}

func audioProductionGeneratedDir(code string) string {
	return filepath.Join(audioProductionProjectDir(code), "generated")
}

func normalizeAudioProductionTemperature(value float64) float64 {
	if value <= 0 {
		return 0.7
	}
	if value < 0.1 {
		return 0.1
	}
	if value > 2 {
		return 2
	}
	return value
}

func normalizeAudioProductionOneLine(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func parseAudioProductionText(text string) ([]audioProductionParsedLine, error) {
	rawLines := strings.Split(text, "\n")
	lines := make([]audioProductionParsedLine, 0, len(rawLines))
	for _, rawLine := range rawLines {
		line := normalizeAudioProductionOneLine(rawLine)
		if line == "" {
			continue
		}
		lines = append(lines, audioProductionParsedLine{
			SortOrder: len(lines) + 1,
			Text:      line,
		})
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("请先填写至少一行要生成的文本")
	}
	return lines, nil
}

func loadAudioProductionProjectOr404(c *gin.Context) (*models.AudioProductionProject, error) {
	projectID := strings.TrimSpace(c.Param("id"))
	var project models.AudioProductionProject
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "音频生产项目不存在"})
		return nil, err
	}
	return &project, nil
}

func loadAudioProductionLineOr404(c *gin.Context) (*models.AudioProductionLine, error) {
	lineID := strings.TrimSpace(c.Param("lineId"))
	var line models.AudioProductionLine
	if err := db.DB.First(&line, lineID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "音频生产行不存在"})
		return nil, err
	}
	return &line, nil
}

func replaceAudioProductionLines(project models.AudioProductionProject, text string) ([]models.AudioProductionLine, error) {
	parsed, err := parseAudioProductionText(text)
	if err != nil {
		return nil, err
	}
	oldGeneratedAudio := make([]string, 0)
	created := make([]models.AudioProductionLine, 0, len(parsed))
	now := time.Now()
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		var oldLines []models.AudioProductionLine
		if err := tx.Where("project_id = ?", project.ID).Find(&oldLines).Error; err != nil {
			return err
		}
		for _, line := range oldLines {
			if strings.TrimSpace(line.GeneratedAudio) != "" {
				oldGeneratedAudio = append(oldGeneratedAudio, line.GeneratedAudio)
			}
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.AudioProductionLine{}).Error; err != nil {
			return err
		}
		for _, item := range parsed {
			line := models.AudioProductionLine{
				ProjectID:        project.ID,
				SortOrder:        item.SortOrder,
				Text:             item.Text,
				Speaker:          strings.TrimSpace(project.Speaker),
				Instruct:         normalizeAudioProductionOneLine(project.Instruct),
				VoiceInstruction: normalizeAudioProductionOneLine(project.VoiceInstruction),
				Temperature:      normalizeAudioProductionTemperature(project.Temperature),
				Status:           audioCloneLineStatusDraft,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if err := tx.Create(&line).Error; err != nil {
				return err
			}
			created = append(created, line)
		}
		return nil
	})
	if err == nil {
		for _, path := range oldGeneratedAudio {
			_ = removeAudioCloneAsset(path)
		}
	}
	return created, err
}

func audioProductionSpeakerPresets() []audioProductionPresetOption {
	presets := []audioProductionPresetOption{
		{
			Label:       "Vivian｜中文｜明亮锐利年轻女声",
			Value:       defaultAudioProductionCustomVoiceSpeaker,
			Description: "来自当前 Qwen3 Custom Voice 工作流的默认内置人设。",
		},
	}
	if comfyPresets, err := fetchQwen3CustomVoiceSpeakerPresets(); err == nil && len(comfyPresets) > 0 {
		return comfyPresets
	}
	return presets
}

func qwen3SpeakerLabel(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	name := trimmed
	detail := ""
	if idx := strings.Index(trimmed, "("); idx > 0 && strings.HasSuffix(trimmed, ")") {
		name = strings.TrimSpace(trimmed[:idx])
		detail = strings.TrimSuffix(strings.TrimSpace(trimmed[idx+1:]), ")")
	}
	if detail == "" {
		return trimmed
	}
	parts := strings.Split(detail, " - ")
	if len(parts) == 0 {
		return trimmed
	}
	langMap := map[string]string{
		"Chinese":         "中文",
		"Chinese Beijing": "中文北京口音",
		"English":         "英文",
		"Japanese":        "日文",
		"Korean":          "韩文",
	}
	lang := strings.TrimSpace(parts[0])
	if translated, ok := langMap[lang]; ok {
		lang = translated
	}
	if len(parts) == 1 {
		return fmt.Sprintf("%s｜%s", name, lang)
	}
	descriptor := translateQwen3SpeakerDescriptor(strings.TrimSpace(strings.Join(parts[1:], " - ")))
	return fmt.Sprintf("%s｜%s｜%s", name, lang, descriptor)
}

func translateQwen3SpeakerDescriptor(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	phraseMap := map[string]string{
		"Bright, Sharp, Young Female": "明亮锐利年轻女声",
		"Warm, Soft, Young Female":    "温暖柔和年轻女声",
		"Deep, Mellow, Mature Male":   "低沉醇厚成熟男声",
		"Clear, Natural Young Male":   "清晰自然年轻男声",
		"Lively, Husky Male":          "活泼沙哑男声",
		"Rhythmic, Dynamic Male":      "节奏感强、动感男声",
		"Sunny, Clear American Male":  "阳光清亮美式男声",
		"Light, Playful Female":       "轻盈俏皮女声",
		"Emotional, Warm Female":      "情绪感强、温暖女声",
		"Bright, Sharp":               "明亮锐利",
		"Warm, Soft":                  "温暖柔和",
		"Deep, Mellow":                "低沉醇厚",
		"Clear, Natural":              "清晰自然",
		"Lively, Husky":               "活泼沙哑",
		"Rhythmic, Dynamic":           "节奏感强、动感",
		"Sunny, Clear American":       "阳光清亮美式",
		"Light, Playful":              "轻盈俏皮",
		"Emotional, Warm":             "情绪感强、温暖",
	}
	if translated, ok := phraseMap[trimmed]; ok {
		return translated
	}
	tokenMap := map[string]string{
		"Bright":    "明亮",
		"Sharp":     "锐利",
		"Warm":      "温暖",
		"Soft":      "柔和",
		"Young":     "年轻",
		"Female":    "女声",
		"Deep":      "低沉",
		"Mellow":    "醇厚",
		"Mature":    "成熟",
		"Male":      "男声",
		"Clear":     "清亮",
		"Natural":   "自然",
		"Lively":    "活泼",
		"Husky":     "沙哑",
		"Rhythmic":  "有节奏感",
		"Dynamic":   "动感",
		"Sunny":     "阳光",
		"American":  "美式",
		"Light":     "轻盈",
		"Playful":   "俏皮",
		"Emotional": "情绪感强",
	}
	segments := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ',' || r == '-' || r == '/'
	})
	translated := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		if value, ok := tokenMap[segment]; ok {
			translated = append(translated, value)
		} else {
			translated = append(translated, segment)
		}
	}
	if len(translated) == 0 {
		return trimmed
	}
	return strings.Join(translated, "、")
}

func extractComfyComboOptions(field interface{}) []string {
	items, ok := field.([]interface{})
	if !ok || len(items) == 0 {
		return nil
	}
	optionsRaw, ok := items[0].([]interface{})
	if !ok {
		return nil
	}
	options := make([]string, 0, len(optionsRaw))
	seen := make(map[string]bool, len(optionsRaw))
	for _, optionRaw := range optionsRaw {
		option, ok := optionRaw.(string)
		option = strings.TrimSpace(option)
		if !ok || option == "" || seen[option] {
			continue
		}
		seen[option] = true
		options = append(options, option)
	}
	return options
}

func fetchComfyObjectInfo(className string) (map[string]interface{}, error) {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyComfyUIAddress).First(&setting).Error; err != nil {
		setting.Value = "127.0.0.1:8188"
	}
	address := setting.Value
	if !strings.HasPrefix(address, "http") {
		address = "http://" + address
	}
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(strings.TrimRight(address, "/") + "/object_info/" + className)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("comfyui object_info returned %d: %s", resp.StatusCode, string(body))
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if nodeRaw, ok := result[className]; ok {
		if node, ok := nodeRaw.(map[string]interface{}); ok {
			return node, nil
		}
	}
	return result, nil
}

func fetchQwen3CustomVoiceSpeakerPresets() ([]audioProductionPresetOption, error) {
	node, err := fetchComfyObjectInfo("Qwen3TTSCustomVoice")
	if err != nil {
		return nil, err
	}
	inputRaw, ok := node["input"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Qwen3TTSCustomVoice object_info missing input")
	}
	requiredRaw, ok := inputRaw["required"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Qwen3TTSCustomVoice object_info missing required inputs")
	}
	options := extractComfyComboOptions(requiredRaw["speaker"])
	if len(options) == 0 {
		return nil, fmt.Errorf("Qwen3TTSCustomVoice speaker options not found")
	}
	presets := make([]audioProductionPresetOption, 0, len(options))
	for _, option := range options {
		presets = append(presets, audioProductionPresetOption{
			Label:       qwen3SpeakerLabel(option),
			Value:       option,
			Description: "来自 ComfyUI Qwen3TTSCustomVoice 节点的内置 speaker。",
		})
	}
	return presets, nil
}

func audioProductionInstructPresets() []audioProductionPresetOption {
	return []audioProductionPresetOption{
		{Label: "开心活泼", Value: "开心、明亮、语速自然，带一点轻快的笑意。"},
		{Label: "温柔亲切", Value: "温柔、亲切、放松，像在认真安慰对方。"},
		{Label: "严肃坚定", Value: "严肃、坚定、语气有分量，情绪克制。"},
		{Label: "愤怒质问", Value: "愤怒、压迫感强，语气带质问感。"},
		{Label: "紧张急促", Value: "紧张、急促，语速略快，情绪明显但不破音。"},
		{Label: "古风台词", Value: "古风台词感，语气郑重，节奏有停顿。"},
		{Label: "广告热情", Value: "热情、有感染力，适合促销和推荐。"},
	}
}

func audioProductionVoicePromptPresets() []audioProductionPresetOption {
	return []audioProductionPresetOption{
		{Label: "撒娇稚嫩女声", Value: "体现撒娇稚嫩的萝莉女声，音调偏高且起伏明显，营造出黏人、做作又刻意卖萌的听觉效果。"},
		{Label: "成熟御姐女声", Value: "体现成熟自信的女性声音，音色偏稳，语速从容，带一点掌控感和距离感。"},
		{Label: "温柔治愈女声", Value: "体现温柔治愈的女性声音，音色柔和，语速稍慢，情绪稳定，像在轻声安慰。"},
		{Label: "清亮少年男声", Value: "体现清亮年轻的少年男声，音色干净，语速自然，带一点朝气。"},
		{Label: "稳重低沉男声", Value: "体现稳重低沉的男性声音，音色厚实，语速平稳，表达克制有力量。"},
		{Label: "新闻播报腔", Value: "体现标准新闻播报声音，吐字清晰，节奏稳定，语气正式克制。"},
		{Label: "纪录片旁白", Value: "体现纪录片旁白声音，沉稳、清晰、有叙事感，节奏舒缓。"},
		{Label: "武侠古风", Value: "体现武侠古风台词声音，咬字有力度，情绪郑重，带一点江湖气。"},
		{Label: "紧张惊恐", Value: "体现紧张惊恐的声音，呼吸感更明显，语速略快，情绪有压迫感。"},
		{Label: "广告促销", Value: "体现广告促销式声音，热情、有感染力，节奏明快，重点词更突出。"},
	}
}

func ListAudioProductionPresets(c *gin.Context) {
	mode := normalizeAudioProductionMode(c.Query("mode"))
	if mode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知的音频生产模式"})
		return
	}
	if mode == audioProductionModeCustomVoice {
		c.JSON(http.StatusOK, gin.H{
			"speakers":  audioProductionSpeakerPresets(),
			"instructs": audioProductionInstructPresets(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"voice_prompts": audioProductionVoicePromptPresets(),
	})
}

func ListAudioProductionProjects(c *gin.Context) {
	mode := normalizeAudioProductionMode(c.Query("mode"))
	if mode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知的音频生产模式"})
		return
	}
	var projects []models.AudioProductionProject
	if err := db.DB.Where("mode = ?", mode).Order("created_at desc").Find(&projects).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取音频生产项目失败"})
		return
	}
	c.JSON(http.StatusOK, projects)
}

func GetAudioProductionProject(c *gin.Context) {
	project, err := loadAudioProductionProjectOr404(c)
	if err != nil {
		return
	}
	c.JSON(http.StatusOK, project)
}

func CreateAudioProductionProject(c *gin.Context) {
	var req struct {
		Mode             string  `json:"mode"`
		Name             string  `json:"name"`
		Code             string  `json:"code"`
		Description      string  `json:"description"`
		Text             string  `json:"text"`
		Speaker          string  `json:"speaker"`
		Instruct         string  `json:"instruct"`
		VoiceInstruction string  `json:"voice_instruction"`
		Temperature      float64 `json:"temperature"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	mode := normalizeAudioProductionMode(req.Mode)
	name := strings.TrimSpace(req.Name)
	code := normalizeAudioCloneCode(req.Code)
	if mode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知的音频生产模式"})
		return
	}
	if name == "" || code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写项目名称和项目文件名"})
		return
	}
	if !validateAudioCloneProjectCode(code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名只允许英文、数字、下划线或连字符"})
		return
	}
	var count int64
	db.DB.Model(&models.AudioProductionProject{}).Where("code = ?", code).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
		return
	}
	if _, err := os.Stat(audioProductionProjectDir(code)); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
		return
	}
	speaker := strings.TrimSpace(req.Speaker)
	if mode == audioProductionModeCustomVoice && speaker == "" {
		speaker = defaultAudioProductionCustomVoiceSpeaker
	}
	project := models.AudioProductionProject{
		Mode:             mode,
		Name:             name,
		Code:             code,
		Description:      strings.TrimSpace(req.Description),
		Text:             strings.TrimSpace(req.Text),
		Speaker:          speaker,
		Instruct:         normalizeAudioProductionOneLine(req.Instruct),
		VoiceInstruction: normalizeAudioProductionOneLine(req.VoiceInstruction),
		Temperature:      normalizeAudioProductionTemperature(req.Temperature),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if err := os.MkdirAll(audioProductionProjectDir(code), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建项目目录失败"})
		return
	}
	if err := db.DB.Create(&project).Error; err != nil {
		_ = os.RemoveAll(audioProductionProjectDir(code))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建音频生产项目失败"})
		return
	}
	c.JSON(http.StatusCreated, project)
}

func UpdateAudioProductionProject(c *gin.Context) {
	project, err := loadAudioProductionProjectOr404(c)
	if err != nil {
		return
	}
	var req struct {
		Mode             string  `json:"mode"`
		Name             string  `json:"name"`
		Code             string  `json:"code"`
		Description      string  `json:"description"`
		Text             string  `json:"text"`
		Speaker          string  `json:"speaker"`
		Instruct         string  `json:"instruct"`
		VoiceInstruction string  `json:"voice_instruction"`
		Temperature      float64 `json:"temperature"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	mode := normalizeAudioProductionMode(req.Mode)
	name := strings.TrimSpace(req.Name)
	code := normalizeAudioCloneCode(req.Code)
	if mode == "" || mode != project.Mode {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能修改音频生产模式"})
		return
	}
	if name == "" || code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写项目名称和项目文件名"})
		return
	}
	if !validateAudioCloneProjectCode(code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名只允许英文、数字、下划线或连字符"})
		return
	}
	if code != project.Code {
		var count int64
		db.DB.Model(&models.AudioProductionProject{}).Where("code = ? AND id <> ?", code, project.ID).Count(&count)
		if count > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
			return
		}
		if _, err := os.Stat(audioProductionProjectDir(code)); err == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
			return
		}
		if err := os.Rename(audioProductionProjectDir(project.Code), audioProductionProjectDir(code)); err != nil && !os.IsNotExist(err) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "重命名项目目录失败"})
			return
		}
		oldPrefix := "/" + filepath.ToSlash(audioProductionProjectDir(project.Code))
		newPrefix := "/" + filepath.ToSlash(audioProductionProjectDir(code))
		var lines []models.AudioProductionLine
		_ = db.DB.Where("project_id = ?", project.ID).Find(&lines).Error
		for _, line := range lines {
			if strings.HasPrefix(line.GeneratedAudio, oldPrefix) {
				_ = db.DB.Model(&models.AudioProductionLine{}).Where("id = ?", line.ID).Update("generated_audio", newPrefix+strings.TrimPrefix(line.GeneratedAudio, oldPrefix)).Error
			}
		}
	}
	speaker := strings.TrimSpace(req.Speaker)
	if project.Mode == audioProductionModeCustomVoice && speaker == "" {
		speaker = defaultAudioProductionCustomVoiceSpeaker
	}
	if err := db.DB.Model(&models.AudioProductionProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"name":              name,
		"code":              code,
		"description":       strings.TrimSpace(req.Description),
		"text":              strings.TrimSpace(req.Text),
		"speaker":           speaker,
		"instruct":          normalizeAudioProductionOneLine(req.Instruct),
		"voice_instruction": normalizeAudioProductionOneLine(req.VoiceInstruction),
		"temperature":       normalizeAudioProductionTemperature(req.Temperature),
		"updated_at":        time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新音频生产项目失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "项目已更新"})
}

func DeleteAudioProductionProject(c *gin.Context) {
	project, err := loadAudioProductionProjectOr404(c)
	if err != nil {
		return
	}
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.AudioProductionLine{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.AudioProductionProject{}, project.ID).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除音频生产项目失败"})
		return
	}
	_ = os.RemoveAll(audioProductionProjectDir(project.Code))
	c.JSON(http.StatusOK, gin.H{"message": "项目已删除"})
}

func ListAudioProductionLines(c *gin.Context) {
	project, err := loadAudioProductionProjectOr404(c)
	if err != nil {
		return
	}
	var lines []models.AudioProductionLine
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&lines).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取音频生产行失败"})
		return
	}
	c.JSON(http.StatusOK, lines)
}

func SaveAudioProductionLines(c *gin.Context) {
	project, err := loadAudioProductionProjectOr404(c)
	if err != nil {
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	lines, err := replaceAudioProductionLines(*project, req.Text)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.DB.Model(&models.AudioProductionProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"text":       strings.TrimSpace(req.Text),
		"updated_at": time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文本失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lines": lines})
}

func GenerateAudioProductionProjectLines(c *gin.Context) {
	project, err := loadAudioProductionProjectOr404(c)
	if err != nil {
		return
	}
	var req struct {
		Text       string `json:"text"`
		RandomSeed bool   `json:"random_seed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	lines, err := replaceAudioProductionLines(*project, req.Text)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.DB.Model(&models.AudioProductionProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"text":       strings.TrimSpace(req.Text),
		"updated_at": time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文本失败"})
		return
	}
	seedBase := getConfiguredGlobalSeed()
	for _, line := range lines {
		seed := seedBase
		if req.RandomSeed {
			seed = randomAudioProductionSeed()
		}
		taskID, err := startAudioProductionLineTask(&line, project, seed)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "提交生成任务失败"})
			return
		}
		_ = db.DB.Model(&models.AudioProductionLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
			"status":          audioCloneLineStatusGenerating,
			"current_task_id": taskID,
			"last_error":      "",
			"updated_at":      time.Now(),
		}).Error
	}
	c.JSON(http.StatusOK, gin.H{"message": "音频生产任务已提交", "lines": len(lines)})
}

func GenerateAudioProductionLine(c *gin.Context) {
	line, err := loadAudioProductionLineOr404(c)
	if err != nil {
		return
	}
	var project models.AudioProductionProject
	if err := db.DB.First(&project, line.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	var req struct {
		RandomSeed bool `json:"random_seed"`
	}
	_ = c.ShouldBindJSON(&req)
	seed := getConfiguredGlobalSeed()
	if req.RandomSeed {
		seed = randomAudioProductionSeed()
	}
	taskID, err := startAudioProductionLineTask(line, &project, seed)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交生成任务失败"})
		return
	}
	if err := db.DB.Model(&models.AudioProductionLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
		"status":          audioCloneLineStatusGenerating,
		"current_task_id": taskID,
		"last_error":      "",
		"updated_at":      time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新生成状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "单行生成任务已提交", "task_id": taskID})
}
