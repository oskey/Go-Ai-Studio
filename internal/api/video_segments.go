package api

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"
	"kt-ai-studio/internal/workflow"

	"github.com/gin-gonic/gin"
)

const defaultSegmentFPS = 24
const fixedSegmentDurationSeconds = 3
const fixedVideoNegativeTemplate = "worst quality, low quality, bad quality, jpeg artifacts, blurry details, cartoon, still image, static frame, bad hands, malformed hands, deformed hands, extra hands, duplicate hands, missing hands, fused hands, merged hands, bad face, malformed limbs, merged limbs, fused arms, extra arms, fused fingers, merged fingers, interlocked fingers, extra fingers, missing fingers, malformed fingers, deformed fingers, broken fingers, twisted fingers, deformed thumbs, malformed thumbs, extra thumbs, missing thumbs, subtitle, subtitles, caption, text, text overlay, on-screen text, lower-third, title card, logo, logos, watermark, watermarks, speech bubble, dialogue box, readable signage, overlay, overlay effects, titles, has blurbox, has subtitles, artifacts around text, unreadable text, incorrect lettering, incorrect slogan"
const minVideoTotalDurationSeconds = 3
const maxVideoTotalDurationSeconds = 15
const maxSingleSceneVideoLengthFrames = defaultSegmentFPS*maxVideoTotalDurationSeconds + 1

type VideoSegmentPlanResponse struct {
	PlayerDesc           string                    `json:"player_desc"`
	RecommendedFPS       int                       `json:"recommended_fps"`
	TotalDurationSeconds int                       `json:"total_duration_seconds"`
	Segments             []VideoSegmentPlanSegment `json:"segments"`
}

type VideoSegmentPlanSegment struct {
	SegmentIndex               int    `json:"segment_index"`
	PromptPos                  string `json:"prompt_pos"`
	PromptNeg                  string `json:"prompt_neg"`
	PlayerDesc                 string `json:"player_desc"`
	RecommendedFPS             int    `json:"recommended_fps"`
	RecommendedDurationSeconds int    `json:"recommended_duration_seconds"`
}

type VideoFingerprintPayload struct {
	RecommendedFPS       int                     `json:"recommended_fps"`
	TotalDurationSeconds int                     `json:"total_duration_seconds"`
	PromptPosZH          string                  `json:"prompt_pos_zh"`
	PromptNegZH          string                  `json:"prompt_neg_zh"`
	PromptPosEN          string                  `json:"prompt_pos_en"`
	PromptNegEN          string                  `json:"prompt_neg_en"`
	StyleZH              string                  `json:"style_zh"`
	StyleEN              string                  `json:"style_en"`
	PlayerDescZH         string                  `json:"player_desc_zh"`
	PlayerDescEN         string                  `json:"player_desc_en"`
	PhasesZH             []VideoFingerprintPhase `json:"phases_zh"`
	PhasesEN             []VideoFingerprintPhase `json:"phases_en"`
}

type VideoFingerprintPhase struct {
	Index     int    `json:"index"`
	TimeRange string `json:"time_range"`
	Content   string `json:"content"`
	Audio     string `json:"audio"`
}

func parseVideoFingerprintPayload(raw string) (*VideoFingerprintPayload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("video_fingerprint is empty")
	}
	var payload VideoFingerprintPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("invalid video_fingerprint JSON: %v", err)
	}
	return &payload, nil
}

func loadPromptLanguage() string {
	return "zh"
}

func selectVideoFingerprintLanguageFields(payload *VideoFingerprintPayload, lang string) (string, string, string, string, []VideoFingerprintPhase) {
	_ = lang
	if payload == nil {
		return "", "", "", "", nil
	}
	promptPos := strings.TrimSpace(payload.PromptPosZH)
	if promptPos == "" {
		promptPos = strings.TrimSpace(payload.PromptPosEN)
	}
	promptNeg := strings.TrimSpace(payload.PromptNegZH)
	if promptNeg == "" {
		promptNeg = strings.TrimSpace(payload.PromptNegEN)
	}
	styleText := strings.TrimSpace(payload.StyleZH)
	if styleText == "" {
		styleText = strings.TrimSpace(payload.StyleEN)
	}
	playerDesc := strings.TrimSpace(payload.PlayerDescZH)
	if playerDesc == "" {
		playerDesc = strings.TrimSpace(payload.PlayerDescEN)
	}
	phases := payload.PhasesZH
	if len(phases) == 0 {
		phases = payload.PhasesEN
	}
	return promptPos, promptNeg, styleText, playerDesc, phases
}

func validateVideoFingerprintAudioText(payload *VideoFingerprintPayload) error {
	if payload == nil {
		return nil
	}

	forbiddenTerms := []string{
		"旁白进入", "旁白继续", "旁白同步", "自然旁白", "中文旁白", "解说继续", "解说进入",
		"内心独白", "心里先过一遍", "旧话回响", "无明确发声", "无发声", "无对白", "旁白",
		"narration", "voiceover", "voice over", "inner monologue", "no clear speech", "no dialogue",
	}

	checkText := func(label string, value string) error {
		text := strings.TrimSpace(strings.ToLower(value))
		for _, term := range forbiddenTerms {
			if strings.Contains(text, strings.ToLower(term)) {
				return fmt.Errorf("%s contains forbidden audio narration term: %s", label, term)
			}
		}
		return nil
	}

	isLikelySpeakerPrefix := func(value string) bool {
		trimmed := strings.TrimSpace(value)
		idx := strings.IndexAny(trimmed, ":：")
		if idx <= 0 || idx > 24 {
			return false
		}
		prefix := strings.TrimSpace(trimmed[:idx])
		if prefix == "" {
			return false
		}
		if strings.Contains(prefix, "中文") || strings.Contains(prefix, "说") || strings.Contains(strings.ToLower(prefix), "audio") {
			return false
		}
		return containsCJKText(prefix) && len([]rune(prefix)) <= 12
	}

	checkSpeechLanguage := func(label string, value string, expectChineseMarker bool) error {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		if isLikelySpeakerPrefix(trimmed) {
			return fmt.Errorf("%s must not use speaker-name prefixes like 角色名:台词; use the fixed Chinese speech sentence instead", label)
		}
		lower := strings.ToLower(trimmed)
		hasSpeechVerb := strings.Contains(trimmed, "说") || strings.Contains(lower, "says") || strings.Contains(lower, "speaks") || strings.Contains(lower, "saying:")
		if !hasSpeechVerb {
			return nil
		}
		if expectChineseMarker {
			if !strings.Contains(trimmed, "中文") {
				return fmt.Errorf("%s must explicitly mark spoken dialogue as Chinese speech", label)
			}
		} else if !strings.Contains(lower, "chinese") {
			return fmt.Errorf("%s must explicitly mark spoken dialogue as Chinese speech", label)
		}
		if !containsCJKText(trimmed) {
			return fmt.Errorf("%s must keep spoken dialogue text in Chinese instead of translating it", label)
		}
		return nil
	}

	for idx, phase := range payload.PhasesZH {
		if strings.TrimSpace(phase.Audio) == "" {
			return fmt.Errorf("phases_zh[%d].audio must not be empty", idx)
		}
		if err := checkText(fmt.Sprintf("phases_zh[%d].audio", idx), phase.Audio); err != nil {
			return err
		}
		if err := checkSpeechLanguage(fmt.Sprintf("phases_zh[%d].audio", idx), phase.Audio, true); err != nil {
			return err
		}
	}
	for idx, phase := range payload.PhasesEN {
		if strings.TrimSpace(phase.Audio) == "" {
			return fmt.Errorf("phases_en[%d].audio must not be empty", idx)
		}
		if err := checkText(fmt.Sprintf("phases_en[%d].audio", idx), phase.Audio); err != nil {
			return err
		}
		if err := checkSpeechLanguage(fmt.Sprintf("phases_en[%d].audio", idx), phase.Audio, false); err != nil {
			return err
		}
	}

	return nil
}

