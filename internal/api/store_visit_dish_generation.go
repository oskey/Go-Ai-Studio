package api

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type storeVisitDishGenerationTaskPayload struct {
	ProjectID uint  `json:"project_id"`
	SpotID    uint  `json:"spot_id"`
	ItemID    uint  `json:"item_id"`
	Seed      int64 `json:"seed"`
}

type storeVisitDishGenerationMotionPreset struct {
	Key     string
	Label   string
	Prompts [5]string
}

type storeVisitDishGenerationSegment struct {
	Prompt          string  `json:"prompt"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type storeVisitDishGenerationItemView struct {
	ID                     uint                              `json:"id"`
	ProjectID              uint                              `json:"project_id"`
	SpotID                 uint                              `json:"spot_id"`
	SortOrder              int                               `json:"sort_order"`
	PresetKey              string                            `json:"preset_key"`
	Frames                 []string                          `json:"frames"`
	Segments               []storeVisitDishGenerationSegment `json:"segments"`
	VideoStatus            string                            `json:"video_status"`
	VideoCurrentTaskID     string                            `json:"video_current_task_id,omitempty"`
	VideoLastError         string                            `json:"video_last_error,omitempty"`
	GeneratedVideo         string                            `json:"generated_video"`
	VideoGeneratedWorkflow string                            `json:"video_generated_workflow,omitempty"`
	CreatedAt              time.Time                         `json:"created_at,omitempty"`
	UpdatedAt              time.Time                         `json:"updated_at,omitempty"`
}

var storeVisitDishGenerationMotionPresets = []storeVisitDishGenerationMotionPreset{
	{
		Key:   "cinematic_reveal",
		Label: "电影感拉远",
		Prompts: [5]string{
			"A premium commercial tabletop shot. The camera begins close to the main subject on the table and slowly pulls back with a smooth cinematic drift toward the left, gradually revealing more of the tabletop presentation and surrounding atmosphere.",
			"A premium commercial tabletop shot. The camera begins close to the main subject on the table and slowly pulls back with a smooth cinematic drift toward the right, gradually revealing more of the tabletop presentation and surrounding atmosphere.",
			"A premium commercial tabletop shot. The camera slowly lifts into a slightly higher angle, revealing more of the tabletop presentation while keeping the main subject visually dominant.",
			"A premium commercial tabletop shot. The camera gently lowers toward the tabletop presentation in a smooth, refined motion while the main subject remains stable and visually dominant.",
			"A premium commercial tabletop shot. The camera performs a slow, elegant orbit around the main subject on the table, keeping the tabletop presentation stable, detailed, and visually rich.",
		},
	},
	{
		Key:   "premium_tabletop",
		Label: "精品桌面展示",
		Prompts: [5]string{
			"Smooth cinematic drift to the left across the tabletop presentation, keeping the main subject as the hero of the frame with premium restaurant lighting and elegant shallow depth of field.",
			"Smooth cinematic drift to the right across the tabletop presentation, keeping the main subject as the hero of the frame with premium restaurant lighting and elegant shallow depth of field.",
			"Subtle upward reveal of the tabletop presentation, showing more of the arrangement while keeping the main subject visually rich and detailed.",
			"Subtle downward settle toward the tabletop presentation, keeping the main subject sharp, stable, and visually luxurious.",
			"Slow elegant orbit around the tabletop presentation, treating the main subject like a premium restaurant commercial hero shot.",
		},
	},
	{
		Key:   "luxury_orbit",
		Label: "高级环绕广告",
		Prompts: [5]string{
			"A luxury culinary commercial shot with the camera gliding left in a smooth, premium arc, while the tabletop presentation remains stable and refined.",
			"A luxury culinary commercial shot with the camera gliding right in a smooth, premium arc, while the tabletop presentation remains stable and refined.",
			"A luxury culinary commercial shot with a smooth upward reveal that opens the premium tabletop presentation and warm restaurant ambience.",
			"A luxury culinary commercial shot with a smooth downward refinement move that returns attention to the main subject on the table.",
			"A luxury culinary hero shot. The camera slowly circles around the tabletop presentation in a refined, controlled orbit while the subject remains detailed, glossy, and visually rich.",
		},
	},
	{
		Key:   "left_reveal",
		Label: "左侧展开",
		Prompts: [5]string{
			"A premium commercial tabletop shot. The camera begins close to the main subject on the table and slowly pulls back while drifting toward the left side of the tabletop presentation.",
			"A premium commercial tabletop shot. The camera continues revealing the tabletop with a gentle leftward cinematic drift, keeping the main subject visually dominant.",
			"A premium commercial tabletop shot. The camera subtly rises to show more of the tabletop layout from the left side while the subject remains stable.",
			"A premium commercial tabletop shot. The camera settles slightly downward toward the tabletop presentation, still favoring the left side reveal.",
			"A premium commercial tabletop shot. The camera finishes with a slow left-biased hero orbit around the tabletop presentation.",
		},
	},
	{
		Key:   "right_reveal",
		Label: "右侧展开",
		Prompts: [5]string{
			"A premium commercial tabletop shot. The camera begins close to the main subject on the table and slowly pulls back while drifting toward the right side of the tabletop presentation.",
			"A premium commercial tabletop shot. The camera continues revealing the tabletop with a gentle rightward cinematic drift, keeping the main subject visually dominant.",
			"A premium commercial tabletop shot. The camera subtly rises to show more of the tabletop layout from the right side while the subject remains stable.",
			"A premium commercial tabletop shot. The camera settles slightly downward toward the tabletop presentation, still favoring the right side reveal.",
			"A premium commercial tabletop shot. The camera finishes with a slow right-biased hero orbit around the tabletop presentation.",
		},
	},
	{
		Key:   "overhead_showcase",
		Label: "俯视展示",
		Prompts: [5]string{
			"A premium tabletop commercial shot. The camera starts close and smoothly lifts toward a higher angle, beginning to reveal more of the tabletop presentation.",
			"A premium tabletop commercial shot. The camera continues into a refined near-overhead reveal while the main subject remains crisp, stable, and visually rich.",
			"A premium tabletop commercial shot. The camera makes a subtle controlled overhead drift across the tabletop arrangement, showing more layout and atmosphere.",
			"A premium tabletop commercial shot. The camera gently lowers back toward the main subject while keeping the tabletop presentation elegant and stable.",
			"A premium tabletop hero shot. The camera ends with a slow overhead orbit around the tabletop presentation, maintaining a luxurious and cinematic mood.",
		},
	},
	{
		Key:   "slow_pullback",
		Label: "克制拉远",
		Prompts: [5]string{
			"A premium commercial tabletop shot. The camera begins close to the main subject and slowly pulls back in a smooth, controlled motion, revealing more of the tabletop.",
			"A premium commercial tabletop shot. The camera continues pulling back gently, showing more arrangement and atmosphere while keeping the main subject dominant.",
			"A premium commercial tabletop shot. The camera adds a subtle cinematic drift during the pullback, with warm lighting and refined tabletop mood.",
			"A premium commercial tabletop shot. The camera keeps pulling back with elegant stability, making the overall presentation feel premium and visually rich.",
			"A premium commercial tabletop shot. The camera ends with a refined wide tabletop reveal, keeping the main subject as the visual hero.",
		},
	},
}

func getStoreVisitDishGenerationMotionPreset(key string) storeVisitDishGenerationMotionPreset {
	normalized := strings.TrimSpace(strings.ToLower(key))
	for _, preset := range storeVisitDishGenerationMotionPresets {
		if preset.Key == normalized {
			return preset
		}
	}
	return storeVisitDishGenerationMotionPresets[0]
}

func getStoreVisitDishGenerationSegmentDuration(item models.StoreVisitDishGenerationItem) float64 {
	if item.SegmentDurationSeconds > 0 {
		return item.SegmentDurationSeconds
	}
	return 2
}

const storeVisitDishGenerationNegativePrompt = "subtitles, caption, text, text overlay, on-screen text, watermark, logo, lower-third, dialogue box, speech bubble, ui elements, readable text, chinese characters, english letters, words, menu text, poster text, sign text, overexposed, still image, low quality, jpeg artifacts, object rotation, table rotation"

func getStoreVisitDishGenerationPrompts(item models.StoreVisitDishGenerationItem) [5]string {
	preset := getStoreVisitDishGenerationMotionPreset(item.PresetKey)
	prompts := preset.Prompts
	custom := [5]string{
		strings.TrimSpace(item.Prompt1),
		strings.TrimSpace(item.Prompt2),
		strings.TrimSpace(item.Prompt3),
		strings.TrimSpace(item.Prompt4),
		strings.TrimSpace(item.Prompt5),
	}
	for idx, text := range custom {
		if text != "" {
			prompts[idx] = text
		}
	}
	return prompts
}

func encodeStoreVisitDishGenerationFrames(frames []string) string {
	data, _ := json.Marshal(frames)
	return string(data)
}

func decodeStoreVisitDishGenerationFrames(item models.StoreVisitDishGenerationItem) []string {
	var frames []string
	if strings.TrimSpace(item.FramesJSON) != "" {
		_ = json.Unmarshal([]byte(item.FramesJSON), &frames)
	}
	if len(frames) == 0 {
		for _, path := range []string{item.Frame1Image, item.Frame2Image, item.Frame3Image, item.Frame4Image, item.Frame5Image, item.Frame6Image} {
			if strings.TrimSpace(path) != "" {
				frames = append(frames, strings.TrimSpace(path))
			}
		}
	}
	return frames
}

func encodeStoreVisitDishGenerationSegments(segments []storeVisitDishGenerationSegment) string {
	data, _ := json.Marshal(segments)
	return string(data)
}

func decodeStoreVisitDishGenerationSegments(item models.StoreVisitDishGenerationItem) []storeVisitDishGenerationSegment {
	var segments []storeVisitDishGenerationSegment
	if strings.TrimSpace(item.SegmentsJSON) != "" {
		_ = json.Unmarshal([]byte(item.SegmentsJSON), &segments)
	}
	if len(segments) == 0 {
		prompts := getStoreVisitDishGenerationPrompts(item)
		duration := getStoreVisitDishGenerationSegmentDuration(item)
		for _, prompt := range prompts {
			if strings.TrimSpace(prompt) != "" {
				segments = append(segments, storeVisitDishGenerationSegment{
					Prompt:          strings.TrimSpace(prompt),
					DurationSeconds: duration,
				})
			}
		}
	}
	return segments
}

func buildStoreVisitDishGenerationItemView(item models.StoreVisitDishGenerationItem) storeVisitDishGenerationItemView {
	return storeVisitDishGenerationItemView{
		ID:                     item.ID,
		ProjectID:              item.ProjectID,
		SpotID:                 item.SpotID,
		SortOrder:              item.SortOrder,
		PresetKey:              item.PresetKey,
		Frames:                 decodeStoreVisitDishGenerationFrames(item),
		Segments:               decodeStoreVisitDishGenerationSegments(item),
		VideoStatus:            item.VideoStatus,
		VideoCurrentTaskID:     item.VideoCurrentTaskID,
		VideoLastError:         item.VideoLastError,
		GeneratedVideo:         item.GeneratedVideo,
		VideoGeneratedWorkflow: item.VideoGeneratedWorkflow,
		CreatedAt:              item.CreatedAt,
		UpdatedAt:              item.UpdatedAt,
	}
}

func buildStoreVisitDishGenerationSegmentsFromPreset(presetKey string, count int, defaultDuration float64) []storeVisitDishGenerationSegment {
	if count <= 0 {
		return nil
	}
	preset := getStoreVisitDishGenerationMotionPreset(presetKey)
	segments := make([]storeVisitDishGenerationSegment, 0, count)
	for i := 0; i < count; i++ {
		segments = append(segments, storeVisitDishGenerationSegment{
			Prompt:          preset.Prompts[i%len(preset.Prompts)],
			DurationSeconds: defaultDuration,
		})
	}
	return segments
}

func parseStoreVisitDishGenerationSegmentsJSON(raw string, presetKey string, expectedCount int) []storeVisitDishGenerationSegment {
	segments := []storeVisitDishGenerationSegment{}
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &segments)
	}
	if len(segments) == 0 {
		segments = buildStoreVisitDishGenerationSegmentsFromPreset(presetKey, expectedCount, 2)
	}
	for i := range segments {
		segments[i].Prompt = strings.TrimSpace(segments[i].Prompt)
		if segments[i].DurationSeconds <= 0 {
			segments[i].DurationSeconds = 2
		}
	}
	if expectedCount > 0 {
		if len(segments) > expectedCount {
			segments = segments[:expectedCount]
		}
		for len(segments) < expectedCount {
			presetSegments := buildStoreVisitDishGenerationSegmentsFromPreset(presetKey, expectedCount, 2)
			segments = append(segments, presetSegments[len(segments)])
		}
	}
	return segments
}

func loadStoreVisitDishGenerationItemOr404(c *gin.Context) (*models.StoreVisitDishGenerationItem, error) {
	itemID := strings.TrimSpace(c.Param("itemId"))
	var item models.StoreVisitDishGenerationItem
	if err := db.DB.First(&item, itemID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "菜品生成条目不存在"})
		return nil, err
	}
	return &item, nil
}

func listStoreVisitDishGenerationItemsBySpot(spotID uint) ([]models.StoreVisitDishGenerationItem, error) {
	var items []models.StoreVisitDishGenerationItem
	if err := db.DB.Where("spot_id = ?", spotID).Order("sort_order asc, id asc").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func shouldApplyStoreVisitDishGenerationTaskResult(itemID uint, taskID string) bool {
	var current models.StoreVisitDishGenerationItem
	if err := db.DB.Select("video_current_task_id").First(&current, itemID).Error; err != nil {
		return false
	}
	return strings.TrimSpace(current.VideoCurrentTaskID) == strings.TrimSpace(taskID)
}

func resetStoreVisitDishGenerationItemState(item *models.StoreVisitDishGenerationItem) error {
	if item == nil {
		return fmt.Errorf("菜品生成条目不存在")
	}
	if err := removeGeneratedVideoAsset(item.GeneratedVideo); err != nil {
		return err
	}
	return db.DB.Model(&models.StoreVisitDishGenerationItem{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
		"video_status":             "draft",
		"video_current_task_id":    "",
		"video_last_error":         "",
		"generated_video":          "",
		"video_generated_workflow": "",
		"updated_at":               time.Now(),
	}).Error
}

func deleteStoreVisitDishGenerationItemAssets(item models.StoreVisitDishGenerationItem) error {
	for _, path := range decodeStoreVisitDishGenerationFrames(item) {
		if err := removeGeneratedAsset(path); err != nil {
			return err
		}
	}
	if err := removeGeneratedVideoAsset(item.GeneratedVideo); err != nil {
		return err
	}
	return nil
}

func storeVisitDishGenerationFrameAbsPath(code string, spotKey string, itemID uint, index int, ext string) string {
	filename := fmt.Sprintf("%s_%d_frame_%d%s", spotKey, itemID, index, ext)
	return filepath.Join(storeVisitDishGenerationFramesDir(code, spotKey), filename)
}

func saveStoreVisitDishGenerationFrame(c *gin.Context, file *multipart.FileHeader, code string, spotKey string, itemID uint, index int) (string, string, error) {
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext == "" {
		ext = ".png"
	}
	dir := storeVisitDishGenerationFramesDir(code, spotKey)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	absPath := storeVisitDishGenerationFrameAbsPath(code, spotKey, itemID, index, ext)
	if err := c.SaveUploadedFile(file, absPath); err != nil {
		return "", "", err
	}
	return absPath, "/" + filepath.ToSlash(absPath), nil
}

func storeVisitDishGenerationFrameImages(item models.StoreVisitDishGenerationItem) []string {
	return decodeStoreVisitDishGenerationFrames(item)
}

func buildStoreVisitDishGenerationWorkflow(item models.StoreVisitDishGenerationItem, spot models.StoreVisitSpot, project models.StoreVisitProject, seed int64) (map[string]interface{}, string, error) {
	workflowJSON := map[string]interface{}{}
	if seed <= 0 {
		seed = getConfiguredGlobalSeed()
	}
	frames := storeVisitDishGenerationFrameImages(item)
	if len(frames) < 2 {
		return nil, "", fmt.Errorf("至少需要 2 张 key frame 图片")
	}
	segments := decodeStoreVisitDishGenerationSegments(item)
	if len(segments) != len(frames)-1 {
		return nil, "", fmt.Errorf("当前段数与图片数量不匹配")
	}

	width := spot.VideoWidth
	height := spot.VideoHeight
	if width <= 0 {
		width = storeVisitVideoWidth
	}
	if height <= 0 {
		height = storeVisitVideoHeight
	}
	fps := storeVisitDefaultVideoFPS

	nextNodeID := 1
	newNodeID := func() string {
		id := strconv.Itoa(nextNodeID)
		nextNodeID++
		return id
	}

	clipLoaderID := newNodeID()
	workflowJSON[clipLoaderID] = map[string]interface{}{
		"inputs": map[string]interface{}{
			"clip_name": "umt5_xxl_fp8_e4m3fn_scaled.safetensors",
			"type":      "wan",
			"device":    "default",
		},
		"class_type": "CLIPLoader",
	}
	negativeEncodeID := newNodeID()
	workflowJSON[negativeEncodeID] = map[string]interface{}{
		"inputs": map[string]interface{}{
			"text": storeVisitDishGenerationNegativePrompt,
			"clip": []interface{}{clipLoaderID, 0},
		},
		"class_type": "CLIPTextEncode",
	}
	unetHighID := newNodeID()
	workflowJSON[unetHighID] = map[string]interface{}{
		"inputs": map[string]interface{}{
			"unet_name":    "wan2.2_i2v_high_noise_14B_fp8_scaled.safetensors",
			"weight_dtype": "default",
		},
		"class_type": "UNETLoader",
	}
	unetLowID := newNodeID()
	workflowJSON[unetLowID] = map[string]interface{}{
		"inputs": map[string]interface{}{
			"unet_name":    "wan2.2_i2v_low_noise_14B_fp8_scaled.safetensors",
			"weight_dtype": "default",
		},
		"class_type": "UNETLoader",
	}
	loraLowID := newNodeID()
	workflowJSON[loraLowID] = map[string]interface{}{
		"inputs": map[string]interface{}{
			"lora_name":      "wan2.2_i2v_lightx2v_4steps_lora_v1_low_noise.safetensors",
			"strength_model": 1,
			"model":          []interface{}{unetLowID, 0},
		},
		"class_type": "LoraLoaderModelOnly",
	}
	loraHighID := newNodeID()
	workflowJSON[loraHighID] = map[string]interface{}{
		"inputs": map[string]interface{}{
			"lora_name":      "wan2.2_i2v_lightx2v_4steps_lora_v1_high_noise.safetensors",
			"strength_model": 1,
			"model":          []interface{}{unetHighID, 0},
		},
		"class_type": "LoraLoaderModelOnly",
	}
	modelLowID := newNodeID()
	workflowJSON[modelLowID] = map[string]interface{}{
		"inputs": map[string]interface{}{
			"shift": 5,
			"model": []interface{}{loraLowID, 0},
		},
		"class_type": "ModelSamplingSD3",
	}
	modelHighID := newNodeID()
	workflowJSON[modelHighID] = map[string]interface{}{
		"inputs": map[string]interface{}{
			"shift": 5,
			"model": []interface{}{loraHighID, 0},
		},
		"class_type": "ModelSamplingSD3",
	}
	vaeID := newNodeID()
	workflowJSON[vaeID] = map[string]interface{}{
		"inputs": map[string]interface{}{
			"vae_name": "wan_2.1_vae.safetensors",
		},
		"class_type": "VAELoader",
	}

	frameNodeIDs := make([]string, 0, len(frames))
	for idx, path := range frames {
		if strings.TrimSpace(path) == "" {
			return nil, "", fmt.Errorf("第 %d 张 key frame 图片缺失", idx+1)
		}
		absPath, err := assetWebPathToAbs(path)
		if err != nil {
			return nil, "", err
		}
		imageValue := absPath
		if uploadedName, err := UploadToComfyUIInput(absPath); err == nil && strings.TrimSpace(uploadedName) != "" {
			imageValue = uploadedName
		}
		nodeID := newNodeID()
		workflowJSON[nodeID] = map[string]interface{}{
			"inputs": map[string]interface{}{
				"image": imageValue,
			},
			"class_type": "LoadImage",
		}
		frameNodeIDs = append(frameNodeIDs, nodeID)
	}

	decodedNodeIDs := make([]string, 0, len(segments))
	for idx, segment := range segments {
		segmentPrompt := strings.TrimSpace(segment.Prompt)
		if segmentPrompt == "" {
			return nil, "", fmt.Errorf("第 %d 段提示词不能为空", idx+1)
		}
		segmentLength := int(float64(fps)*segment.DurationSeconds) + 1
		if segmentLength < 9 {
			segmentLength = 9
		}

		positiveEncodeID := newNodeID()
		workflowJSON[positiveEncodeID] = map[string]interface{}{
			"inputs": map[string]interface{}{
				"text": segmentPrompt,
				"clip": []interface{}{clipLoaderID, 0},
			},
			"class_type": "CLIPTextEncode",
		}
		wanID := newNodeID()
		workflowJSON[wanID] = map[string]interface{}{
			"inputs": map[string]interface{}{
				"width":       width,
				"height":      height,
				"length":      segmentLength,
				"batch_size":  1,
				"positive":    []interface{}{positiveEncodeID, 0},
				"negative":    []interface{}{negativeEncodeID, 0},
				"vae":         []interface{}{vaeID, 0},
				"start_image": []interface{}{frameNodeIDs[idx], 0},
				"end_image":   []interface{}{frameNodeIDs[idx+1], 0},
			},
			"class_type": "WanFirstLastFrameToVideo",
		}
		ksamplerHighID := newNodeID()
		workflowJSON[ksamplerHighID] = map[string]interface{}{
			"inputs": map[string]interface{}{
				"add_noise":                  "enable",
				"noise_seed":                 seed + int64(idx)*97 + 1,
				"steps":                      4,
				"cfg":                        1,
				"sampler_name":               "euler",
				"scheduler":                  "simple",
				"start_at_step":              0,
				"end_at_step":                2,
				"return_with_leftover_noise": "enable",
				"model":                      []interface{}{modelHighID, 0},
				"positive":                   []interface{}{wanID, 0},
				"negative":                   []interface{}{wanID, 1},
				"latent_image":               []interface{}{wanID, 2},
			},
			"class_type": "KSamplerAdvanced",
		}
		ksamplerLowID := newNodeID()
		workflowJSON[ksamplerLowID] = map[string]interface{}{
			"inputs": map[string]interface{}{
				"add_noise":                  "disable",
				"noise_seed":                 0,
				"steps":                      4,
				"cfg":                        1,
				"sampler_name":               "euler",
				"scheduler":                  "simple",
				"start_at_step":              2,
				"end_at_step":                10000,
				"return_with_leftover_noise": "disable",
				"model":                      []interface{}{modelLowID, 0},
				"positive":                   []interface{}{wanID, 0},
				"negative":                   []interface{}{wanID, 1},
				"latent_image":               []interface{}{ksamplerHighID, 0},
			},
			"class_type": "KSamplerAdvanced",
		}
		decodeID := newNodeID()
		workflowJSON[decodeID] = map[string]interface{}{
			"inputs": map[string]interface{}{
				"samples": []interface{}{ksamplerLowID, 0},
				"vae":     []interface{}{vaeID, 0},
			},
			"class_type": "VAEDecode",
		}
		decodedNodeIDs = append(decodedNodeIDs, decodeID)
	}

	finalImageSourceID := decodedNodeIDs[0]
	for idx := 1; idx < len(decodedNodeIDs); idx++ {
		batchID := newNodeID()
		workflowJSON[batchID] = map[string]interface{}{
			"inputs": map[string]interface{}{
				"image1": []interface{}{finalImageSourceID, 0},
				"image2": []interface{}{decodedNodeIDs[idx], 0},
			},
			"class_type": "ImageBatch",
		}
		finalImageSourceID = batchID
	}

	finalCreateVideoID := newNodeID()
	workflowJSON[finalCreateVideoID] = map[string]interface{}{
		"inputs": map[string]interface{}{
			"fps":    fps,
			"images": []interface{}{finalImageSourceID, 0},
		},
		"class_type": "CreateVideo",
	}
	saveVideoID := newNodeID()
	workflowJSON[saveVideoID] = map[string]interface{}{
		"inputs": map[string]interface{}{
			"filename_prefix": fmt.Sprintf("%s_%s_dish_video_%d", project.Code, getStoreVisitSpotFileKey(spot), item.ID),
			"format":          "auto",
			"codec":           "auto",
			"video-preview":   "",
			"video":           []interface{}{finalCreateVideoID, 0},
		},
		"class_type": "SaveVideo",
	}

	return workflowJSON, "dynamic_tabletop_keyframes", nil
}

func waitForStoreVisitDishGenerationVideoOutput(promptID string, projectCode string, spotKey string, itemID uint, shouldContinue func() bool) (string, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if shouldContinue != nil && !shouldContinue() {
			return "", fmt.Errorf("dish generation interrupted")
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
			saveDir := storeVisitDishGenerationVideosDir(projectCode, spotKey)
			if err := os.MkdirAll(saveDir, 0755); err != nil {
				return "", err
			}
			ext := filepath.Ext(filename)
			if ext == "" {
				ext = ".mp4"
			}
			saveFilename := fmt.Sprintf("%s_dish_%d_%d%s", spotKey, itemID, time.Now().UnixNano(), ext)
			savePath := filepath.Join(saveDir, saveFilename)
			if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
				return "", err
			}
			return "/" + filepath.ToSlash(savePath), nil
		}
	}
	return "", nil
}

func ListStoreVisitDishGenerationItems(c *gin.Context) {
	spot, err := loadStoreVisitSpotOr404(c)
	if err != nil {
		return
	}
	if normalizeStoreVisitSpotType(spot.SpotType, spot.Name) != storeVisitSpotTypeDishGeneration {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前区域不是菜品生成"})
		return
	}
	items, err := listStoreVisitDishGenerationItemsBySpot(spot.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取菜品生成条目失败"})
		return
	}
	views := make([]storeVisitDishGenerationItemView, 0, len(items))
	for _, item := range items {
		views = append(views, buildStoreVisitDishGenerationItemView(item))
	}
	c.JSON(http.StatusOK, views)
}

func CreateStoreVisitDishGenerationItem(c *gin.Context) {
	spot, err := loadStoreVisitSpotOr404(c)
	if err != nil {
		return
	}
	if normalizeStoreVisitSpotType(spot.SpotType, spot.Name) != storeVisitSpotTypeDishGeneration {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前区域不是菜品生成"})
		return
	}
	if spot.ImageStatus == "generating" || spot.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前区域仍在生成中，请稍后再试"})
		return
	}

	var project models.StoreVisitProject
	if err := db.DB.First(&project, spot.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}

	files := make([]*multipart.FileHeader, 0, 8)
	if form, err := c.MultipartForm(); err == nil && form != nil {
		if uploaded := form.File["frame_images"]; len(uploaded) > 0 {
			files = append(files, uploaded...)
		}
		if len(files) == 0 {
			for i := 1; i <= 64; i++ {
				key := fmt.Sprintf("frame%d_image", i)
				if uploaded := form.File[key]; len(uploaded) > 0 {
					files = append(files, uploaded[0])
				}
			}
		}
	}
	if len(files) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "至少需要上传 2 张 key frame 图片"})
		return
	}

	preset := getStoreVisitDishGenerationMotionPreset(c.PostForm("preset_key"))
	segments := parseStoreVisitDishGenerationSegmentsJSON(c.PostForm("segments_json"), preset.Key, len(files)-1)
	now := time.Now()
	spotKey := getStoreVisitSpotFileKey(*spot)
	item := models.StoreVisitDishGenerationItem{
		ProjectID:    project.ID,
		SpotID:       spot.ID,
		PresetKey:    preset.Key,
		VideoStatus:  "draft",
		SegmentsJSON: encodeStoreVisitDishGenerationSegments(segments),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	savedPaths := make([]string, 0, len(files))

	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.StoreVisitDishGenerationItem{}).Where("spot_id = ?", spot.ID).Count(&count).Error; err != nil {
			return err
		}
		item.SortOrder = int(count) + 1
		if err := tx.Create(&item).Error; err != nil {
			return err
		}

		framePaths := make([]string, 0, len(files))
		for idx, file := range files {
			absPath, webPath, err := saveStoreVisitDishGenerationFrame(c, file, project.Code, spotKey, item.ID, idx+1)
			if err != nil {
				return err
			}
			savedPaths = append(savedPaths, absPath)
			framePaths = append(framePaths, webPath)
		}
		updates := map[string]interface{}{
			"frames_json": encodeStoreVisitDishGenerationFrames(framePaths),
			"updated_at":  now,
		}
		return tx.Model(&models.StoreVisitDishGenerationItem{}).Where("id = ?", item.ID).Updates(updates).Error
	}); err != nil {
		for _, path := range savedPaths {
			_ = os.Remove(path)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建菜品生成条目失败"})
		return
	}

	var refreshed models.StoreVisitDishGenerationItem
	if err := db.DB.First(&refreshed, item.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取菜品生成条目失败"})
		return
	}
	BroadcastUpdate("store_visit_spot", spot.ID)
	c.JSON(http.StatusCreated, buildStoreVisitDishGenerationItemView(refreshed))
}

func UpdateStoreVisitDishGenerationItem(c *gin.Context) {
	item, err := loadStoreVisitDishGenerationItemOr404(c)
	if err != nil {
		return
	}
	if item.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前条目仍在生成中，请稍后再试"})
		return
	}

	var spot models.StoreVisitSpot
	if err := db.DB.First(&spot, item.SpotID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属区域不存在"})
		return
	}
	var project models.StoreVisitProject
	if err := db.DB.First(&project, item.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}

	preset := getStoreVisitDishGenerationMotionPreset(c.PostForm("preset_key"))
	existingFrames := decodeStoreVisitDishGenerationFrames(*item)
	newFiles := make([]*multipart.FileHeader, 0, 8)
	if form, err := c.MultipartForm(); err == nil && form != nil {
		if uploaded := form.File["frame_images"]; len(uploaded) > 0 {
			newFiles = append(newFiles, uploaded...)
		}
	}

	totalFrameCount := len(existingFrames) + len(newFiles)
	segments := parseStoreVisitDishGenerationSegmentsJSON(c.PostForm("segments_json"), preset.Key, totalFrameCount-1)
	now := time.Now()
	spotKey := getStoreVisitSpotFileKey(spot)
	savedPaths := make([]string, 0, len(newFiles))
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		framePaths := append([]string{}, existingFrames...)
		for _, file := range newFiles {
			absPath, webPath, err := saveStoreVisitDishGenerationFrame(c, file, project.Code, spotKey, item.ID, len(framePaths)+1)
			if err != nil {
				return err
			}
			savedPaths = append(savedPaths, absPath)
			framePaths = append(framePaths, webPath)
		}
		updates := map[string]interface{}{
			"preset_key":    preset.Key,
			"segments_json": encodeStoreVisitDishGenerationSegments(segments),
			"frames_json":   encodeStoreVisitDishGenerationFrames(framePaths),
			"updated_at":    now,
		}
		return tx.Model(&models.StoreVisitDishGenerationItem{}).Where("id = ?", item.ID).Updates(updates).Error
	}); err != nil {
		for _, path := range savedPaths {
			_ = os.Remove(path)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新菜品生成条目失败"})
		return
	}
	var refreshed models.StoreVisitDishGenerationItem
	if err := db.DB.First(&refreshed, item.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取菜品生成条目失败"})
		return
	}
	BroadcastUpdate("store_visit_spot", item.SpotID)
	c.JSON(http.StatusOK, buildStoreVisitDishGenerationItemView(refreshed))
}

func DeleteStoreVisitDishGenerationItem(c *gin.Context) {
	item, err := loadStoreVisitDishGenerationItemOr404(c)
	if err != nil {
		return
	}
	if item.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前条目仍在生成中，暂时不能删除"})
		return
	}
	spotID := item.SpotID
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := deleteStoreVisitDishGenerationItemAssets(*item); err != nil {
			return err
		}
		return tx.Delete(&models.StoreVisitDishGenerationItem{}, item.ID).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除菜品生成条目失败"})
		return
	}
	BroadcastUpdate("store_visit_spot", spotID)
	c.JSON(http.StatusOK, gin.H{"message": "菜品生成条目已删除"})
}

func ResetStoreVisitDishGenerationItemState(c *gin.Context) {
	item, err := loadStoreVisitDishGenerationItemOr404(c)
	if err != nil {
		return
	}
	if err := resetStoreVisitDishGenerationItemState(item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置菜品生成条目失败"})
		return
	}
	BroadcastUpdate("store_visit_spot", item.SpotID)
	c.JSON(http.StatusOK, gin.H{"message": "菜品生成条目状态已重置"})
}

func InterruptStoreVisitDishGenerationItemGeneration(c *gin.Context) {
	item, err := loadStoreVisitDishGenerationItemOr404(c)
	if err != nil {
		return
	}
	if item.VideoStatus != "generating" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前没有正在生成的任务"})
		return
	}
	if err := StopComfyUI(); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if err := resetStoreVisitDishGenerationItemState(item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "中断后重置菜品生成条目失败"})
		return
	}
	BroadcastUpdate("store_visit_spot", item.SpotID)
	c.JSON(http.StatusOK, gin.H{"message": "已中断当前生成任务"})
}

func GenerateStoreVisitDishGenerationItemVideo(c *gin.Context) {
	item, err := loadStoreVisitDishGenerationItemOr404(c)
	if err != nil {
		return
	}
	if item.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前条目仍在生成中，请等待完成后再操作"})
		return
	}
	frames := storeVisitDishGenerationFrameImages(*item)
	segments := decodeStoreVisitDishGenerationSegments(*item)
	if len(frames) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前条目的 key frame 图片不足，至少需要 2 张"})
		return
	}
	if len(segments) != len(frames)-1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前条目的过渡段配置不完整"})
		return
	}
	for _, path := range frames {
		if strings.TrimSpace(path) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请先补齐 key frame 图片"})
			return
		}
	}
	payload := storeVisitDishGenerationTaskPayload{
		ProjectID: item.ProjectID,
		SpotID:    item.SpotID,
		ItemID:    item.ID,
		Seed:      getConfiguredGlobalSeed(),
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("render_store_visit_dish_generation", payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交菜品生成任务失败"})
		return
	}
	now := time.Now()
	if err := db.DB.Model(&models.StoreVisitDishGenerationItem{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
		"video_status":             "generating",
		"video_current_task_id":    taskRecord.ID,
		"video_last_error":         "",
		"generated_video":          "",
		"video_generated_workflow": "",
		"updated_at":               now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新菜品生成状态失败"})
		return
	}
	BroadcastUpdate("store_visit_spot", item.SpotID)
	c.JSON(http.StatusOK, gin.H{"message": "菜品生成任务已提交", "task_id": taskRecord.ID})
}

func HandleRenderStoreVisitDishGenerationTask(t *models.Task) (interface{}, error) {
	var payload storeVisitDishGenerationTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	var project models.StoreVisitProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	var spot models.StoreVisitSpot
	if err := db.DB.First(&spot, payload.SpotID).Error; err != nil {
		return nil, fmt.Errorf("spot not found: %w", err)
	}
	var item models.StoreVisitDishGenerationItem
	if err := db.DB.First(&item, payload.ItemID).Error; err != nil {
		return nil, fmt.Errorf("dish generation item not found: %w", err)
	}

	workflowJSON, workflowLabel, err := buildStoreVisitDishGenerationWorkflow(item, spot, project, payload.Seed)
	if err != nil {
		if shouldApplyStoreVisitDishGenerationTaskResult(item.ID, t.ID) {
			_ = db.DB.Model(&models.StoreVisitDishGenerationItem{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}

	logComfyWorkflowPayload("Store Visit Dish Generation ComfyUI Payload", workflowLabel, workflowJSON)
	promptID, err := QueueComfyPrompt(workflowJSON)
	if err != nil {
		if shouldApplyStoreVisitDishGenerationTaskResult(item.ID, t.ID) {
			_ = db.DB.Model(&models.StoreVisitDishGenerationItem{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}

	Log(LogLevelInfo, "菜品生成视频已提交到 ComfyUI 队列", fmt.Sprintf("ProjectID: %d\nSpotID: %d\nItemID: %d\nPromptID: %s", project.ID, spot.ID, item.ID, promptID))
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 40, "")

	webPath, err := waitForStoreVisitDishGenerationVideoOutput(promptID, project.Code, getStoreVisitSpotFileKey(spot), item.ID, func() bool {
		return shouldApplyStoreVisitDishGenerationTaskResult(item.ID, t.ID)
	})
	if err != nil {
		if shouldApplyStoreVisitDishGenerationTaskResult(item.ID, t.ID) {
			_ = db.DB.Model(&models.StoreVisitDishGenerationItem{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}

	if !shouldApplyStoreVisitDishGenerationTaskResult(item.ID, t.ID) {
		return gin.H{"video_path": webPath, "workflow": workflowLabel, "applied": false}, nil
	}

	if err := db.DB.Model(&models.StoreVisitDishGenerationItem{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
		"video_status":             "generated",
		"video_current_task_id":    "",
		"video_last_error":         "",
		"generated_video":          webPath,
		"video_generated_workflow": workflowLabel,
		"updated_at":               time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	BroadcastUpdate("store_visit_spot", spot.ID)
	return gin.H{"video_path": webPath, "workflow": workflowLabel}, nil
}
