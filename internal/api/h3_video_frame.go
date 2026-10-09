package api

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kt-ai-studio/internal/models"
)

const (
	h3T2VWorkflowFileName       = "minimax_h3_t2v-gguf-api.json"
	h3Ref2VWorkflowFileName     = "minimax_h3_ref2v-gguf-api.json"
	h3VideoFrameDurationSeconds = 0.1
	h3VideoFrameMaxPixels       = 980000
	h3VideoFrameSizeMultiple    = 16
)

// h3RefImageSlotLimit 是 ref2v 工作流 MiniMaxH3ReferenceToVideo 参考图通道的上限：
// ref_images 为 Autogrow（max=9，ref_image_0..8），其中 ref_image_0 固定留给场景参考图
// （防背景漂移），因此机制 A 的角色参考图最多占用 ref_image_1..8 共 8 张。
const h3RefImageSlotLimit = 9

// h3ReferenceTagPattern 匹配机制 A 提示词中的角色参考图引用标记「@图N」。
var h3ReferenceTagPattern = regexp.MustCompile(`@图(\d+)`)

// h3VideoFramePromptPreset 是 H3 抽帧附加提示词的内置预设，供设置页下拉框直接选用。
type h3VideoFramePromptPreset struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

// h3VideoFramePromptPresets 是预设文本的唯一真源（设置页下拉与默认值都取自这里），首项即默认值。
// 注意：H3 t2v 工作流没有 negative 输入，所有禁止项只能写进正向提示词并置于末尾。
var h3VideoFramePromptPresets = []h3VideoFramePromptPreset{
	{
		ID:    "compact",
		Label: "精简版（推荐）",
		Text:  "固定机位、固定焦段、固定构图的静态锁定镜头，全片是同一时刻的一张高分辨率照片，完全冻结，不是慢动作也不是升格。全片所有帧完全一致，人物五官、发型、服装、姿势与位置无漂移变形，曝光、色温、色调、锐度保持不变。上文如有动作、运动或镜头调度描述，一律理解为该动作的定格瞬间，不产生实际运动。禁止：相机运动与镜头切换、眨眼、呼吸、口型与表情变化、头发与衣物摆动、风吹叶动、水面波动、烟雾尘埃、人群走动、运动模糊与拖影、噪点闪烁与纹理游移、明暗色彩跳变、字幕文字水印、声音。",
	},
	{
		ID:    "full",
		Label: "完整版（约束最多，最稳定）",
		Text:  "静态锁定镜头、固定机位与焦段、固定构图，全片为同一时刻的一张高分辨率照片，完全冻结，非慢动作、非升格、非定格动画。全片所有帧完全相同：人物五官、发型、发色、服装、配饰、姿势与位置完全一致，无漂移、无变形、无人数增减；曝光、白平衡、色温、对比度、饱和度、锐度、颗粒全片不变；无景深变化、无自动对焦呼吸、无焦段变化、无视差。上文任何动作、运动、镜头调度或情绪变化描述，一律理解为该动作的定格瞬间，不产生实际运动。严格禁止：推、拉、摇、移、跟、升、降、环绕、手持抖动、镜头切换、淡入淡出、黑帧白闪；眨眼、呼吸起伏、口型变化、表情渐变、肢体移动、头发与衣物摆动；风、云移、树叶与窗帘与旗帜飘动、水面波动、烟雾、尘埃、雨雪、火花、影子与光斑移动、人群走动、车辆驶过、物体进出画面；运动模糊、拖影、重影、残影、双重曝光；噪点闪烁、纹理游移、摩尔纹、压缩伪影跳动、亮度与色彩跳变；字幕、文字、水印；声音。",
	},
	{
		ID:    "minimal",
		Label: "极简版（怕稀释场景描述时用）",
		Text:  "静态锁定镜头，固定机位与焦段，全片是同一时刻的高分辨率照片，完全冻结（非慢动作、非升格）；全片所有帧一致，人物与场景无漂移变形；上文动作描述一律理解为定格瞬间。无相机运动与镜头切换，无运动模糊与拖影，无闪烁与明暗跳变，无声音。",
	},
}

func h3VideoFrameDefaultPrompt() string {
	if len(h3VideoFramePromptPresets) == 0 {
		return ""
	}
	return h3VideoFramePromptPresets[0].Text
}

// useH3VideoFrameMode 判断当前图片生成是否走 MiniMax H3 短视频抽帧。
func useH3VideoFrameMode() bool {
	return getConfiguredImageGenMode() == ImageGenModeH3VideoFrame
}