func validateVideoFingerprintDuration(payload *VideoFingerprintPayload) error {
	if payload == nil {
		return nil
	}
	if payload.TotalDurationSeconds < minVideoTotalDurationSeconds || payload.TotalDurationSeconds > maxVideoTotalDurationSeconds {
		return fmt.Errorf("video_fingerprint total_duration_seconds must be between %d and %d seconds", minVideoTotalDurationSeconds, maxVideoTotalDurationSeconds)
	}
	return nil
}

func containsCJKText(input string) bool {
	for _, r := range input {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func validateVideoFingerprintPhaseContent(payload *VideoFingerprintPayload) error {
	if payload == nil {
		return nil
	}

	checkContent := func(label string, value string) error {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil
		}
		lower := strings.ToLower(trimmed)
		forbiddenPrefixes := []string{"style:", "style：", "phase ", "phase:", "phase：", "audio:", "audio：", "风格：", "阶段1", "阶段2", "音频："}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(lower, prefix) {
				return fmt.Errorf("%s must not include label prefixes like Style/Phase/Audio", label)
			}
		}
		return nil
	}

	for idx, phase := range payload.PhasesZH {
		if err := checkContent(fmt.Sprintf("phases_zh[%d].content", idx), phase.Content); err != nil {
			return err
		}
	}
	for idx, phase := range payload.PhasesEN {
		if err := checkContent(fmt.Sprintf("phases_en[%d].content", idx), phase.Content); err != nil {
			return err
		}
	}

	return nil
}

func buildStoredVideoSegmentPlan(video models.Video, workflowFamily string, lang string) (*VideoSegmentPlanResponse, error) {
	if family := strings.TrimSpace(strings.ToLower(workflowFamily)); family != "" && family != "ltx" {
		return nil, fmt.Errorf("only the LTX video workflow is supported in this version")
	}
	fullPrompt := strings.TrimSpace(video.VideoPrompt)
	if fullPrompt == "" {
		return nil, fmt.Errorf("video_prompt is empty")
	}

	promptNeg := ""
	playerDesc := strings.TrimSpace(video.Scene.Description)
	total := clampStoredVideoTotalDuration(video.DurationSeconds)
	recommendedFPS := defaultSegmentFPS

	if payload, err := parseVideoFingerprintPayload(video.VideoFingerprint); err == nil {
		_, promptNeg, _, parsedPlayerDesc, _ := selectVideoFingerprintLanguageFields(payload, lang)
		warnNegativePromptLeadIn(fmt.Sprintf("video=%d video_fingerprint.prompt_neg", video.ID), promptNeg)
		if strings.TrimSpace(parsedPlayerDesc) != "" {
			playerDesc = strings.TrimSpace(parsedPlayerDesc)
		}
		if payload.TotalDurationSeconds > 0 {
			total = clampStoredVideoTotalDuration(payload.TotalDurationSeconds)
		}
		recommendedFPS = sanitizeRecommendedVideoFPS(payload.RecommendedFPS, defaultSegmentFPS)
	}
	if playerDesc == "" {
		playerDesc = fullPrompt
	}

	return &VideoSegmentPlanResponse{
		PlayerDesc:           playerDesc,
		RecommendedFPS:       recommendedFPS,
		TotalDurationSeconds: total,
		Segments: []VideoSegmentPlanSegment{
			{
				SegmentIndex:               1,
				PromptPos:                  fullPrompt,
				PromptNeg:                  promptNeg,
				PlayerDesc:                 playerDesc,
				RecommendedFPS:             recommendedFPS,
				RecommendedDurationSeconds: total,
			},
		},
	}, nil
}

func clampStoredVideoTotalDuration(recommended int) int {
	total := sanitizeRecommendedDurationSeconds(recommended)
	if total == 0 {
		total = minVideoTotalDurationSeconds
	}
	if total < minVideoTotalDurationSeconds {
		return minVideoTotalDurationSeconds
	}
	return total
}

func validateSegmentPlanDuration(video models.Video, plan *VideoSegmentPlanResponse) error {
	_ = video
	_ = plan
	return nil
}

func callLLMVideoSegmentPlan(provider models.LLMProvider, system, user string, taskID string) (*VideoSegmentPlanResponse, error) {
	content, err := requestLLMContent(provider, system, user, taskID, 10*time.Minute, 5, "正在请求 LLM 规划视频分段...", "视频分段规划")
	if err != nil {
		return nil, err
	}

	db.DB.Create(&models.SystemLog{
		Level:     LogLevelInfo,
		Message:   llmLogMessage("LLM 完整返回(视频分段规划)", provider),
		Details:   content,
		CreatedAt: time.Now(),
	})

	jsonContent := cleanupLLMJSON(content)
	var result VideoSegmentPlanResponse
	if err := json.Unmarshal([]byte(jsonContent), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}
	return &result, nil
}

func callLLMVideoSegmentPlanWithRetryReason(provider models.LLMProvider, system, user string, taskID string, retryReason string) (*VideoSegmentPlanResponse, error) {
	if strings.TrimSpace(retryReason) != "" {
		return nil, fmt.Errorf("secondary llm retry planning is disabled in lightweight story mode: %s", strings.TrimSpace(retryReason))
	}
	return callLLMVideoSegmentPlan(provider, system, user, taskID)
}

func planVideoSegmentsWithRetryReason(video models.Video, project models.Project, taskID string, retryReason string) (*VideoSegmentPlanResponse, error) {
	lang := loadPromptLanguage()

	systemPrompt, userPrompt := constructVideoSegmentPlanPrompt(video, project, lang)
	var llmProvider models.LLMProvider
	if err := db.DB.Where("is_active = ?", true).First(&llmProvider).Error; err != nil {
		return nil, fmt.Errorf("no active LLM provider found")
	}
	Log(LogLevelInfo, llmLogMessage("LLM Request", llmProvider), fmt.Sprintf("Starting segment replanning for video: %d", video.ID))
	Log(LogLevelInfo, llmLogMessage("LLM Request Prompt", llmProvider), fmt.Sprintf("System: %s\n\nUser: %s", systemPrompt, userPrompt))

	llmResponse, err := callLLMVideoSegmentPlanWithRetryReason(llmProvider, systemPrompt, userPrompt, taskID, retryReason)
	if err != nil {
		return nil, fmt.Errorf("LLM调用失败: %v", err)
	}
	if err := validateSegmentPlanDuration(video, llmResponse); err != nil {
		return nil, err
	}
	if err := validateSegmentPromptPos(llmResponse); err != nil {
		return nil, err
	}
	for _, warning := range collectSegmentPromptNegWarnings(llmResponse) {
		Log(LogLevelWarn, "视频分段负向提示词告警", warning)
	}
	return sanitizeVideoSegmentPlan(video, llmResponse)
}

