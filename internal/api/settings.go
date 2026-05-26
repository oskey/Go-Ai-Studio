package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/gin-gonic/gin"
)

const (
	KeyImageHeight          = "image_height"
	KeyImageWidth           = "image_width"
	KeyCharacterImageHeight = "character_image_height"
	KeyCharacterImageWidth  = "character_image_width"
	KeyOptimizeClothing     = "optimize_clothing"
	KeyVideoHeight          = "video_height"
	KeyVideoWidth           = "video_width"
	KeyVideoGenerationProvider   = "video_generation_provider"
	KeyJimengAPIBase             = "jimeng_api_base"
	KeyJimengAccessKey           = "jimeng_access_key"
	KeyJimengSecretKey           = "jimeng_secret_key"
	KeyJimengReqKey              = "jimeng_req_key"
	KeyJimengAspectRatio         = "jimeng_aspect_ratio"
	KeyJimengVideoWidth          = "jimeng_video_width"            // legacy migration only
	KeyJimengVideoHeight         = "jimeng_video_height"           // legacy migration only
	KeyJimengVideoDurationSeconds = "jimeng_video_duration_seconds" // legacy migration only
	KeyJimengVideoFrames         = "jimeng_video_frames"           // legacy migration only
	KeyLLMTimeoutMinutes         = "llm_timeout_minutes"
	KeyDefaultImageModel         = "default_image_model"
	KeyDefaultVideoModel         = "default_video_model"
	KeyGlobalSeed                = "global_seed"
	KeyStoreVisitImageReferenceOrder = "store_visit_image_reference_order"
	KeyGeneralGuideTransitionEngine = "general_guide_transition_engine"
	KeyComfyUIAddress            = "comfyui_api_address"
	KeyComfyUIModelsDir          = "comfyui_models_dir"
	KeyFFmpegPath                = "ffmpeg_path"
)

const (
	VideoGenerationProviderLocal  = "local"
	VideoGenerationProviderJimeng = "jimeng"
	StoreVisitImageOrderBloggerFirst = "blogger_first"
	StoreVisitImageOrderSceneFirst   = "scene_first"
	GeneralGuideTransitionEngineLTX23 = "ltx2_3"
	GeneralGuideTransitionEngineWan22 = "wan2_2"
	GeneralGuideTransitionEngineFFmpeg = "ffmpeg"
)

type jimengVideoPreset struct {
	Width       int
	Height      int
	AspectRatio string
	Label       string
}

var jimengVideoPresets = []jimengVideoPreset{
	{Width: 2176, Height: 928, AspectRatio: "21:9", Label: "2176 × 928（21:9）"},
	{Width: 1920, Height: 1088, AspectRatio: "16:9", Label: "1920 × 1088（16:9）"},
	{Width: 1664, Height: 1248, AspectRatio: "4:3", Label: "1664 × 1248（4:3）"},
	{Width: 1440, Height: 1440, AspectRatio: "1:1", Label: "1440 × 1440（1:1）"},
	{Width: 1248, Height: 1664, AspectRatio: "3:4", Label: "1248 × 1664（3:4）"},
	{Width: 1088, Height: 1920, AspectRatio: "9:16", Label: "1088 × 1920（9:16）"},
}