// findH3T2VWorkflowFile 按文件名定位内置 H3 t2v 工作流。
// 该工作流的 SaveVideo prefix 与 i2v 相同，不能依赖 WorkflowName 匹配。
func findH3T2VWorkflowFile() (string, error) {
	files, _ := filepath.Glob(filepath.Join("workflows", "*.json"))
	for _, file := range files {
		if strings.EqualFold(filepath.Base(file), h3T2VWorkflowFileName) {
			return file, nil
		}
	}
	return "", fmt.Errorf("H3 t2v workflow '%s' not found", h3T2VWorkflowFileName)
}

func findH3Ref2VWorkflowFile() (string, error) {
	files, _ := filepath.Glob(filepath.Join("workflows", "*.json"))
	for _, file := range files {
		if strings.EqualFold(filepath.Base(file), h3Ref2VWorkflowFileName) {
			return file, nil
		}
	}
	return "", fmt.Errorf("H3 ref2v workflow '%s' not found", h3Ref2VWorkflowFileName)
}

// stripH3Ref2VExampleAssets 移除 ref2v 官方模板中残留的示例参考素材节点
// （LoadAudio / LoadVideo / GetVideoComponents）及其在 ReferenceToVideo 节点上的输入引用。
// 场景图仅使用参考图，官方示例的音频/视频文件不存在于 ComfyUI input，不剥离会导致提交校验失败
// （prompt_outputs_failed_validation: LoadAudio/LoadVideo 文件缺失）。
func stripH3Ref2VExampleAssets(wfJSON map[string]interface{}) {
	var orphanIDs []string
	for id, node := range wfJSON {
		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		classType, _ := nodeMap["class_type"].(string)
		inputs, _ := nodeMap["inputs"].(map[string]interface{})
		switch classType {
		case "LoadAudio", "LoadVideo", "GetVideoComponents":
			orphanIDs = append(orphanIDs, id)
		case "MiniMaxH3ReferenceToVideo":
			if inputs == nil {
				continue
			}
			// ref_videos.ref_video_0 / ref_video_audios.ref_video_audio_0 指向 GetVideoComponents(152/156)，
			// ref_audios.ref_audio_0 指向 LoadAudio(153)。仅保留 ref_images.* 参考图通道。
			for key := range inputs {
				if strings.HasPrefix(key, "ref_videos") || strings.HasPrefix(key, "ref_video_audios") || strings.HasPrefix(key, "ref_audios") {
					delete(inputs, key)
				}
			}
		}
	}
	for _, id := range orphanIDs {
		delete(wfJSON, id)
	}
}

// normalizeH3VideoFrameSize 保持宽高比等比缩小至 H3 分辨率上限内，并对齐到 16 的倍数。
func normalizeH3VideoFrameSize(width, height int) (int, int) {
	if width <= 0 || height <= 0 {
		width, height = 768, 1344
	}
	if width*height > h3VideoFrameMaxPixels {
		scale := math.Sqrt(float64(h3VideoFrameMaxPixels) / float64(width*height))
		width = int(math.Floor(float64(width) * scale))
		height = int(math.Floor(float64(height) * scale))
	}
	return alignDownH3Size(width), alignDownH3Size(height)
}

func alignDownH3Size(value int) int {
	aligned := value / h3VideoFrameSizeMultiple * h3VideoFrameSizeMultiple
	if aligned < h3VideoFrameSizeMultiple {
		return h3VideoFrameSizeMultiple
	}
	return aligned
}

// injectH3T2VParams 注入提示词/种子/尺寸/时长。
func injectH3T2VParams(wfJSON map[string]interface{}, meta *models.WorkflowMetadata, positivePrompt string, seed int64, width, height int) {
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
	setInput(meta.PositiveNodeID, meta.PositiveInputKey, positivePrompt)
	if meta.NegativeNodeID != "" {
		setInput(meta.NegativeNodeID, meta.NegativeInputKey, "")
	}
	setInput(meta.SeedNodeID, meta.SeedInputKey, seed)
	setInput(meta.WidthNodeID, meta.WidthInputKey, width)
	setInput(meta.HeightNodeID, meta.HeightInputKey, height)
	injectH3Duration(wfJSON, h3VideoFrameDurationSeconds)
}