func constructVideoSegmentPlanPrompt(video models.Video, project models.Project, lang string) (string, string) {
	_ = project
	_ = lang
	promptLangInstruction := "无论输入是什么语言，你都必须让 prompt_pos、prompt_neg、player_desc 全部使用中文返回。"
	targetLang := "中文"

	systemPrompt := fmt.Sprintf(`
你是一位专业的【图生视频分段规划专家】。
你的任务是根据场景描述、场景图中文提示词和对白，先理解这一镜承担的是建立、关系、行为、反应、信息揭露还是收束等哪一种主要镜头功能，再推理完整镜头需要多长时间，最后把它拆成多个连续的短视频片段。

【核心规则】
1. 当前下游模型是 ComfyUI LTX2.3。每一段都必须固定为 3 秒的短视频提示词。镜头语言要按事件自然速度写：剧情真的慢下来时才写慢推、慢跟或长停顿；普通动作默认写正常速度跟进、顺势推进或短促转向；追拦、扑救、闯入、灾变、动物扑冲等事件允许快跟、急停、被冲击带得一晃后找稳。禁止大幅甩镜、环绕乱飞、没有叙事目的的推拉摇移。
2. 你必须先根据场景图中文提示词、场景描述和对白推理整段视频自然播放的大致总时长，再把它拆成多个连续片段。总时长最低 3 秒，最高 15 秒。
3. 有对白时，必须按真实中文对白内容长度、标点停顿、换气、情绪留白和必要听者反应来估时；只要自然总长仍在 15 秒内，就给足能完整说完并保留少量收尾的时长，不要因为保守而主动再拆碎。若自然总长会超过 15 秒，说明上游应该拆镜，不能靠分段阶段硬兜。
4. 无对白时，按可见动作、情绪停顿、环境动态和镜头任务估时。要更积极利用 LTX2.3 在稳定镜头里承载表情、肢体动作、受控位移、道具互动和环境变化，而不是默认把很多剧情动作压成只有呼吸和眼神。
5. 每个片段只能是 3 秒，绝不能超过 3 秒。
6. 每个片段的动作必须承接上一段最后一帧，不能重新起手，不能像多个独立短视频。
7. prompt_pos 只写模型直接可执行的画面变化：人物动作、表情、视线、重心、手部、上肢、躯干、步伐、道具互动、衣摆发丝、背景动态和镜头运动结果；不要写抽象情绪判断、故事标签，也不要把“建立镜头、关系镜头、主位、次位、背景反应位、视线互锁、轴线稳定、信息核心、回忆感、压迫感、中等强度、微到中微、受控”这类导演学术术语或控制词原样塞进 prompt_pos。
8. 人物动作强度必须服从首帧图和场景描述，但不能默认极小，也不能默认减速。静压镜头可以只写呼吸、视线、嘴角和手部细动；普通走步、转身、伸手、开门、取物等行为默认按正常速度写；对峙、揭露、冲突、阻拦、决裂、离场、追拦、扑救、甩物、闯入、奔逃、动物扑冲等镜头允许按事件自然速度写出明显动作。允许多人做清晰的联动反应，但不要让三四个人同时各做一套互不相关的大动作。
9. 画面中没有被场景图中文提示词、场景描述或对白明确写入的人物，就不允许出现；不要凭空补人，但如果上游已明确存在多人或群体，你必须把重要联动人物与背景人物的合理联动反应也写清，而不是让他们集体僵住。若有一整组持续可见的背景人物，例如侍卫、宫女、官员、士兵、宾客或围观者，要默认延续首帧图中的统一服色、层级和制式，不要在视频里自由换色。
9.1 若背景人物本来就属于席位、班列、守位、门侧、桌边、会场座区、课堂座位、宴席席位、仪仗位置或其它固定站位系统，他们默认应原地维持位置关系，双脚不离位，不换位，不穿越前景，只做原地弱反应；弱反应优先写视线收紧、轻微侧头、肩线收紧、袖摆轻晃、呼吸变化或极轻微躬身。除非剧情明确推动出列、换位、围拢、离席、让路、追赶、奔逃或穿越前景，否则不要把他们写成无缘由走来走去、互换位置、跨过镜头中心，或做出会被模型理解成明显走步的动作。
10. 必须主动从场景图中文提示词和场景描述中提取门帘、灯火、纸张、蒸汽、尘埃、树叶、小物件、火光、水面反光、雨丝、滴水、烟雾、布帘、挂饰等可动元素，并把它们分配到对应片段；背景动态不能缺席。只要场景里存在这些元素，每个片段至少要保留 1 个环境动态。
11. 若当前镜头需要发声或情绪爆点，人物可以伴随动作做受控表演，例如下颌绷紧、嘴唇轻张、肩线前送、手臂抬起、步伐停顿、抽手或转身；禁止明确说话口型、连续台词口型和多人同时抢口型。台词只能出现在 audio 中，不能出现在 prompt_pos。
12. recommended_fps 固定返回 24，并统一按 24 fps 理解动作连续性、口型和时长。
13. 不要输出角色姓名、专有名词称呼、项目内部代号；涉及人物时，必须改写成视觉身份描述。
14. 只要镜头主体是人类或类人角色，就默认保持正常人体结构，不要补出多手、多臂、多腿、额外手指、手腕反折或道具被第三只手抓住的情况。
15. 如果当前镜头已经明确人物朝向，例如背影、侧背、过肩、低头离开或不看正脸，你必须保持这个朝向设定；除非场景明确要求，否则不允许自动补出转身、回头、露正脸。除非剧情明确要求真正走出画面或从画外再入，否则关键人物的动作优先在当前构图内完成，不要把人无缘由带出镜头边界后又重新回到画面。
16. 对 ComfyUI LTX2.3 这类图生视频模型，动作边界如果不写进最终 prompt_pos，模型就会自己猜测并补动作；因此你必须把“谁在动、怎么动、动到什么程度、何时停住、镜头是否跟、环境怎么动”直接写回 prompt_pos，而不是只写一个抽象动作词。
17. 如果当前镜头明显属于建立、关系、行为、反应、信息揭露或收束镜头，prompt_pos 和各片段节奏都必须服从这个镜头功能；但这些镜头功能词只用于你内部推理，不要直接写进最终 prompt_pos。最终 prompt_pos 只保留具体动作、具体站位、具体视线与具体镜头反应。
18. prompt_pos 的表达优先级必须固定为：先写叙事中心人物动作，再写与当前事件直接相关的重要联动反应，最后再写环境动态。环境动态只能增强镜头生命力，不能替代人物表演。
19. 不要把山名、城名、寺名、湖名、桥名、街名、村名以及“X方向”“Y一带”“本应是某地的位置”这类叙事坐标，当成模型会自动理解的视觉锚点。若需要交代远景地标或背景方位，必须改写成可见轮廓、屋脊层级、山体走势、水岸线、桥体结构、树线和空间关系。
20. prompt_pos 中涉及建筑、家具、器物、照明、景观、门窗、墙面、地面和背景人物时，必须继续服从故事时代、地域、文明语境、阶层和场合，不要写出跨时代或跨地域穿帮内容。若是双人关系镜头、离场对峙、阻拦或告别镜头，优先保持关键两人同框，不要默认拆成过碎的单人反打。
21. prompt_pos 必须使用 %s，保持清晰、直接、可执行，不要写成散文或大段抽象说明。
22. prompt_neg 必须使用 %s，且保持简短克制；系统会额外追加一组固定视频负向模板，你不要重复那组模板。负向提示词要直接罗列要排除的对象或错误词，不要写“不要文字水印”“别出现 logo”“禁止字幕”这类带引导词的句式。
23. 返回语言约定：%s

请直接输出合法 JSON。`, targetLang, targetLang, promptLangInstruction)

	userPrompt := fmt.Sprintf(`
场景描述 (Scene Description, 补充空间与静态构图信息):
%s

场景图中文提示词 (Primary Visual Input):
%s

请先判断整段自然时长，再拆成多个连续片段。
每段都必须能独立生成，但同时要严格承接上一段最后一帧。
允许受控镜头语言，但必须克制且服务剧情。
- 先判断这一镜是在交代空间、推进行为、接人物反应、揭露信息还是做收束，再决定动作强度和镜头节奏。
- 你必须优先继承场景基础描述与场景图中文提示词里已经出现的可动背景元素，不要只写人物动作。
- 场景基础描述主要用来理解这一镜的静态画面与叙事重点；场景图中文提示词主要用来理解后续视频应抓住哪些动态锚点、朝向约束、动作起点和环境可动元素。你应区分这两者的职责，不要把它们都写成一段静态复述。
- 如果当前镜头属于多人场面，除叙事中心动作外，还应尽量写出 1 到 2 个与当前事件直接相关的联动反应，不要让其余重要角色像静止摆拍。
- 无论环境多丰富，也不要先写环境再写人物；必须先把叙事中心人物和重要联动反应写清，再补环境动态。
- 如果当前画面里已经有草、树叶、枝条、雨、雨丝、滴水、积水、涟漪、倒影、雾气、烟雾、蒸汽、火苗、布帘、草绳、挂饰、纸角、窗纸、尘土、发丝、衣摆等能持续变化的元素，这些元素必须尽量写回 style 与 phases。
- 如果当前镜头里下雨，就必须明确继续下雨；如果场景里有草，就应写草叶或碎草在风里轻晃；如果有积水，就应写反光或涟漪的轻微变化。不要把这些已存在的动态背景省略掉。
- 如果当前场景里有烛火、灯焰、烟雾、蒸汽、水面、布帘、纱幔、旗帜、灰尘、落叶、挂饰等可动元素，也必须明确继续怎么动，不要把这些元素写成静止背景。
- 场景中的建筑、器物、照明方式、景观和背景服化必须继续符合故事时代、地域、文明语境和场合，不要在古代镜头里补出现代建筑、电器和城市设施，也不要在现代镜头里补出古代建筑和器物。
- 不要把地名、寺名、山名、湖名和方向词原样写回 prompt_pos；如果需要交代远景或地标，必须改写成可见轮廓、层级和空间关系，不要写“某山那边”“某寺方向”“某城位置”。
- 最终 prompt_pos 不要直接写“建立镜头、关系镜头、行为镜头、反应镜头、信息镜头、收束镜头、主位、次位、背景反应位、视线互锁、轴线稳定、信息核心、回忆感、压迫感、中等强度、微到中微、受控”这类抽象术语；这些只能作为你的内部理解，最后必须翻译成具体动作、具体站位、具体视线和具体镜头反应。
- 对 LTX2.3 i2v 而言，不要长段复述输入图里已经静止可见的背景本体；更重要的是把这些背景元素“接下来怎么动”写出来。
- 即使当前镜头是多人群像，phases 里也应围绕 1 个主导事件组织，但可以包含 1 个叙事中心动作、1 到 2 个与其直接相关的重要联动反应，以及若干弱背景活动；不要在同一个 phase 里用分号并列多名清晰人物互不相关的独立大动作。
- 动作和镜头速度要按事件自然速度写，不要默认给“轻微、极轻、缓慢、慢速”这类前缀；只有剧情本身慢下来时才写慢动作。普通行为按正常速度，追拦、扑救、闯入、灾变、动物扑冲按正常偏快或明显快速速度。
- 只要当前镜头已经能够靠稳定构图、明确动作、眼神、停顿和环境动态讲清，就不要为了“更细”而主动把一个成立的镜头写得过碎。`, strings.TrimSpace(video.Scene.Description), strings.TrimSpace(video.Scene.PositivePrompt))

	return systemPrompt, userPrompt
}