// InitDefaultSettings initializes system settings with default values if they don't exist
func InitDefaultSettings() {
	defaults := map[string]string{
		KeyImageHeight:          "1344",
		KeyImageWidth:           "768",
		KeyCharacterImageHeight: "1344",
		KeyCharacterImageWidth:  "768",
		KeyOptimizeClothing:     "false",
		KeyVideoHeight:          "640",
		KeyVideoWidth:           "640",
		KeyVideoGenerationProvider: VideoGenerationProviderLocal,
		KeyJimengAPIBase:           "https://visual.volcengineapi.com",
		KeyJimengAccessKey:         "",
		KeyJimengSecretKey:         "",
		KeyJimengReqKey:            "jimeng_ti2v_v30_pro",
		KeyJimengAspectRatio:       "16:9",
		KeyLLMTimeoutMinutes:    "30",
		KeyDefaultImageModel:    "",
		KeyDefaultVideoModel:    "",
		KeyGlobalSeed:           "-1",
		KeyStoreVisitImageReferenceOrder: StoreVisitImageOrderBloggerFirst,
		KeyGeneralGuideTransitionEngine:  GeneralGuideTransitionEngineLTX23,
		KeyComfyUIAddress:       "127.0.0.1:8188",
		KeyComfyUIModelsDir:     "",
		KeyFFmpegPath:           "",
	}

	for key, value := range defaults {
		var count int64
		db.DB.Model(&models.SystemSettings{}).Where("key = ?", key).Count(&count)
		if count == 0 {
			if key == KeyJimengAspectRatio {
				var legacyWidth models.SystemSettings
				var legacyHeight models.SystemSettings
				if err := db.DB.Where("key = ?", KeyJimengVideoWidth).First(&legacyWidth).Error; err == nil {
					if err := db.DB.Where("key = ?", KeyJimengVideoHeight).First(&legacyHeight).Error; err == nil {
						preset := resolveJimengLegacyVideoPreset(strings.TrimSpace(legacyWidth.Value), strings.TrimSpace(legacyHeight.Value))
						if preset.AspectRatio != "" {
							value = preset.AspectRatio
						}
					}
				}
			}
			setting := models.SystemSettings{
				Key:         key,
				Value:       value,
				Description: getDescription(key),
			}
			db.DB.Create(&setting)
		}
	}
	db.DB.Where("key IN ?", []string{
		"video_fps",
		"video_length",
		"llm_prompt_lang",
		KeyJimengVideoWidth,
		KeyJimengVideoHeight,
		KeyJimengVideoDurationSeconds,
		KeyJimengVideoFrames,
	}).Delete(&models.SystemSettings{})
	Log(LogLevelInfo, "初始化设置", "已检查并初始化默认系统设置")
}

func getDescription(key string) string {
	switch key {
	case KeyImageHeight:
		return "默认场景图片生成高度"
	case KeyImageWidth:
		return "默认场景图片生成宽度"
	case KeyCharacterImageHeight:
		return "默认角色预览图片生成高度"
	case KeyCharacterImageWidth:
		return "默认角色预览图片生成宽度"
	case KeyOptimizeClothing:
		return "是否启用服装优化"
	case KeyVideoHeight:
		return "默认视频生成高度"
	case KeyVideoWidth:
		return "默认视频生成宽度"
	case KeyVideoGenerationProvider:
		return "视频生成接入方式（local/jimeng）"
	case KeyJimengAPIBase:
		return "即梦视频 API 地址"
	case KeyJimengAccessKey:
		return "即梦视频 AccessKey"
	case KeyJimengSecretKey:
		return "即梦视频 SecretKey"
	case KeyJimengReqKey:
		return "即梦视频 req_key"
	case KeyJimengAspectRatio:
		return "即梦视频画幅比例预设"
	case KeyLLMTimeoutMinutes:
		return "LLM 请求超时时间（分钟）"
	case KeyDefaultImageModel:
		return "默认图片生成模型工作流"
	case KeyDefaultVideoModel:
		return "默认视频生成模型工作流"
	case KeyGlobalSeed:
		return "全局默认种子 (Seed)"
	case KeyStoreVisitImageReferenceOrder:
		return "博主探店图片生成中，Qwen Image Edit 的人物图和场景图注入顺序"
	case KeyGeneralGuideTransitionEngine:
		return "综合讲解转场引擎（ltx2_3 / wan2_2 / ffmpeg）"
	case KeyComfyUIAddress:
		return "ComfyUI API 地址"
	case KeyComfyUIModelsDir:
		return "ComfyUI 模型根目录"
	case KeyFFmpegPath:
		return "FFmpeg 可执行文件绝对路径（留空则尝试从 PATH 查找）"
	default:
		return ""
	}
}

func getConfiguredSceneImageSize() (int, int) {
	var settings []models.SystemSettings
	db.DB.Find(&settings)

	width := 0
	height := 0
	for _, s := range settings {
		switch s.Key {
		case KeyImageWidth:
			width, _ = strconv.Atoi(strings.TrimSpace(s.Value))
		case KeyImageHeight:
			height, _ = strconv.Atoi(strings.TrimSpace(s.Value))
		}
	}

	if width <= 0 {
		width, _ = strconv.Atoi(defaultSettingValue(KeyImageWidth))
	}
	if height <= 0 {
		height, _ = strconv.Atoi(defaultSettingValue(KeyImageHeight))
	}
	if width <= 0 {
		width = 768
	}
	if height <= 0 {
		height = 1344
	}

	return width, height
}