// injectH3Duration 找到 title 含 duration 的 PrimitiveFloat（ComfyMathExpression 的时长输入）并设为指定秒数。
func injectH3Duration(wfJSON map[string]interface{}, seconds float64) {
	for _, node := range wfJSON {
		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		if classType, _ := nodeMap["class_type"].(string); classType != "PrimitiveFloat" {
			continue
		}
		metaMap, _ := nodeMap["_meta"].(map[string]interface{})
		title, _ := metaMap["title"].(string)
		if !strings.Contains(strings.ToLower(title), "duration") {
			continue
		}
		if inputs, ok := nodeMap["inputs"].(map[string]interface{}); ok {
			inputs["value"] = seconds
		}
	}
}

// isVideoOutputFilename 通过扩展名判断 ComfyUI 输出是视频而非图片。
func isVideoOutputFilename(name string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".mp4", ".webm", ".mov", ".mkv", ".gif":
		return true
	default:
		return false
	}
}

// resolveImageOrVideoOutput 从 ComfyUI 单节点输出中取出首个媒体引用，并判断其是否为视频。
func resolveImageOrVideoOutput(nodeOutput map[string]interface{}) (map[string]interface{}, bool, bool) {
	if videoData, ok := firstComfyOutputItem(nodeOutput, "gifs"); ok {
		return videoData, true, true
	}
	imgData, ok := firstComfyOutputItem(nodeOutput, "images")
	if !ok {
		return nil, false, false
	}
	filename, _ := imgData["filename"].(string)
	if filename == "" {
		return nil, false, false
	}
	return imgData, isVideoOutputFilename(filename), true
}

func firstComfyOutputItem(nodeOutput map[string]interface{}, key string) (map[string]interface{}, bool) {
	items, ok := nodeOutput[key].([]interface{})
	if !ok || len(items) == 0 {
		return nil, false
	}
	item, ok := items[0].(map[string]interface{})
	if !ok {
		return nil, false
	}
	return item, true
}

// downloadHistoryVideoAndExtractFrame 下载视频输出并抽取中间帧为 PNG，返回 web 路径。
func downloadHistoryVideoAndExtractFrame(fileData map[string]interface{}, saveDir string, saveFilename string) (string, error) {
	filename, _ := fileData["filename"].(string)
	subfolder, _ := fileData["subfolder"].(string)
	typeStr, _ := fileData["type"].(string)
	if filename == "" {
		return "", fmt.Errorf("empty video filename in comfyui output")
	}
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return "", err
	}

	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".mp4"
	}
	tempVideoPath := filepath.Join(saveDir, fmt.Sprintf(".h3_tmp_%d%s", time.Now().UnixNano(), ext))
	if err := DownloadComfyImage(filename, subfolder, typeStr, tempVideoPath); err != nil {
		return "", err
	}
	defer os.Remove(tempVideoPath)

	savePath := filepath.Join(saveDir, saveFilename)
	if err := extractFrameFromVideo(tempVideoPath, savePath, getConfiguredH3FramePick()); err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(savePath), nil
}

// extractFrameFromVideo 用 ffprobe 取总帧数后，按 pick（first/middle/last）抽取一帧。
func extractFrameFromVideo(videoAbsPath string, pngAbsPath string, pick string) error {
	totalFrames, _, err := ffprobeVideoFramesAndFPS(videoAbsPath)
	if err != nil {
		return err
	}
	targetFrame := h3TargetFrameIndex(totalFrames, pick)
	filter := fmt.Sprintf("select=eq(n\\,%d)", targetFrame)
	return runFFmpeg("-i", videoAbsPath, "-vf", filter, "-frames:v", "1", pngAbsPath, "-y")
}

// appendH3VideoFrameStaticPrompt 仅在 H3 抽帧模式下，把用户配置的附加提示词追加到提示词末尾。
// 未启用抽帧模式或配置为空时原样返回。
func appendH3VideoFrameStaticPrompt(prompt string) string {
	if !useH3VideoFrameMode() {
		return prompt
	}
	return mergeH3StaticPrompt(prompt, getConfiguredH3VideoFramePrompt())
}

// mergeH3StaticPrompt 把附加提示词拼到提示词末尾；附加词为空或已包含时原样返回。
func mergeH3StaticPrompt(prompt string, extra string) string {
	trimmedExtra := strings.TrimSpace(extra)
	if trimmedExtra == "" {
		return prompt
	}
	base := strings.TrimSpace(prompt)
	if strings.Contains(base, trimmedExtra) {
		return prompt
	}
	if base == "" {
		return trimmedExtra
	}
	return base + "\n" + trimmedExtra
}