func sanitizeVideoSegmentPlan(video models.Video, plan *VideoSegmentPlanResponse) (*VideoSegmentPlanResponse, error) {
	_ = video
	if plan == nil {
		return nil, fmt.Errorf("empty video plan")
	}
	if len(plan.Segments) == 0 {
		return nil, fmt.Errorf("video plan returned no segments")
	}

	globalFPS := sanitizeRecommendedVideoFPS(plan.RecommendedFPS, defaultSegmentFPS)

	sanitized := &VideoSegmentPlanResponse{
		PlayerDesc:     strings.TrimSpace(plan.PlayerDesc),
		RecommendedFPS: globalFPS,
	}

	total := 0
	for idx, segment := range plan.Segments {
		duration := fixedSegmentDurationSeconds
		if duration < fixedSegmentDurationSeconds {
			break
		}

		sanitized.Segments = append(sanitized.Segments, VideoSegmentPlanSegment{
			SegmentIndex:               idx + 1,
			PromptPos:                  strings.TrimSpace(segment.PromptPos),
			PromptNeg:                  strings.TrimSpace(segment.PromptNeg),
			PlayerDesc:                 strings.TrimSpace(segment.PlayerDesc),
			RecommendedFPS:             globalFPS,
			RecommendedDurationSeconds: duration,
		})
		total += duration
	}

	if len(sanitized.Segments) == 0 {
		return nil, fmt.Errorf("video plan produced no usable segments")
	}

	if total < fixedSegmentDurationSeconds {
		total = sanitized.Segments[0].RecommendedDurationSeconds
	}
	if total < minVideoTotalDurationSeconds {
		return nil, fmt.Errorf("default workflow total duration must be at least %d seconds", minVideoTotalDurationSeconds)
	}
	sanitized.TotalDurationSeconds = total
	return sanitized, nil
}

func validateSegmentPromptPos(plan *VideoSegmentPlanResponse) error {
	_ = plan
	return nil
}

func collectSegmentPromptNegWarnings(plan *VideoSegmentPlanResponse) []string {
	if plan == nil {
		return nil
	}
	forbiddenTerms := []string{
		"无人物出现", "无人出现", "无人物", "没有人物", "无对话", "无口型",
		"头像浮窗", "贴纸", "边框", "横条", "字幕", "文字", "对话框",
		"无新增杂物", "动作自然不僵硬",
		"主体数量稳定", "结构稳定", "细节连贯", "整体稳定", "画面纯净",
		"动作自然", "主体完整", "结构正确",
		"无关内容混入", "多余主体混入", "局部变形", "整体失真", "背景抢戏", "动作卡顿",
	}
	warnings := make([]string, 0)
	for _, segment := range plan.Segments {
		text := strings.TrimSpace(segment.PromptNeg)
		if hasNegativePromptLeadIn(text) {
			warnings = append(warnings, fmt.Sprintf("segment %d prompt_neg 使用了带引导词的负向提示词，系统仅告警不重写：%s", segment.SegmentIndex, text))
		}
		for _, term := range forbiddenTerms {
			if strings.Contains(text, term) {
				warnings = append(warnings, fmt.Sprintf("segment %d prompt_neg contains discouraged term: %s", segment.SegmentIndex, term))
			}
		}
	}
	return warnings
}

func summarizeSegmentPrompts(segments []VideoSegmentPlanSegment) (string, string) {
	posParts := make([]string, 0, len(segments))
	negParts := make([]string, 0, len(segments))
	seenNeg := make(map[string]struct{})
	for _, segment := range segments {
		if strings.TrimSpace(segment.PromptPos) != "" {
			posParts = append(posParts, fmt.Sprintf("第%d段：%s", segment.SegmentIndex, strings.TrimSpace(segment.PromptPos)))
		}
		if neg := strings.TrimSpace(buildSegmentNegativePrompt(segment.PromptNeg)); neg != "" {
			if _, exists := seenNeg[neg]; !exists {
				seenNeg[neg] = struct{}{}
				negParts = append(negParts, neg)
			}
		}
	}
	return strings.Join(posParts, "\n\n"), strings.Join(negParts, "\n")
}