func getConfiguredVideoSize() (int, int) {
	var settings []models.SystemSettings
	db.DB.Find(&settings)

	width := 0
	height := 0
	for _, s := range settings {
		switch s.Key {
		case KeyVideoWidth:
			width, _ = strconv.Atoi(strings.TrimSpace(s.Value))
		case KeyVideoHeight:
			height, _ = strconv.Atoi(strings.TrimSpace(s.Value))
		}
	}

	if width <= 0 {
		width, _ = strconv.Atoi(defaultSettingValue(KeyVideoWidth))
	}
	if height <= 0 {
		height, _ = strconv.Atoi(defaultSettingValue(KeyVideoHeight))
	}
	if width <= 0 {
		width = 640
	}
	if height <= 0 {
		height = 640
	}

	return width, height
}

func normalizeVideoGenerationProvider(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", VideoGenerationProviderLocal:
		return VideoGenerationProviderLocal
	case VideoGenerationProviderJimeng:
		return VideoGenerationProviderJimeng
	default:
		return VideoGenerationProviderLocal
	}
}

func getConfiguredVideoGenerationProvider() string {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyVideoGenerationProvider).First(&setting).Error; err != nil {
		return normalizeVideoGenerationProvider(defaultSettingValue(KeyVideoGenerationProvider))
	}
	return normalizeVideoGenerationProvider(setting.Value)
}

func getConfiguredJimengAPIBase() string {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyJimengAPIBase).First(&setting).Error; err == nil {
		value := strings.TrimSpace(setting.Value)
		if value != "" {
			return value
		}
	}
	return defaultSettingValue(KeyJimengAPIBase)
}

func getConfiguredJimengReqKey() string {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyJimengReqKey).First(&setting).Error; err == nil {
		value := strings.TrimSpace(setting.Value)
		if value != "" {
			return value
		}
	}
	return defaultSettingValue(KeyJimengReqKey)
}

func getConfiguredJimengCredentials() (string, string) {
	var settings []models.SystemSettings
	db.DB.Find(&settings)

	var ak string
	var sk string
	for _, s := range settings {
		switch s.Key {
		case KeyJimengAccessKey:
			ak = strings.TrimSpace(s.Value)
		case KeyJimengSecretKey:
			sk = strings.TrimSpace(s.Value)
		}
	}
	return ak, sk
}

func getConfiguredJimengAspectRatio() string {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyJimengAspectRatio).First(&setting).Error; err == nil {
		return normalizeJimengAspectRatio(setting.Value)
	}
	return defaultSettingValue(KeyJimengAspectRatio)
}

func getConfiguredJimengVideoPreset() jimengVideoPreset {
	aspectRatio := getConfiguredJimengAspectRatio()
	for _, preset := range jimengVideoPresets {
		if preset.AspectRatio == aspectRatio {
			return preset
		}
	}
	return jimengVideoPresets[1]
}

func resolveJimengLegacyVideoPreset(widthRaw string, heightRaw string) jimengVideoPreset {
	width, _ := strconv.Atoi(strings.TrimSpace(widthRaw))
	height, _ := strconv.Atoi(strings.TrimSpace(heightRaw))
	for _, preset := range jimengVideoPresets {
		if preset.Width == width && preset.Height == height {
			return preset
		}
	}
	return jimengVideoPresets[1]
}

func normalizeJimengAspectRatio(raw string) string {
	value := strings.TrimSpace(raw)
	for _, preset := range jimengVideoPresets {
		if preset.AspectRatio == value {
			return value
		}
	}
	return defaultSettingValue(KeyJimengAspectRatio)
}

func isLegacyJimengSettingKey(key string) bool {
	switch key {
	case KeyJimengVideoWidth, KeyJimengVideoHeight, KeyJimengVideoDurationSeconds, KeyJimengVideoFrames:
		return true
	default:
		return false
	}
}