// h3TargetFrameIndex 把抽帧位置映射为帧序号（越界时夹到合法范围）。
func h3TargetFrameIndex(totalFrames int, pick string) int {
	targetFrame := 0
	switch normalizeH3FramePick(pick) {
	case H3FramePickFirst:
		targetFrame = 0
	case H3FramePickLast:
		targetFrame = totalFrames - 1
	default:
		targetFrame = totalFrames / 2
	}
	if targetFrame < 0 {
		return 0
	}
	if totalFrames > 0 && targetFrame > totalFrames-1 {
		return totalFrames - 1
	}
	return targetFrame
}

// planH3CharacterRefInjections 解析提示词中机制 A 的 @图N 引用，返回应注入的角色资产前缀
// 与桥接后的提示词（@图N → <Picture N+1>）。chars 须与 buildReferenceCharactersIndexBlock
// 同序（IDL：project_id=? AND ref_image<>'' + id asc + 跳过空名），保证编号严格对齐。
// 注入上限为 h3RefImageSlotLimit-1（8 张：ref_image_0 固定给场景参考图），超出的 @图N
// 无法注入，桥接时保留原标签由调用方记 WARN。未引用任何 @图 或无可注入资产时原样返回。
func planH3CharacterRefInjections(prompt string, chars []models.Character) ([]models.Character, string) {
	matches := h3ReferenceTagPattern.FindAllStringSubmatch(prompt, -1)
	if len(matches) == 0 {
		return nil, prompt
	}
	maxRef := 0
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			continue
		}
		if n > maxRef {
			maxRef = n
		}
	}
	if maxRef == 0 {
		return nil, prompt
	}
	limit := maxRef
	if limit > len(chars) {
		limit = len(chars)
	}
	if limit > h3RefImageSlotLimit-1 {
		limit = h3RefImageSlotLimit - 1
	}
	if limit == 0 {
		return nil, prompt
	}
	bridged := h3ReferenceTagPattern.ReplaceAllStringFunc(prompt, func(tag string) string {
		m := h3ReferenceTagPattern.FindStringSubmatch(tag)
		n, _ := strconv.Atoi(m[1])
		if n >= 1 && n <= limit {
			return fmt.Sprintf("<Picture %d>", n+1)
		}
		return tag
	})
	return chars[:limit], bridged
}

// injectH3Ref2VCharacterRefs 把机制 A 引用的角色参考图注入 ref2v 场景图工作流：
// 每个角色动态创建 LoadImage 节点、上传图片，并接到 MiniMaxH3ReferenceToVideo 的
// ref_images.ref_image_{slot}（scene 参考图固定占 ref_image_0，角色从 1 起）。
// 未引用 @图 或无可注入资产时原样返回提示词。
func injectH3Ref2VCharacterRefs(wfJSON map[string]interface{}, prompt string, chars []models.Character, upload func(string) (string, error)) (string, error) {
	refs, bridged := planH3CharacterRefInjections(prompt, chars)
	if len(refs) == 0 {
		return bridged, nil
	}
	var ref2vNode map[string]interface{}
	for _, node := range wfJSON {
		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		if classType, _ := nodeMap["class_type"].(string); classType == "MiniMaxH3ReferenceToVideo" {
			ref2vNode = nodeMap
			break
		}
	}
	if ref2vNode == nil {
		return "", fmt.Errorf("ref2v workflow missing MiniMaxH3ReferenceToVideo node")
	}
	inputs, ok := ref2vNode["inputs"].(map[string]interface{})
	if !ok {
		inputs = map[string]interface{}{}
		ref2vNode["inputs"] = inputs
	}
	nextID := h3NextFreeNodeID(wfJSON)
	for slot, char := range refs {
		cleanRefPath := strings.TrimPrefix(char.RefImage, "/")
		absRefPath, _ := filepath.Abs(cleanRefPath)
		uploadedName, err := upload(absRefPath)
		if err != nil {
			return "", fmt.Errorf("failed to upload character reference image to comfyui input: %v", err)
		}
		nodeID := strconv.Itoa(nextID + slot)
		wfJSON[nodeID] = map[string]interface{}{
			"class_type": "LoadImage",
			"inputs":     map[string]interface{}{"image": uploadedName},
		}
		inputs[fmt.Sprintf("ref_images.ref_image_%d", slot+1)] = []interface{}{nodeID, 0}
	}
	return bridged, nil
}

// h3NextFreeNodeID 返回工作流中现有最大整数节点编号之后的第一个可用编号。
func h3NextFreeNodeID(wfJSON map[string]interface{}) int {
	maxID := 0
	for id := range wfJSON {
		if n, err := strconv.Atoi(id); err == nil && n > maxID {
			maxID = n
		}
	}
	return maxID + 1
}