func buildSegmentNegativePrompt(input string) string {
	parts := strings.Split(strings.TrimSpace(fixedVideoNegativeTemplate), ",")
	if trimmed := strings.TrimSpace(input); trimmed != "" {
		parts = append(parts, strings.Split(trimmed, ",")...)
	}
	seen := make(map[string]struct{}, len(parts))
	merged := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, item)
	}
	return strings.Join(merged, ", ")
}

func clearVideoSegments(videoID uint) error {
	var segments []models.VideoSegment
	if err := db.DB.Where("video_id = ?", videoID).Find(&segments).Error; err != nil {
		return err
	}
	for _, segment := range segments {
		if err := removeGeneratedVideoAsset(segment.GeneratedVideo); err != nil {
			return err
		}
		if err := removeGeneratedVideoAsset(segment.TransitionFrame); err != nil {
			return err
		}
	}
	return db.DB.Where("video_id = ?", videoID).Delete(&models.VideoSegment{}).Error
}

func saveVideoSegmentPlan(video *models.Video, project models.Project, plan *VideoSegmentPlanResponse) error {
	if video == nil {
		return fmt.Errorf("nil video")
	}
	if err := removeGeneratedVideoAsset(video.GeneratedVideo); err != nil {
		return err
	}
	if err := clearVideoSegments(video.ID); err != nil {
		return err
	}

	_, summaryNeg := summarizeSegmentPrompts(plan.Segments)
	video.PositivePrompt = ""
	video.NegativePrompt = summaryNeg
	video.GeneratedVideo = ""
	video.GeneratedWorkflow = ""
	video.Status = "pending"
	video.UpdatedAt = time.Now()

	if err := db.DB.Save(video).Error; err != nil {
		return err
	}

	start := 0
	for _, segment := range plan.Segments {
		duration := segment.RecommendedDurationSeconds
		fps := sanitizeRecommendedVideoFPS(segment.RecommendedFPS, defaultSegmentFPS)
		record := models.VideoSegment{
			VideoID:         video.ID,
			SegmentIndex:    segment.SegmentIndex,
			StartSecond:     start,
			EndSecond:       start + duration,
			DurationSeconds: duration,
			FPS:             fps,
			Length:          convertRecommendedDurationToFrameCount(duration, fps, 0),
			PositivePrompt:  strings.TrimSpace(segment.PromptPos),
			NegativePrompt:  buildSegmentNegativePrompt(segment.PromptNeg),
			PlayerDesc:      strings.TrimSpace(segment.PlayerDesc),
			Status:          "pending",
			GeneratedVideo:  "",
			TransitionFrame: "",
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		if err := db.DB.Create(&record).Error; err != nil {
			return err
		}
		start += duration
	}

	return nil
}

func planVideoSegments(video models.Video, project models.Project, taskID string) (*VideoSegmentPlanResponse, error) {
	lang := loadPromptLanguage()

	systemPrompt, userPrompt := constructVideoSegmentPlanPrompt(video, project, lang)
	var llmProvider models.LLMProvider
	if err := db.DB.Where("is_active = ?", true).First(&llmProvider).Error; err != nil {
		return nil, fmt.Errorf("no active LLM provider found")
	}
	Log(LogLevelInfo, llmLogMessage("LLM Request", llmProvider), fmt.Sprintf("Starting segment planning for video: %d", video.ID))
	Log(LogLevelInfo, llmLogMessage("LLM Request Prompt", llmProvider), fmt.Sprintf("System: %s\n\nUser: %s", systemPrompt, userPrompt))

	llmResponse, err := callLLMVideoSegmentPlan(llmProvider, systemPrompt, userPrompt, taskID)
	if err != nil {
		return nil, fmt.Errorf("LLM调用失败: %v", err)
	}
	if err := validateSegmentPlanDuration(video, llmResponse); err != nil {
		return nil, err
	}
	if err := validateSegmentPromptPos(llmResponse); err != nil {
		return nil, err
	}
	for _, warning := range collectSegmentPromptNegWarnings(llmResponse) {
		Log(LogLevelWarn, llmLogMessage("LLM 视频分段负向提示词告警", llmProvider), warning)
	}
	plan, err := sanitizeVideoSegmentPlan(video, llmResponse)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func generateVideoSegmentPlanInternal(payloadJSON string, taskID string) error {
	_ = taskID
	var payload struct {
		VideoID   uint `json:"video_id"`
		ProjectID uint `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return fmt.Errorf("invalid payload: %v", err)
	}

	var video models.Video
	if err := db.DB.First(&video, payload.VideoID).Error; err != nil {
		return fmt.Errorf("video not found")
	}
	if err := hydrateVideoScene(&video, true); err != nil {
		return err
	}
	var project models.Project
	if err := db.DB.Preload("ArtStyle").First(&project, payload.ProjectID).Error; err != nil {
		return fmt.Errorf("project not found")
	}
	workflowFamily, err := resolveSelectedVideoWorkflowFamily()
	if err != nil {
		return err
	}
	lang := loadPromptLanguage()
	plan, err := buildStoredVideoSegmentPlan(video, workflowFamily, lang)
	if err != nil {
		return fmt.Errorf("video_fingerprint is invalid and secondary llm regeneration is disabled: %w", err)
	}
	if err := saveVideoSegmentPlan(&video, project, plan); err != nil {
		return fmt.Errorf("failed to save video segment plan: %v", err)
	}

	BroadcastUpdate("video", video.ID)
	return nil
}

func resolveFFmpegBinary() (string, error) {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyFFmpegPath).First(&setting).Error; err == nil {
		path := strings.TrimSpace(strings.Trim(setting.Value, `"`))
		if path != "" {
			return path, nil
		}
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not found; please configure ffmpeg_path in settings")
	}
	return ffmpegPath, nil
}

func runFFmpeg(args ...string) error {
	ffmpegPath, err := resolveFFmpegBinary()
	if err != nil {
		return err
	}
	cmd := exec.Command(ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %v, output: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func assetWebPathToAbs(assetPath string) (string, error) {
	cleanPath := strings.TrimSpace(strings.TrimPrefix(assetPath, "/"))
	if cleanPath == "" {
		return "", fmt.Errorf("empty asset path")
	}
	return filepath.Abs(cleanPath)
}

func waitForVideoOutputFile(promptID string, projectCode string, savePrefix string) (string, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			history, err := GetComfyHistory(promptID)
			if err == nil {
				if outputs, ok := history["outputs"].(map[string]interface{}); ok {
					for _, nodeOutput := range outputs {
						var fileData map[string]interface{}
						if nodeMap, ok := nodeOutput.(map[string]interface{}); ok {
							if gifs, ok := nodeMap["gifs"].([]interface{}); ok && len(gifs) > 0 {
								fileData = gifs[0].(map[string]interface{})
							} else if images, ok := nodeMap["images"].([]interface{}); ok && len(images) > 0 {
								fileData = images[0].(map[string]interface{})
							}
						}
						if fileData == nil {
							continue
						}

						filename := fileData["filename"].(string)
						subfolder := fileData["subfolder"].(string)
						typeStr := fileData["type"].(string)

						saveDir := filepath.Join("output", projectCode, "videos")
						if err := os.MkdirAll(saveDir, 0755); err != nil {
							return "", err
						}
						ext := filepath.Ext(filename)
						if ext == "" {
							ext = ".mp4"
						}
						savePath := filepath.Join(saveDir, fmt.Sprintf("%s_%d%s", savePrefix, time.Now().Unix(), ext))
						if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
							return "", err
						}
						return "/" + filepath.ToSlash(savePath), nil
					}
				}
				continue
			}
		}
	}
}

func queueLTXVideoRender(videoID uint, projectID uint) error {
	var video models.Video
	if err := db.DB.Preload("Segments").First(&video, videoID).Error; err != nil {
		return fmt.Errorf("video not found")
	}
	if err := ensureVideoSceneLoaded(&video, true); err != nil {
		return err
	}
	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		return fmt.Errorf("project not found")
	}
	if strings.TrimSpace(video.Scene.GeneratedImage) == "" {
		return fmt.Errorf("scene has no generated image")
	}

	var segmentCount int64
	if err := db.DB.Model(&models.VideoSegment{}).Where("video_id = ?", video.ID).Count(&segmentCount).Error; err != nil {
		return err
	}
	if segmentCount == 0 {
		lang := loadPromptLanguage()
		plan, err := buildStoredVideoSegmentPlan(video, "ltx", lang)
		if err != nil {
			return err
		}
		if err := saveVideoSegmentPlan(&video, project, plan); err != nil {
			return err
		}
	}

	if err := db.DB.Preload("Segments").First(&video, video.ID).Error; err != nil {
		return err
	}
	if err := ensureVideoSceneLoaded(&video, true); err != nil {
		return err
	}
	if len(video.Segments) == 0 {
		return fmt.Errorf("video has no planned segments")
	}

	sort.Slice(video.Segments, func(i, j int) bool {
		return video.Segments[i].SegmentIndex < video.Segments[j].SegmentIndex
	})
	segment := video.Segments[0]

	if err := removeGeneratedVideoAsset(video.GeneratedVideo); err != nil {
		return err
	}
	if err := removeGeneratedVideoAsset(segment.GeneratedVideo); err != nil {
		return err
	}
	if err := removeGeneratedVideoAsset(segment.TransitionFrame); err != nil {
		return err
	}

	video.GeneratedVideo = ""
	video.GeneratedWorkflow = ""
	video.Status = "generating"
	video.UpdatedAt = time.Now()
	if err := db.DB.Save(&video).Error; err != nil {
		return err
	}

	segment.Status = "generating"
	segment.GeneratedVideo = ""
	segment.TransitionFrame = ""
	segment.UpdatedAt = time.Now()
	if err := db.DB.Save(&segment).Error; err != nil {
		return err
	}
	BroadcastUpdate("video", video.ID)

	positivePrompt := strings.TrimSpace(segment.PositivePrompt)

	seed := getConfiguredGlobalSeed()
	promptID, err := triggerVideoGenerationWithInput(
		video,
		video.Scene.GeneratedImage,
		positivePrompt,
		segment.NegativePrompt,
		sanitizeRecommendedVideoFPS(segment.FPS, defaultSegmentFPS),
		convertRecommendedDurationToFrameCount(segment.DurationSeconds, sanitizeRecommendedVideoFPS(segment.FPS, defaultSegmentFPS), segment.Length),
		seed,
		fmt.Sprintf("video_%d_segment_%02d", video.ID, segment.SegmentIndex),
	)
	if err != nil {
		wrappedErr := fmt.Errorf("segment %d failed to queue ComfyUI prompt: %w", segment.SegmentIndex, err)
		logVideoRenderFailure(video, segment.SegmentIndex, "queue_prompt", wrappedErr, "")
		segment.Status = "failed"
		_ = db.DB.Save(&segment).Error
		video.Status = "failed"
		_ = db.DB.Save(&video).Error
		BroadcastUpdate("video", video.ID)
		return wrappedErr
	}

	db.DB.Create(&models.SystemLog{
		Level:     LogLevelInfo,
		Message:   "LTX 视频已提交到 ComfyUI 队列",
		Details:   fmt.Sprintf("VideoID: %d\nSceneID: %d\nSegmentIndex: %d\nPromptID: %s", video.ID, video.SceneID, segment.SegmentIndex, promptID),
		CreatedAt: time.Now(),
	})

	go func(video models.Video, segment models.VideoSegment, projectCode string, promptID string) {
		webPath, err := waitForVideoOutputFile(promptID, projectCode, fmt.Sprintf("video_%d_segment_%02d", video.ID, segment.SegmentIndex))
		if err != nil {
			wrappedErr := fmt.Errorf("segment %d failed while waiting for ComfyUI output: %w", segment.SegmentIndex, err)
			logVideoRenderFailure(video, segment.SegmentIndex, "wait_output", wrappedErr, promptID)
			segment.Status = "failed"
			segment.UpdatedAt = time.Now()
			_ = db.DB.Save(&segment).Error
			video.Status = "failed"
			video.UpdatedAt = time.Now()
			_ = db.DB.Save(&video).Error
			BroadcastUpdate("video", video.ID)
			return
		}

		segment.GeneratedVideo = webPath
		segment.Status = "generated"
		segment.UpdatedAt = time.Now()
		_ = db.DB.Save(&segment).Error

		video.GeneratedVideo = webPath
		video.Status = "generated"
		video.UpdatedAt = time.Now()
		_ = db.DB.Save(&video).Error
		BroadcastUpdate("video", video.ID)
	}(video, segment, project.Code, promptID)

	return nil
}