func getConfiguredCharacterImageSize() (int, int) {
	var settings []models.SystemSettings
	db.DB.Find(&settings)

	width := 0
	height := 0
	sceneWidth := 0
	sceneHeight := 0
	for _, s := range settings {
		switch s.Key {
		case KeyCharacterImageWidth:
			width, _ = strconv.Atoi(strings.TrimSpace(s.Value))
		case KeyCharacterImageHeight:
			height, _ = strconv.Atoi(strings.TrimSpace(s.Value))
		case KeyImageWidth:
			sceneWidth, _ = strconv.Atoi(strings.TrimSpace(s.Value))
		case KeyImageHeight:
			sceneHeight, _ = strconv.Atoi(strings.TrimSpace(s.Value))
		}
	}

	if width <= 0 {
		width = sceneWidth
	}
	if height <= 0 {
		height = sceneHeight
	}
	if width <= 0 {
		width, _ = strconv.Atoi(defaultSettingValue(KeyCharacterImageWidth))
	}
	if height <= 0 {
		height, _ = strconv.Atoi(defaultSettingValue(KeyCharacterImageHeight))
	}
	if width <= 0 {
		width = 768
	}
	if height <= 0 {
		height = 1344
	}

	return width, height
}

func getConfiguredGlobalSeed() int64 {
	var settings []models.SystemSettings
	db.DB.Find(&settings)

	for _, s := range settings {
		if s.Key != KeyGlobalSeed {
			continue
		}
		seed, err := strconv.ParseInt(strings.TrimSpace(s.Value), 10, 64)
		if err == nil {
			return seed
		}
		break
	}

	seed, _ := strconv.ParseInt(defaultSettingValue(KeyGlobalSeed), 10, 64)
	return seed
}

func normalizeStoreVisitImageReferenceOrder(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", StoreVisitImageOrderBloggerFirst:
		return StoreVisitImageOrderBloggerFirst
	case StoreVisitImageOrderSceneFirst:
		return StoreVisitImageOrderSceneFirst
	default:
		return StoreVisitImageOrderBloggerFirst
	}
}

func getConfiguredStoreVisitImageReferenceOrder() string {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyStoreVisitImageReferenceOrder).First(&setting).Error; err == nil {
		return normalizeStoreVisitImageReferenceOrder(setting.Value)
	}
	return normalizeStoreVisitImageReferenceOrder(defaultSettingValue(KeyStoreVisitImageReferenceOrder))
}

func normalizeGeneralGuideTransitionEngine(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", GeneralGuideTransitionEngineLTX23:
		return GeneralGuideTransitionEngineLTX23
	case GeneralGuideTransitionEngineWan22:
		return GeneralGuideTransitionEngineWan22
	case GeneralGuideTransitionEngineFFmpeg:
		return GeneralGuideTransitionEngineFFmpeg
	default:
		return GeneralGuideTransitionEngineLTX23
	}
}

func getConfiguredGeneralGuideTransitionEngine() string {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyGeneralGuideTransitionEngine).First(&setting).Error; err == nil {
		return normalizeGeneralGuideTransitionEngine(setting.Value)
	}
	return normalizeGeneralGuideTransitionEngine(defaultSettingValue(KeyGeneralGuideTransitionEngine))
}

func defaultSettingValue(key string) string {
	switch key {
	case KeyImageHeight:
		return "1344"
	case KeyImageWidth:
		return "768"
	case KeyCharacterImageHeight:
		return "1344"
	case KeyCharacterImageWidth:
		return "768"
	case KeyOptimizeClothing:
		return "false"
	case KeyVideoHeight:
		return "640"
	case KeyVideoWidth:
		return "640"
	case KeyVideoGenerationProvider:
		return VideoGenerationProviderLocal
	case KeyJimengAPIBase:
		return "https://visual.volcengineapi.com"
	case KeyJimengAccessKey:
		return ""
	case KeyJimengSecretKey:
		return ""
	case KeyJimengReqKey:
		return "jimeng_ti2v_v30_pro"
	case KeyJimengAspectRatio:
		return "16:9"
	case KeyLLMTimeoutMinutes:
		return "30"
	case KeyDefaultImageModel:
		return ""
	case KeyDefaultVideoModel:
		return ""
	case KeyGlobalSeed:
		return "-1"
	case KeyStoreVisitImageReferenceOrder:
		return StoreVisitImageOrderBloggerFirst
	case KeyGeneralGuideTransitionEngine:
		return GeneralGuideTransitionEngineLTX23
	case KeyComfyUIAddress:
		return "127.0.0.1:8188"
	case KeyComfyUIModelsDir:
		return ""
	case KeyFFmpegPath:
		return ""
	default:
		return ""
	}
}