func extractVideoTransitionFrame(segment models.VideoSegment) (string, error) {
	inputPath, err := assetWebPathToAbs(segment.GeneratedVideo)
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(inputPath)
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	outputPath := filepath.Join(dir, base+"_last.png")
	fps := sanitizeRecommendedVideoFPS(segment.FPS, defaultSegmentFPS)
	offsetSeconds := (3.0 / float64(fps)) + 0.01
	if err := runFFmpeg("-y", "-v", "error", "-sseof", fmt.Sprintf("-%.4f", offsetSeconds), "-i", inputPath, "-frames:v", "1", outputPath); err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(outputPath), nil
}

func mergeVideoSegments(projectCode string, videoID uint, segments []models.VideoSegment) (string, error) {
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].SegmentIndex < segments[j].SegmentIndex
	})

	saveDir := filepath.Join("output", projectCode, "videos")
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return "", err
	}

	listPath := filepath.Join(saveDir, fmt.Sprintf("video_%d_concat.txt", videoID))
	lines := make([]string, 0, len(segments))
	for _, segment := range segments {
		if strings.TrimSpace(segment.GeneratedVideo) == "" {
			return "", fmt.Errorf("segment %d has no generated video", segment.SegmentIndex)
		}
		absPath, err := assetWebPathToAbs(segment.GeneratedVideo)
		if err != nil {
			return "", err
		}
		lines = append(lines, fmt.Sprintf("file '%s'", filepath.ToSlash(absPath)))
	}
	if err := os.WriteFile(listPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return "", err
	}
	defer os.Remove(listPath)

	outputPath := filepath.Join(saveDir, fmt.Sprintf("video_%d_merged_%d.mp4", videoID, time.Now().Unix()))
	if err := runFFmpeg("-y", "-v", "error", "-f", "concat", "-safe", "0", "-i", listPath, "-c:v", "libx264", "-c:a", "aac", "-pix_fmt", "yuv420p", "-movflags", "+faststart", outputPath); err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(outputPath), nil
}

func triggerVideoGenerationWithInput(video models.Video, inputImagePath string, positive string, negative string, fps int, length int, seed int64, saveLabel string) (string, error) {
	var project models.Project
	if err := db.DB.First(&project, video.ProjectID).Error; err != nil {
		return "", fmt.Errorf("project not found")
	}

	files, _ := filepath.Glob(filepath.Join("workflows", "*.json"))
	var targetFile string
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyDefaultVideoModel).First(&setting).Error; err != nil {
		return "", fmt.Errorf("default video model not set")
	}
	workflowName := strings.TrimSpace(setting.Value)
	if workflowName == "" {
		return "", fmt.Errorf("default video model is empty")
	}
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
		return "", fmt.Errorf("failed to unmarshal workflow: %v", err)
	}

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
	hasLinkedInput := func(nodeID string, key string) bool {
		if nodeID == "" {
			return false
		}
		node, ok := wfJSON[nodeID].(map[string]interface{})
		if !ok {
			return false
		}
		inputs, ok := node["inputs"].(map[string]interface{})
		if !ok {
			return false
		}
		value, ok := inputs[key]
		if !ok {
			return false
		}
		link, ok := value.([]interface{})
		return ok && len(link) >= 1
	}

	negative = buildSegmentNegativePrompt(negative)
	warnNegativePromptLeadIn(fmt.Sprintf("video=%d segment submit negative prompt", video.ID), negative)
	setInput(meta.PositiveNodeID, meta.PositiveInputKey, positive)
	setInput(meta.NegativeNodeID, meta.NegativeInputKey, negative)

	width, height := getConfiguredVideoSize()
	setInput(meta.SeedNodeID, meta.SeedInputKey, seed)
	// Preserve workflow graph links such as Width -> Math -> Latent instead of
	// overwriting connected inputs with raw integers.
	if !hasLinkedInput(meta.WidthNodeID, meta.WidthInputKey) {
		setInput(meta.WidthNodeID, meta.WidthInputKey, width)
	}
	if !hasLinkedInput(meta.HeightNodeID, meta.HeightInputKey) {
		setInput(meta.HeightNodeID, meta.HeightInputKey, height)
	}
	if fps > 0 {
		setInput(meta.FPSNodeID, meta.FPSInputKey, fps)
	}
	if length > 0 {
		setInput(meta.LengthNodeID, meta.LengthInputKey, length)
	}
	for _, node := range wfJSON {
		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		classType, _ := nodeMap["class_type"].(string)
		metaInfo, _ := nodeMap["_meta"].(map[string]interface{})
		title, _ := metaInfo["title"].(string)
		inputs, _ := nodeMap["inputs"].(map[string]interface{})
		if inputs == nil {
			continue
		}

		switch classType {
		case "PrimitiveStringMultiline", "PrimitiveString":
			if title == "Prompt" {
				inputs["value"] = positive
			}
		case "PreviewAny":
			if title == "预览任意" {
				inputs["preview_text"] = positive
				inputs["preview_markdown"] = positive
			}
		case "PrimitiveInt":
			switch title {
			case "Width":
				inputs["value"] = width
			case "Height":
				inputs["value"] = height
			case "Frame Rate":
				if fps > 0 {
					inputs["value"] = fps
				}
			case "Length":
				if length > 0 {
					inputs["value"] = length
				}
			}
		}
	}

	var imageNodeID string
	for id, node := range wfJSON {
		if nodeMap, ok := node.(map[string]interface{}); ok {
			if classType, ok := nodeMap["class_type"].(string); ok && classType == "LoadImage" {
				imageNodeID = id
				break
			}
		}
	}

	absImagePath, err := assetWebPathToAbs(inputImagePath)
	if err != nil {
		return "", err
	}
	uploadedName, err := UploadToComfyUIInput(absImagePath)
	if err != nil {
		if imageNodeID != "" {
			setInput(imageNodeID, "image", absImagePath)
		}
	} else if imageNodeID != "" {
		setInput(imageNodeID, "image", uploadedName)
	}

	logComfyWorkflowPayload("Video ComfyUI Workflow Payload", workflowLabel, wfJSON)

	promptID, err := QueueComfyPrompt(wfJSON)
	if err != nil {
		return "", err
	}
	if workflowLabel != "" {
		if saveErr := db.DB.Model(&models.Video{}).
			Where("id = ?", video.ID).
			Update("video_generated_workflow", workflowLabel).Error; saveErr != nil {
			Log(LogLevelWarn, "Video Workflow Save Failed", fmt.Sprintf("video=%d workflow=%s err=%v", video.ID, workflowLabel, saveErr))
		}
	}
	return promptID, nil
}

func logVideoRenderFailure(video models.Video, segmentIndex int, stage string, err error, promptID string) {
	if err == nil {
		return
	}

	details := []string{
		fmt.Sprintf("VideoID: %d", video.ID),
		fmt.Sprintf("SceneID: %d", video.SceneID),
		fmt.Sprintf("SegmentIndex: %d", segmentIndex),
		fmt.Sprintf("Stage: %s", strings.TrimSpace(stage)),
	}
	if strings.TrimSpace(promptID) != "" {
		details = append(details, fmt.Sprintf("PromptID: %s", strings.TrimSpace(promptID)))
	}
	details = append(details, fmt.Sprintf("Error: %v", err))

	Log(LogLevelError, "视频分段生成失败", strings.Join(details, "\n"))
	db.DB.Create(&models.SystemLog{
		Level:     LogLevelError,
		Message:   "视频分段生成失败",
		Details:   strings.Join(details, "\n"),
		CreatedAt: time.Now(),
	})
}