func buildSceneImageResolutionInstruction() string {
	width, height := getConfiguredSceneImageSize()
	frameType := "横幅"
	if height > width {
		frameType = "竖幅"
	} else if height == width {
		frameType = "方幅"
	}

	return fmt.Sprintf(`当前场景图目标分辨率为宽%d、高%d（%s）。你必须按这个画幅推理人物承载量和空间感，而不是假设画面可以无限容纳人物。
- 这组宽高数字只用于你在内部推理构图、景别和人物承载量，禁止在 description、prompt_pos_screen_zh、prompt_neg_screen_zh、video_fingerprint、style_zh、player_desc_zh 中原样回填“宽%d高%d”“%dx%d”“横幅%d×%d”这类尺寸字样；若确实需要表达画幅，只能概括成“横向宽画幅”“竖向窄画幅”或“方形画幅”，不要写具体数字。
- 在这种画幅里，近景、中近景、半身近景、肩颈特写、脸部特写通常只适合 1 个清晰主位；若仍需第二人入画，优先使用过肩、dirty single、边缘半脸、前景肩背、远一层听位或模糊陪体，不要把两张清晰脸和两个完整上半身硬塞进同一紧画幅。
- 两名正式角色都需要清楚可辨时，优先改成中景双人 two-shot、profile two-shot、over-the-shoulder、同侧并行或有清楚前后层次的构图。
- 三名正式角色都需要清楚可辨时，只适合更宽的中远景、建立镜头、群像反应、围观站位、关系交代或打斗站位；不适合近景对白镜头。
- 对 Qwen Image / Z-Image 而言，当前画幅里没有写出来的可见身份锚点，不要假设模型会自动从角色预览里补完。
- 因此，若当前画面只拍到腰部以上、胸口以上、肩颈、脸部或局部身体，你必须把这个裁切范围内真正可见的性别身份、脸部、基础发型/发饰、肩颈与体态轮廓、当前上半身服装结构、主副配色、当前可见装备与持物完整写出；全身镜头则必须把全身可见锚点写全。`, width, height, frameType, width, height, width, height, width, height)
}

// GetSettings retrieves all system settings
func GetSettings(c *gin.Context) {
	var settings []models.SystemSettings
	if err := db.DB.Find(&settings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}

	// Convert to map for easier frontend consumption
	settingsMap := make(map[string]interface{})
	legacyJimengWidth := ""
	legacyJimengHeight := ""
	hasJimengAspectRatio := false
	for _, s := range settings {
		if isLegacyJimengSettingKey(s.Key) {
			switch s.Key {
			case KeyJimengVideoWidth:
				legacyJimengWidth = s.Value
			case KeyJimengVideoHeight:
				legacyJimengHeight = s.Value
			}
			continue
		}
		// Convert boolean strings to actual booleans for JSON
		if s.Key == KeyOptimizeClothing {
			val, _ := strconv.ParseBool(s.Value)
			settingsMap[s.Key] = val
		} else if s.Key == KeyJimengAspectRatio {
			hasJimengAspectRatio = true
			settingsMap[s.Key] = normalizeJimengAspectRatio(s.Value)
		} else if s.Key == KeyGeneralGuideTransitionEngine {
			settingsMap[s.Key] = normalizeGeneralGuideTransitionEngine(s.Value)
		} else {
			settingsMap[s.Key] = s.Value
		}
	}
	if !hasJimengAspectRatio {
		settingsMap[KeyJimengAspectRatio] = resolveJimengLegacyVideoPreset(legacyJimengWidth, legacyJimengHeight).AspectRatio
	}

	c.JSON(http.StatusOK, settingsMap)
}

// UpdateSettings updates system settings
func UpdateSettings(c *gin.Context) {
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx := db.DB.Begin()
	for key, value := range updates {
		strValue := ""
		// Handle different types
		switch v := value.(type) {
		case bool:
			strValue = strconv.FormatBool(v)
		case float64:
			strValue = strconv.FormatFloat(v, 'f', -1, 64)
		case string:
			strValue = v
		default:
			strValue = fmt.Sprintf("%v", v)
		}

		if err := tx.Model(&models.SystemSettings{}).Where("key = ?", key).Update("value", strValue).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update setting: " + key})
			return
		}
	}
	tx.Commit()

	Log(LogLevelInfo, "更新设置", "用户更新了系统设置")
	c.JSON(http.StatusOK, gin.H{"message": "Settings updated successfully"})
}