func renderVideoSegments(videoID uint, projectID uint, taskID string, startSegmentIndex int, transientPlan *VideoSegmentPlanResponse) error {
	var video models.Video
	if err := db.DB.Preload("Segments").First(&video, videoID).Error; err != nil {
		return fmt.Errorf("video not found")
	}
	if err := hydrateVideoScene(&video, true); err != nil {
		return err
	}
	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		return fmt.Errorf("project not found")
	}
	if video.Scene.GeneratedImage == "" {
		return fmt.Errorf("scene has no generated image")
	}
	if len(video.Segments) == 0 {
		return fmt.Errorf("video has no planned segments")
	}
	workflowFamily, err := resolveSelectedVideoWorkflowFamily()
	if err != nil {
		return err
	}
	lang := loadPromptLanguage()
	if transientPlan == nil {
		transientPlan, _ = buildStoredVideoSegmentPlan(video, workflowFamily, lang)
	}

	sort.Slice(video.Segments, func(i, j int) bool {
		return video.Segments[i].SegmentIndex < video.Segments[j].SegmentIndex
	})

	if startSegmentIndex < 1 {
		startSegmentIndex = 1
	}
	if startSegmentIndex > len(video.Segments) {
		return fmt.Errorf("invalid start segment")
	}

	if err := removeGeneratedVideoAsset(video.GeneratedVideo); err != nil {
		return err
	}
	video.GeneratedVideo = ""
	video.GeneratedWorkflow = ""
	video.Status = "generating"
	video.UpdatedAt = time.Now()
	if err := db.DB.Save(&video).Error; err != nil {
		return err
	}
	BroadcastUpdate("video", video.ID)

	inputImagePath := video.Scene.GeneratedImage
	if startSegmentIndex > 1 {
		prevSegment := video.Segments[startSegmentIndex-2]
		if strings.TrimSpace(prevSegment.TransitionFrame) == "" {
			return fmt.Errorf("previous segment transition frame is missing")
		}
		inputImagePath = prevSegment.TransitionFrame
	}

	for i := startSegmentIndex - 1; i < len(video.Segments); i++ {
		segment := video.Segments[i]
		if err := removeGeneratedVideoAsset(segment.GeneratedVideo); err != nil {
			return err
		}
		if err := removeGeneratedVideoAsset(segment.TransitionFrame); err != nil {
			return err
		}

		segment.Status = "generating"
		segment.GeneratedVideo = ""
		segment.TransitionFrame = ""
		segment.UpdatedAt = time.Now()
		if err := db.DB.Save(&segment).Error; err != nil {
			return err
		}
		BroadcastUpdate("video", video.ID)

		if taskID != "" {
			progress := int(float64(i-(startSegmentIndex-1)) / float64(len(video.Segments)-(startSegmentIndex-1)) * 100)
			task.GlobalTaskManager.UpdateTaskProgress(taskID, progress, fmt.Sprintf("正在生成第 %d 段视频", segment.SegmentIndex))
		}

		seed := getConfiguredGlobalSeed()
		positivePrompt := strings.TrimSpace(segment.PositivePrompt)

		promptID, err := triggerVideoGenerationWithInput(
			video,
			inputImagePath,
			positivePrompt,
			segment.NegativePrompt,
			sanitizeRecommendedVideoFPS(segment.FPS, defaultSegmentFPS),
			convertRecommendedDurationToFrameCount(segment.DurationSeconds, sanitizeRecommendedVideoFPS(segment.FPS, defaultSegmentFPS), segment.Length),
			seed,
			fmt.Sprintf("video_%d_segment_%02d", video.ID, segment.SegmentIndex),
		)
		if err != nil {
			wrappedErr := fmt.Errorf("segment %d failed to queue ComfyUI prompt: %w", segment.SegmentIndex, err)
			logVideoRenderFailure(video, segment.SegmentIndex, "queue_prompt", wrappedErr, "")
			segment.Status = "failed"
			db.DB.Save(&segment)
			video.Status = "failed"
			db.DB.Save(&video)
			BroadcastUpdate("video", video.ID)
			return wrappedErr
		}

		webPath, err := waitForVideoOutputFile(promptID, project.Code, fmt.Sprintf("video_%d_segment_%02d", video.ID, segment.SegmentIndex))
		if err != nil {
			wrappedErr := fmt.Errorf("segment %d failed while waiting for ComfyUI output: %w", segment.SegmentIndex, err)
			logVideoRenderFailure(video, segment.SegmentIndex, "wait_output", wrappedErr, promptID)
			segment.Status = "failed"
			db.DB.Save(&segment)
			video.Status = "failed"
			db.DB.Save(&video)
			BroadcastUpdate("video", video.ID)
			return wrappedErr
		}

		segment.GeneratedVideo = webPath
		segment.Status = "generated"
		segment.UpdatedAt = time.Now()
		framePath, err := extractVideoTransitionFrame(segment)
		if err != nil {
			wrappedErr := fmt.Errorf("segment %d failed while extracting transition frame: %w", segment.SegmentIndex, err)
			logVideoRenderFailure(video, segment.SegmentIndex, "extract_transition_frame", wrappedErr, promptID)
			segment.Status = "failed"
			db.DB.Save(&segment)
			video.Status = "failed"
			db.DB.Save(&video)
			BroadcastUpdate("video", video.ID)
			return wrappedErr
		}
		segment.TransitionFrame = framePath
		if err := db.DB.Save(&segment).Error; err != nil {
			return err
		}
		BroadcastUpdate("video", video.ID)
		inputImagePath = framePath
	}

	var refreshedSegments []models.VideoSegment
	if err := db.DB.Where("video_id = ?", video.ID).Order("segment_index asc").Find(&refreshedSegments).Error; err != nil {
		return err
	}
	mergedVideoPath, err := mergeVideoSegments(project.Code, video.ID, refreshedSegments)
	if err != nil {
		wrappedErr := fmt.Errorf("failed to merge rendered segments for video %d: %w", video.ID, err)
		logVideoRenderFailure(video, 0, "merge_segments", wrappedErr, "")
		video.Status = "failed"
		db.DB.Save(&video)
		BroadcastUpdate("video", video.ID)
		return wrappedErr
	}

	video.GeneratedVideo = mergedVideoPath
	video.Status = "generated"
	video.UpdatedAt = time.Now()
	if err := db.DB.Save(&video).Error; err != nil {
		return err
	}
	BroadcastUpdate("video", video.ID)
	return nil
}

func HandleRenderVideoSegmentsTask(t *models.Task) (interface{}, error) {
	var payload struct {
		VideoID   uint `json:"video_id"`
		ProjectID uint `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	var segmentCount int64
	db.DB.Model(&models.VideoSegment{}).Where("video_id = ?", payload.VideoID).Count(&segmentCount)

	var transientPlan *VideoSegmentPlanResponse
	if segmentCount == 0 {
		var video models.Video
		if err := db.DB.First(&video, payload.VideoID).Error; err == nil {
			if err := hydrateVideoScene(&video, true); err == nil {
				var project models.Project
				if err := db.DB.Preload("ArtStyle").First(&project, payload.ProjectID).Error; err == nil {
					workflowFamily, familyErr := resolveSelectedVideoWorkflowFamily()
					if familyErr != nil {
						return nil, familyErr
					}
					lang := loadPromptLanguage()
					plan, planErr := buildStoredVideoSegmentPlan(video, workflowFamily, lang)
					if planErr != nil {
						return nil, planErr
					}
					transientPlan = plan
					if err := saveVideoSegmentPlan(&video, project, plan); err != nil {
						return nil, err
					}
					BroadcastUpdate("video", video.ID)
				}
			}
		}
	}

	if err := renderVideoSegments(payload.VideoID, payload.ProjectID, t.ID, 1, transientPlan); err != nil {
		return nil, err
	}
	return "Video segments rendered successfully", nil
}

func HandleRenderVideoFromSegmentTask(t *models.Task) (interface{}, error) {
	var payload struct {
		VideoID      uint `json:"video_id"`
		ProjectID    uint `json:"project_id"`
		StartSegment int  `json:"start_segment"`
	}
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	if payload.StartSegment <= 0 {
		return nil, fmt.Errorf("invalid start segment")
	}

	if err := renderVideoSegments(payload.VideoID, payload.ProjectID, t.ID, payload.StartSegment, nil); err != nil {
		return nil, err
	}
	return "Video segments regenerated successfully", nil
}

func RegenerateVideoSegment(c *gin.Context) {
	videoID := c.Param("videoId")
	projectID := c.Param("id")
	segmentID := c.Param("segmentId")
	videoIDValue := parseUint(videoID)

	var segment models.VideoSegment
	if err := db.DB.First(&segment, segmentID).Error; err != nil {
		c.JSON(404, gin.H{"error": "Video segment not found"})
		return
	}
	if segment.VideoID != videoIDValue {
		c.JSON(400, gin.H{"error": "Video segment does not belong to this video"})
		return
	}

	startSegment := segment.SegmentIndex
	payload := map[string]interface{}{
		"video_id":      videoIDValue,
		"project_id":    parseUint(projectID),
		"start_segment": startSegment,
	}
	t, err := task.GlobalTaskManager.AddTask("render_video_from_segment", payload)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to submit segment regeneration task"})
		return
	}
	c.JSON(202, gin.H{"message": "Segment regeneration task submitted", "task_id": t.ID})
}

func parseUint(value string) uint {
	parsed, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	return uint(parsed)
}
