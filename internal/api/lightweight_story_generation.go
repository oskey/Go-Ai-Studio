package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"

	"github.com/sashabaranov/go-openai"
	"gorm.io/gorm"
)

type lightweightStoryCharacter struct {
	Name       string `json:"name"`
	Gender     string `json:"gender"`
	Age        string `json:"age"`
	Height     string `json:"height"`
	Era        string `json:"era"`
	Country    string `json:"country"`
	Appearance string `json:"appearance"`
}

func (c *lightweightStoryCharacter) UnmarshalJSON(data []byte) error {
	type rawCharacter struct {
		Name       json.RawMessage `json:"name"`
		Gender     json.RawMessage `json:"gender"`
		Age        json.RawMessage `json:"age"`
		Height     json.RawMessage `json:"height"`
		Era        json.RawMessage `json:"era"`
		Country    json.RawMessage `json:"country"`
		Appearance json.RawMessage `json:"appearance"`
	}
	var raw rawCharacter
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	var err error
	if c.Name, err = coerceJSONScalarToString(raw.Name); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	if c.Gender, err = coerceJSONScalarToString(raw.Gender); err != nil {
		return fmt.Errorf("gender: %w", err)
	}
	if c.Age, err = coerceJSONScalarToString(raw.Age); err != nil {
		return fmt.Errorf("age: %w", err)
	}
	if c.Height, err = coerceJSONScalarToString(raw.Height); err != nil {
		return fmt.Errorf("height: %w", err)
	}
	if c.Era, err = coerceJSONScalarToString(raw.Era); err != nil {
		return fmt.Errorf("era: %w", err)
	}
	if c.Country, err = coerceJSONScalarToString(raw.Country); err != nil {
		return fmt.Errorf("country: %w", err)
	}
	if c.Appearance, err = coerceJSONScalarToString(raw.Appearance); err != nil {
		return fmt.Errorf("appearance: %w", err)
	}
	return nil
}

type lightweightStoryScene struct {
	SceneID         int    `json:"scene_id"`
	DurationSeconds int    `json:"duration_seconds"`
	Narration       string `json:"narration"`
	ImagePrompt     string `json:"image_prompt"`
	VideoPrompt     string `json:"video_prompt"`
}

type lightweightStoryEpisodeCharacterStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type lightweightStoryEpisodeMemory struct {
	StorySummary    string                                   `json:"story_summary"`
	EndingState     string                                   `json:"ending_state"`
	CharacterStatus []lightweightStoryEpisodeCharacterStatus `json:"character_status"`
	OpenThreads     []string                                 `json:"open_threads"`
}

type lightweightStoryResponse struct {
	TotalScenes   int                           `json:"total_scenes"`
	Characters    []lightweightStoryCharacter   `json:"characters"`
	Scenes        []lightweightStoryScene       `json:"scenes"`
	EpisodeMemory lightweightStoryEpisodeMemory `json:"episode_memory"`
}

type lightweightStoryTaskPayload struct {
	ProjectID          uint                       `json:"project_id"`
	Request            models.AutoGenerateRequest `json:"request"`
	ContinueFromTaskID string                     `json:"continue_from_task_id,omitempty"`
}

type lightweightStoryPromptMetrics struct {
	PlotRuneCount    int
	DialogueMarkers  int
	StoryDensityText string
}

type lightweightStoryPromptContext struct {
	Project                    models.Project
	Request                    models.AutoGenerateRequest
	SelectedTagRules           string
	ExistingCharactersJSON     string
	PreviousEpisodeContextJSON string
	Metrics                    lightweightStoryPromptMetrics
	SceneImageWidth            int
	SceneImageHeight           int
	SceneImageFrameType        string
	FixedVideoFPS              int
}

func emptyEpisodeMemory() lightweightStoryEpisodeMemory {
	return lightweightStoryEpisodeMemory{
		CharacterStatus: []lightweightStoryEpisodeCharacterStatus{},
		OpenThreads:     []string{},
	}
}

func buildLightweightStoryPromptMetrics(plot string) lightweightStoryPromptMetrics {
	trimmed := strings.TrimSpace(plot)
	runeCount := len([]rune(trimmed))
	dialogueMarkers := strings.Count(trimmed, "“") + strings.Count(trimmed, "”") + strings.Count(trimmed, "\"")

	storyDensity := "中等"
	switch {
	case runeCount > 1800 || dialogueMarkers > 30:
		storyDensity = "很高"
	case runeCount > 1100 || dialogueMarkers > 18:
		storyDensity = "较高"
	case runeCount > 500 || dialogueMarkers > 8:
		storyDensity = "偏高"
	}

	return lightweightStoryPromptMetrics{
		PlotRuneCount:    runeCount,
		DialogueMarkers:  dialogueMarkers,
		StoryDensityText: storyDensity,
	}
}

func describeFrameType(width int, height int) string {
	switch {
	case height > width:
		return "竖向窄画幅"
	case height == width:
		return "方形画幅"
	default:
		return "横向宽画幅"
	}
}

func buildSelectedAutoGenerateTagRulesBlock(tags []models.AutoGenerateTag) string {
	if len(tags) == 0 {
		return ""
	}
	sections := make([]string, 0, len(tags))
	for _, tag := range tags {
		name := strings.TrimSpace(tag.Name)
		rules := strings.TrimSpace(tag.Rules)
		if name == "" || rules == "" {
			continue
		}
		sections = append(sections, fmt.Sprintf("【%s补充规则】\n%s", name, rules))
	}
	if len(sections) == 0 {
		return ""
	}
	return "【补充规则】\n" + strings.Join(sections, "\n\n")
}

func buildLightweightStoryPromptContext(project models.Project, req models.AutoGenerateRequest, existingCharacters []lightweightStoryCharacter, previousEpisodeContext lightweightStoryEpisodeMemory) (lightweightStoryPromptContext, error) {
	if existingCharacters == nil {
		existingCharacters = []lightweightStoryCharacter{}
	}
	if previousEpisodeContext.CharacterStatus == nil {
		previousEpisodeContext.CharacterStatus = []lightweightStoryEpisodeCharacterStatus{}
	}
	if previousEpisodeContext.OpenThreads == nil {
		previousEpisodeContext.OpenThreads = []string{}
	}

	selectedTags, err := listSelectedAutoGenerateTags(req.TagIDs)
	if err != nil {
		return lightweightStoryPromptContext{}, err
	}

	sceneImageWidth, sceneImageHeight := getConfiguredSceneImageSize()

	existingJSONBytes, err := json.MarshalIndent(map[string]interface{}{
		"existing_characters": existingCharacters,
	}, "", "  ")
	if err != nil {
		return lightweightStoryPromptContext{}, err
	}

	previousContextJSONBytes, err := json.MarshalIndent(map[string]interface{}{
		"previous_episode_context": previousEpisodeContext,
	}, "", "  ")
	if err != nil {
		return lightweightStoryPromptContext{}, err
	}

	return lightweightStoryPromptContext{
		Project:                    project,
		Request:                    req,
		SelectedTagRules:           buildSelectedAutoGenerateTagRulesBlock(selectedTags),
		ExistingCharactersJSON:     string(existingJSONBytes),
		PreviousEpisodeContextJSON: string(previousContextJSONBytes),
		Metrics:                    buildLightweightStoryPromptMetrics(req.Plot),
		SceneImageWidth:            sceneImageWidth,
		SceneImageHeight:           sceneImageHeight,
		SceneImageFrameType:        describeFrameType(sceneImageWidth, sceneImageHeight),
		FixedVideoFPS:              defaultSegmentFPS,
	}, nil
}

func buildSceneSegmentationGuidance(plot string, allowCharacterSpeech bool) string {
	trimmed := strings.TrimSpace(plot)
	runeCount := len([]rune(trimmed))
	dialogueMarkers := strings.Count(trimmed, "“") + strings.Count(trimmed, "”") + strings.Count(trimmed, "\"")

	storyDensity := "中等"
	switch {
	case runeCount > 1800 || dialogueMarkers > 30:
		storyDensity = "很高"
	case runeCount > 1100 || dialogueMarkers > 18:
		storyDensity = "较高"
	case runeCount > 500 || dialogueMarkers > 8:
		storyDensity = "偏高"
	}

	base := "剧本全文约 %d 字，对白标记约 %d 处，信息密度%s。请先提取完整故事主线、因果关系、人物目标变化、空间变化、动作链与关键转折，再把剧情重组为一串完整的 story beats。然后按导演式 coverage 把 beats 拆成候选 scene，让每个候选 scene 承担明确的镜头功能，例如建立、关系、行为、反应、信息揭露或收束。scene 数量不要预设，不要为了凑固定镜头数而删剧情；必须按中文旁白的自然语气、停顿、断点、信息量和视频承载能力决定需要多少个 scene。你在输出最终 JSON 之前，必须先在内部完成一轮候选分镜试讲：先给每个候选 scene 写一句 narration 草稿，再按正常中文解说速度把这句 narration 从头到尾真实试讲一遍。估算 narration 时，不要只按字数平均，必须把年份、朝代、数字、地名、人名、官职、专有名词、顿号、引号、转折停顿、强调停顿和情绪留白一起算进自然口播时长。如果试讲时需要赶语速、吞字、省掉自然停顿、压缩专有名词解释，或者一条 narration 同时塞进过多背景说明、动作发生、结果落点和人物反应，说明这个候选 scene 还没有拆够，必须继续拆成两个或更多 scene。即使时长表面上还能勉强塞下，只要一条 narration 同时承担两个以上重信息块，例如“背景交代 + 决策变化”“动作执行 + 条件说明”“结果落点 + 人物反应”“史料原句 + 现实意义”“空间变化 + 情绪结论”，也应优先拆成相邻 scene，而不是硬压成一句全包式旁白。"
	if allowCharacterSpeech {
		base += "如果原始输入剧本已经明确写出镜头编号、场景切换、旁白、对白、机位、景别、动作或转场，请把这些原始分镜单元当成第一优先级的拆分骨架，优先沿用原始顺序和镜头意图，不要为了套用自己的概括结构而整段打散重排。只有当原始单元明显过载、逻辑冲突、时长无法承载或镜头衔接断裂时，才允许局部拆开、合并或重排，并且仍要尽量保持原始输入剧本已经给出的镜头表达。若某个候选 scene 需要听见角色开口，必须把它当成对白表演镜头来试讲：先判断当前信息应由 narration 承担，还是应由 dialogue + 可见表演承担。对白镜头里的 narration 只能保留桥接、点题或结果提示，不要继续承担大段背景解释或整段因果链；若一镜里同时需要长 narration、长 dialogue 和多步表演，说明镜头仍然过载，必须继续拆镜。"
	}
	base += "只有当每条 narration 在自然试讲下都能从容讲完、且主要信息重心足够集中时，才能确定 total_scenes 和 duration_seconds；边界情况宁可向上取整，不要卡着极限时长写。只要一段 narration 在这种真实口播推演下明显装不进当前镜头时长，尤其一旦会超过 13 秒、接近或超过 15 秒，就必须继续拆成新的 scene。任何长对白、连续争执、连续信息揭露，如果单个镜头讲不完，必须继续拆成后续 scene，直到故事讲通，同时保持空间骨架、左右站位和动作方向清晰。"
	return fmt.Sprintf(base, runeCount, dialogueMarkers, storyDensity)
}

func buildSpeechModeSystemRules(allow bool) string {
	if !allow {
		return strings.TrimSpace(`
【对白与音频规则】
- 每个 scene 仍然必须返回 dialogue 字段，但该字段必须是空字符串。
- narration 是当前镜头唯一的语言推进载体；凡是剧情推进必须落在 narration 内，而不是放进 dialogue 或 Audio。
- Audio 只能写环境音与环境声源，不写台词，不写旁白，不写角色名字。
- 可以有情绪性口部动作，例如嘴唇轻张、咬牙、抿嘴、下颌绷紧，但禁止明确说话口型、禁止连续念台词口型、禁止多人抢口型。`)
	}

	return strings.TrimSpace(`
【对白与表演规则】
- 每个 scene 都必须返回 dialogue 字段；只有当前镜头真的需要听见角色说话时才填写中文台词，其他镜头返回空字符串。
- 如果原始输入剧本已经明确给出镜头编号、场景切换、旁白、对白、机位、景别、动作或转场，必须优先沿用这些原始分镜单元来组织 scenes，不要把用户已经写好的镜头结构整段打散重排。
- 只有当原始输入里的单个镜头明显过载、时长承载不下、表演与镜头逻辑冲突，或与上下镜衔接断裂时，才允许局部拆开、合并或重排；即使调整，也要尽量保留原始镜头顺序、原始对白归属和原始镜头意图。
- dialogue 负责承载当前镜头真正需要听见的劝说、试探、命令、回击、谈判、安慰、摊牌或短促交流；不要把整段小说对白整包搬进 dialogue。
- 一旦某个 scene 返回非空 dialogue，该 scene 的 narration 只能保留桥接、点题、结果提示或必要背景钩子，不要继续承担大段背景解释、完整因果链或长条说明；不要让 narration 和 dialogue 在同一镜里重复说同一层信息。
- 有对白的镜头必须优先建立说话 blocking：主说话者成为最清晰的口型与上半身表演承载位，听者必须留在画面内承担反应位；优先双人同框、中近景双人、前后层次双人、过肩或侧拍，不要默认双人都正对镜头。
- 有对白的镜头不能写成站桩说话。image_prompt 和 video_prompt 必须同步写出主说话者的嘴部、下颌、呼吸、眉眼、肩颈、手势、重心和步态变化，以及听者的目光、停顿、压迫、回避、打断或被说服反应。
- video_prompt 的台词文本只能写进 Audio 行，不能把对白文字写进 Phase 行。Phase 只写说话者的可见表演、听者反应、镜头变化和环境变化。
- 单个 phase 只允许 1 个清晰主说话者；听者默认闭口。若一镜里需要两人轮流清楚开口，必须拆到 Phase 1 / Phase 2，或者继续拆成更多 scene。
- Audio 行必须保持单行；可以写成“用中文以【声线/语气】清晰说到:"台词"；环境音……”这种结构，但不要写角色名字。
- 长对白、密集对骂、连续问答、连续宣读、连续解释，只要在当前 duration_seconds 内自然语速讲不完，或与 narration、动作表演同时过载，就必须拆成更多 scene，不要硬塞在一个 scene 里。
- 说话镜头里仍然禁止多人同时抢口型，仍然要锁定人物脸、年龄感、体型、发型和服装层级。`)
}

func buildSpeechModeUserRules(allow bool) string {
	if !allow {
		return strings.TrimSpace(`
- 每个 scene 都要返回 dialogue 字段，但必须为 ""。
- Audio 只能写环境音，不要写台词文本，不要让人物出现明确说话口型。`)
	}

	return strings.TrimSpace(`
- 需要听见角色开口的镜头才返回 dialogue；无对白镜头的 dialogue 返回 ""。
- 如果原始输入剧本已经写了镜头编号、场景、旁白、对白、机位、动作、景别或转场，请优先按这些原始分镜单元输出，不要擅自把现成镜头结构打散成另一套。
- 只有当原始镜头本身明显过载、长度装不下、表演与机位冲突，或上下镜衔接不通时，才允许局部拆镜或合并，并尽量保留原剧本的顺序、对白归属和镜头意图。
- dialogue 只写当前镜头实际需要听到的中文台词，推荐一行一句，格式为“角色名：台词”；不要把整段小说对白全搬进去。
- 有对白的镜头里，narration 只保留桥接、点题、结果提示或必要背景钩子，不要再把同一层信息重复讲给观众。
- 有对白的镜头里，image_prompt 要让主说话者成为最清晰的口型与上半身表演承载位；听者必须留在画面里做反应，不要写成双方都傻站着说。
- 说话镜头优先内部推理成稳定双人同框、中近景双人、过肩、侧拍或前后层次双人，不要默认做成两个正对镜头的人像站桩图。
- video_prompt 里，台词文本只能写进 Audio 行；Phase 行只写可见动作、表情、口型承载、听者反应和镜头变化。
- 说话镜头的 video_prompt 必须比不开口镜头更重表演：主说话者要同时有嘴部、下颌、呼吸、眉眼、肩颈、手势、重心或步态变化；听者要有停顿、回避、压迫、打断、被说服或不耐烦等反应。
- 如果同一镜需要两人轮流清楚说话，请拆到两个 phase 或继续拆成更多 scene，不要让多人同时抢口型。
- 长对白必须拆分；如果按真实中文语速和停顿讲不完，或者 dialogue、narration 和表演三者在同一镜里同时过载，就继续增加 scene。`)
}

func buildNarrationModeRule(allow bool) string {
	if !allow {
		return "18. narration 必须是中文解说，不是对白，不是台词，不是散文，信息密度高，节奏清楚，情绪准确。所有对白都必须改写成解说式表达，不能直接整段搬运原文台词。"
	}

	return "18. narration 必须是中文解说，不是对白，不是台词，不是散文，信息密度高，节奏清楚，情绪准确。有对白的 scene 里，narration 只保留桥接、点题、结果提示或必要背景钩子，不要继续承担同一层冲突、劝说、谈判、安慰或回应信息；真正需要听见的话写进 dialogue，并让 dialogue 通过可见表演来推进。"
}

func buildAudioModeRule(allow bool) string {
	if !allow {
		return "56. Audio 行只能写环境音与环境声源变化，不写台词，不写旁白，不写角色名字。"
	}

	return "56. Audio 行只承担当前 phase 的中文台词与环境音组合，不写旁白，不写角色名字，也不要让多个说话者在同一 phase 同时清晰开口。"
}

func stripMarkdownSectionByHeading(text string, heading string) string {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(heading) == "" {
		return strings.TrimSpace(text)
	}

	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	start := strings.Index(normalized, "\n"+heading)
	if start < 0 {
		if strings.HasPrefix(normalized, heading+"\n") || normalized == heading {
			start = 0
		} else {
			return strings.TrimSpace(text)
		}
	} else {
		start++
	}

	end := len(normalized)
	searchStart := start + len(heading)
	if searchStart < len(normalized) {
		lines := strings.Split(normalized[searchStart:], "\n")
		offset := searchStart
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") {
				end = offset
				break
			}
			offset += len(line) + 1
		}
	}

	rebuilt := strings.TrimSpace(normalized[:start] + normalized[end:])
	return rebuilt
}

func buildExistingCharacterHardRules(hasExistingCharacters bool) string {
	if !hasExistingCharacters {
		return strings.TrimSpace(`7. 本次 existing_characters 为空，不存在既有锁定角色资产；不要虚构旧角色来源、旧外观资产或上一集遗留人物关系。
8. characters 数组必须返回本集实际涉及的全部正式角色。
9. scenes 中出现的人物锚点以本次返回的 characters 为唯一角色来源。`)
	}

	return strings.TrimSpace(`7. 你收到的 existing_characters 是项目已锁定角色资产。它们只作为续写输入和场景复用依据，不允许修改，不允许润色，不允许扩写，不允许再次作为旧角色回填到输出的 characters 数组。
8. 当 existing_characters 非空时，characters 数组只能返回本集首次出现的新角色，禁止把已有锁定角色再次返回。
9. scenes 里只要出现已有角色，必须直接复用 existing_characters 里的 height 与 appearance 作为永久人物锚点来源，在 scene 的主体行中按原有关键词和顺序展开，不要重写成另一张脸。`)
}

func buildEpisodeContinuityHardRules(hasPreviousEpisode bool) string {
	if !hasPreviousEpisode {
		return strings.TrimSpace(`57. episode_memory 必须完整返回 story_summary、ending_state、character_status、open_threads。
58. 本次没有上一集上下文；不要虚构上一集剧情、上一集结尾状态、既有悬念或历史人物关系。
59. character_status 数组必须覆盖本集实际参与剧情推进的重要角色。`)
	}

	return strings.TrimSpace(`57. episode_memory 必须完整返回 story_summary、ending_state、character_status、open_threads。
58. episode_memory 必须严格承接 previous_episode_context；禁止重讲上一集，必须接着上一集结束状态和悬念推进。
59. character_status 数组必须覆盖本集实际参与剧情推进的重要角色，不论他们是旧角色还是新角色。`)
}

func normalizeStoryCharacterRecord(char models.Character) lightweightStoryCharacter {
	return lightweightStoryCharacter{
		Name:       strings.TrimSpace(char.Name),
		Gender:     strings.TrimSpace(char.Gender),
		Age:        strings.TrimSpace(char.Age),
		Height:     strings.TrimSpace(char.BodyHeight),
		Era:        strings.TrimSpace(char.Era),
		Country:    strings.TrimSpace(char.Country),
		Appearance: strings.TrimSpace(char.Appearance),
	}
}

func loadExistingStoryCharacters(projectID uint) ([]models.Character, []lightweightStoryCharacter, error) {
	var records []models.Character
	if err := db.DB.Where("project_id = ?", projectID).Order("id asc").Find(&records).Error; err != nil {
		return nil, nil, err
	}

	output := make([]lightweightStoryCharacter, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		normalized := normalizeStoryCharacterRecord(record)
		if normalized.Name == "" {
			continue
		}
		if _, exists := seen[normalized.Name]; exists {
			return nil, nil, fmt.Errorf("duplicate existing character name: %s", normalized.Name)
		}
		seen[normalized.Name] = struct{}{}
		output = append(output, normalized)
	}

	return records, output, nil
}

func parseEpisodeMemoryJSONItems[T any](raw string) ([]T, error) {
	if strings.TrimSpace(raw) == "" {
		return []T{}, nil
	}
	var items []T
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, err
	}
	if items == nil {
		return []T{}, nil
	}
	return items, nil
}

func loadEpisodeMemory(projectID uint, episode int) (lightweightStoryEpisodeMemory, error) {
	context := emptyEpisodeMemory()
	if episode <= 0 {
		return context, nil
	}
	var record models.EpisodeMemory
	if err := db.DB.Where("project_id = ? AND episode = ?", projectID, episode).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return context, fmt.Errorf("missing episode_memory for episode %d", episode)
		}
		return context, err
	}

	characterStatus, err := parseEpisodeMemoryJSONItems[lightweightStoryEpisodeCharacterStatus](record.CharacterStatusJSON)
	if err != nil {
		return context, fmt.Errorf("invalid previous episode character_status JSON: %v", err)
	}
	openThreads, err := parseEpisodeMemoryJSONItems[string](record.OpenThreadsJSON)
	if err != nil {
		return context, fmt.Errorf("invalid previous episode open_threads JSON: %v", err)
	}

	context.StorySummary = strings.TrimSpace(record.StorySummary)
	context.EndingState = strings.TrimSpace(record.EndingState)
	context.CharacterStatus = characterStatus
	context.OpenThreads = openThreads
	return context, nil
}

func loadPreviousEpisodeContext(projectID uint, currentEpisode int) (lightweightStoryEpisodeMemory, error) {
	context := emptyEpisodeMemory()
	if currentEpisode <= 1 {
		return context, nil
	}
	return loadEpisodeMemory(projectID, currentEpisode-1)
}

// Legacy prompt builder kept only as migration reference.
// Active dispatch now lives in buildLightweightStoryPrompts and the two mode-specific prompt files.
func buildLightweightStoryPromptsLegacy(project models.Project, req models.AutoGenerateRequest, existingCharacters []lightweightStoryCharacter, previousEpisodeContext lightweightStoryEpisodeMemory) (string, string, error) {
	hasExistingCharacters := len(existingCharacters) > 0
	hasPreviousEpisode := req.Episode > 1

	var existingJSON string
	if hasExistingCharacters {
		existingJSONBytes, marshalErr := json.MarshalIndent(map[string]interface{}{
			"existing_characters": existingCharacters,
		}, "", "  ")
		if marshalErr != nil {
			return "", "", marshalErr
		}
		existingJSON = string(existingJSONBytes)
	}

	var previousContextJSON string
	if hasPreviousEpisode {
		previousContextJSONBytes, marshalErr := json.MarshalIndent(map[string]interface{}{
			"previous_episode_context": previousEpisodeContext,
		}, "", "  ")
		if marshalErr != nil {
			return "", "", marshalErr
		}
		previousContextJSON = string(previousContextJSONBytes)
	}

	sceneSegmentationGuidance := buildSceneSegmentationGuidance(req.Plot, req.AllowCharacterSpeech)
	speechModeSystemRules := buildSpeechModeSystemRules(req.AllowCharacterSpeech)
	speechModeUserRules := buildSpeechModeUserRules(req.AllowCharacterSpeech)
	narrationModeRule := buildNarrationModeRule(req.AllowCharacterSpeech)
	audioModeRule := buildAudioModeRule(req.AllowCharacterSpeech)
	existingCharacterHardRules := buildExistingCharacterHardRules(hasExistingCharacters)
	episodeContinuityHardRules := buildEpisodeContinuityHardRules(hasPreviousEpisode)
	generationModeLabel := "旁白驱动 + 导演式视觉分镜 + 图像视频提示词生成"

	systemPrompt := fmt.Sprintf(`你是“%s”这条生成链的唯一内容生成器。

你必须严格执行以下硬约束：
1. 只能返回一次、且只能返回一个完整 JSON。
2. 禁止输出 JSON 之外的任何解释、标题、注释、代码块标记。
3. 顶层 JSON 必须且只能包含：total_scenes、characters、scenes、episode_memory。
4. 禁止先返回摘要再返回详情。
5. 本次输出必须完整、稳定、可直接入库，禁止缺字段、缺角色、缺镜头、缺提示词。
6. 所有字段值必须使用简体中文；只有 video_prompt 的固定标签允许使用英文 Style / Phase / Audio，标签后的正文内容必须使用中文。
%s
10. 新角色必须完整生成 name、gender、age、height、era、country、appearance。
11. gender 只能返回：男性、女性、其他。
12. age、height、era、country 都必须明确，禁止模糊词。age 应尽量写成明确年龄，例如“16岁”“24岁”“31岁”；height 必须写成可视化的明确身高表达，例如“一米七二”“约一米八五”，不能只写“高挑”“偏高”“娇小”。
13. appearance 只写“永久人物锚点”，不写可变服装、不写可变配饰、不写手持物、不写临时动作。appearance 必须清晰包含：发型、脸型、额头与眉骨、眼型、眼距或眼睑特征、鼻梁与鼻尖、唇形与下巴、颧骨或下颌线、肤色、身材比例、明确身高、明确年龄、可见年龄锚点、国别或文化、特征识别点。发型必须写清长度、分线、颜色、卷直程度、刘海形态、发量或蓬松度、是否束发，以及发际线或鬓角特征。年龄不能只停留在数字字段，也不能只写“青年感、少女感、成熟感、中年感”这类模糊词；appearance 里必须把年龄落实成模型能看见的线索，例如面颊饱满度、眼下状态、法令纹深浅、下颌紧致度、颈部状态或体态年龄感。只要是成年人男性且当前或后续镜头可能看见下半张脸，appearance 里还必须明确胡须状态，例如下巴干净无胡须、上唇无胡茬，或胡须样式固定为何种状态。国别或文化身份要优先使用模型更容易稳定理解的表达，例如中国女性、中国古代男性、东亚女性，不要把“华夏青年女性”这类宽泛文学化身份词当成核心人物锚点。appearance 同时是后续所有 scene 复用的固定身份块，后续 scene 的永久人物锚点部分必须尽量沿用 appearance 的原有关键词和顺序，不要改写成同义词。
14. 角色脸部禁止模板化。你必须主动拉开同一集角色之间的脸部结构差异，禁止批量生成“窄脸、高鼻梁、薄唇、冷白皮、狭长眼”这类重复模板脸；必须让每个角色拥有可区分的额头、眉骨、眼距、眼尾、鼻型、唇厚、下巴或颧骨差异。若是同国别、同年龄层、同性别角色，至少主动拉开五个脸部维度，不能只靠发型、妆感或服装区分。
15. scenes 必须先服务“完整讲清故事”，再服务导演式 visual coverage。你必须先提取完整故事主线、人物关系变化、人物目标变化、因果链、关键转折与结果，再把剧情重组为连续 story beats，最后按导演视角拆分 scene；禁止只抽取燃点导致故事不通，禁止平淡过渡镜头。
16. scene 数量必须由故事完整度、旁白长度、中文语气停顿、信息密度、镜头功能和视频承载能力共同决定，不允许预设固定镜头数；你必须先完成一轮候选 scene 的 narration 草稿与内部试讲，再根据试讲结果确定最终 total_scenes；如果一个镜头讲不完，就继续拆分新的 scene，直到故事整体通畅。
17. 每个 scene 必须返回 duration_seconds，范围只能是 3 到 13 秒。
%s
19. narration 必须与 duration_seconds 匹配，并按中文解说的自然语气、停顿和断点决定长度。你必须先给每个候选 scene 写出 narration 草稿，再在内部按正常中文解说速度真实朗读一遍，然后才允许确定 duration_seconds。估算 narration 时，不要只按字数平均，必须把年份、朝代、数字、地名、人名、官职、专有名词、顿号、引号、转折停顿、强调停顿和情绪留白一起算进自然口播时长。若内部试讲时需要赶语速、吞字、省掉自然停顿、跳过解释，或一条 narration 同时承担过多背景说明、动作发生、结果落点和反应信息，说明这个候选 scene 仍然过满，必须继续拆成后续 scene，而不是硬压到当前时长。即使一条 narration 在秒数上勉强装得下，只要它同时承担两个以上重信息块，也应继续拆镜，让每个 scene 保持单一主要信息重心。只有当 narration 在自然试讲下从容成立、且主要信息重心清晰时，才能保留该 scene；边界情况宁可向上取整，不要卡在极限时长。只要一段旁白在这种真实口播推演下仍然讲不完，尤其一旦自然口播会超过 13 秒、接近或超过 15 秒时，绝不能留在单镜头里。
%s
20. image_prompt 必须严格使用中文模板，并按以下 5 个标签顺序组织：
    主体：
    场景：
    构图：
    光影：
    约束：
    可以在单个标签下继续补充内容，但不要新增“主体补充”“场景补充”“构图补充”这类额外标签；多人、多群体或多层远景也仍然写在对应原标签段内。
21. 只要某个 scene 出现角色，就必须在该 scene 的“主体”行里写出“国别或文化身份 + 性别 + 明确年龄 + 明确身高 + 当前机位可见的永久人物锚点 + 当前镜头状态（服装、配饰、手持物、表情、姿态、动作）”；若仍需补年龄阶段，也必须放在明确年龄之后，例如“中国女性，24岁，年轻成年，约一米七二”“中国男性，30岁，成熟成年，约一米八五”。不要写成“20岁青年感”“青年女性”“华夏青年男性”这种模糊或文学化身份短语，也不要写成“女性，中国”这种拆散形式。不要写角色名字，不要写“同上角色”，只写可直接用于图像生成的可视化内容。只要是成年人男性且当前镜头能看见下半张脸或下巴区域，就必须显式写出胡须状态，不要把是否有胡子留给模型猜。
22. 对出现在 scene 中的每个角色，scene 不是照抄完整 appearance。你必须先判断当前镜头的机位、朝向、遮挡和动作，再从该角色 appearance 与 height 中挑选当前真正看得见的稳定锚点来写；可见锚点仍要尽量复用 appearance 的原有关键词和顺序，但不要为了复用而硬把看不见的正脸细节写进去。若当前镜头是正面或近正面，可多写眉眼、鼻唇和下巴；若是侧脸、三分之二侧身、低头、俯身、跪拜、背身、回头、遮挡、群像远景，就应改写为当前可见的轮廓、发型、发际线、耳部、侧脸线条、身高比例、肩背姿态、服装层次和动作方向，禁止把本应背对镜头的人物硬写成正面对镜头露脸。发型也必须按同样原则复用，不要上一镜写“黑色长直发偏中分”，下一镜无故改成“侧分卷发”或“高马尾”。
23. image_prompt 中每个出场人物都要保留足够稳定锚点，但锚点数量与类型必须服从当前机位可见性。正面、中近景或特写人物，至少写 6 个稳定锚点，其中至少 4 个是脸部锚点；侧脸、背身、跪拜、俯身、远景或有明显遮挡的人物，则改为优先写朝向、发型和发际线、头脸轮廓、身高和体型比例、肩背姿态、服装层次、动作方向和可见识别点，不要硬凑不可见的脸部细节。推荐顺序是：身份短语（含年龄与身高） -> 朝向或机位可见关系 -> 当前可见的发型和头脸轮廓 -> 当前可见的五官或侧脸线条 -> 身材、年龄状态与识别点 -> 当前镜头状态。
24. 当前镜头状态里的服装不能只写“西装”“礼服”“长裙”“外套”这类大类词，必须至少写清颜色、材质、版型或剪裁、长度或层次；如果服装是视觉重点，再补领口、肩线、袖型、裙摆、裤型、装饰等细节。若服装存在内层、打底、中层、外披、披帛、斗篷、披肩或短褂等层级，必须固定按“贴身层 -> 中间层 -> 外层”顺序写清，每一层的颜色、材质和类别都不能互换。
25. image_prompt 必须明确景别、位置关系、镜头重心和镜头功能；建立镜头优先交代空间与站位，关系与行为镜头优先交代左右关系和动作通道，反应与信息镜头优先保证脸部、手部、道具和证据可读。允许换装，但必须清楚写明仍是同一人物，不得让人物变成另一个人。同一镜头里如果有两个及以上同国别同年龄层同性别角色，必须把各自脸部锚点、身高和体型差异写开，不能只写“长发女性”“西装男性”。多人或群像镜头里，还必须明确谁是叙事中心、谁是重要联动人物、谁是背景反应层、谁看向谁；除非当前镜头明确是第一人称视角、直视观众、肖像式定格或人物明确对镜头内“观众位置”说话，否则背景人物、群演以及主要人物都不要无缘由看镜头。单镜头里只允许 1 个叙事中心人物使用完整的“脸部 + 发型 + 服装层级”高密度描述；其余人物必须按可见性降密度，只写当前可见关键锚点，避免不同人物之间串发型、串服装、串身份。若当前镜头本质上是双人关系、追问、阻拦、离场对峙、告别、试探、劝说或情绪拉扯，而两人都对剧情推进重要，优先写成稳定的双人同框、中景双人、同侧并行、前后层次双人或一前一后关系镜头；除非故事明确要求切单人视角，否则不要默认拆成过度碎裂的正反打。以上这些镜头分类与人物层级词只用于你内部推理，不要原样写进最终 image_prompt 或 video_prompt；最终 prompt 只能保留模型能直接看懂的景别、站位、朝向、动作和视线事实。
26. 镜头朝向必须服从故事动作逻辑和导演视角，而不是为了展示人物资产而强行正面露脸。若剧情要求角色跪拜、叩首、背身离开、俯身查看、转头回望、侧身阻拦、朝门而立、朝皇帝或高位者俯首，那么主体行和构图都必须明确这类朝向关系，例如背对镜头、三分之二侧身、低头半侧脸、面朝高台跪伏、肩背朝向镜头但侧脸略露；不要把本应朝向别人的人物写成正对镜头站着。若需要同时看清高位者和下位者、说话者和回应者，优先使用侧拍、过肩、反打、高低机位或拆成相邻镜头，不要把所有人都摆成正对镜头。若高位者、说话者或权力中心不是当前镜头叙事中心，只保留其高位身份、位置、服装主色块、可见轮廓和视线方向，不要把背景高位人物写成和叙事中心同密度的完整角色块。最终输出里不要写“侧拍、过肩、反打、轴线稳定、视线互锁、主位、次位、背景反应位”这类导演术语；要把它们翻译成直接的可见结果，例如“机位在右后方，只见对方半侧脸”“后排官员同时转头看向闯入者”。
27. 同一段连续剧情、同一地点连续镜头、同一时间段内，如果角色没有明确换装，你必须复用同一套服装关键词；不要上一镜写“黑色丝绸浴袍”，下一镜改成“深色家居袍”，也不要上一镜写“红色晚宴长裙”，下一镜只写“礼服”。
28. 发型默认属于永久人物锚点；同一段连续剧情、同一地点连续镜头、同一时间段内，如果剧情没有明确原因，你必须复用同一套发型关键词，不要随意改变分线、长度、卷直程度、刘海形态、束发状态、发量蓬松度。若一个角色的基础发型由多层结构组成，例如“高颅顶 + 偏侧分 + 半束半披 + 细碎斜刘海”，连续镜头里必须按同一结构顺序复写，不要丢层、拆层或互换层级。若只发生临时发丝变化，只能在当前镜头状态里追加“发尾被风吹乱”“额前碎发被雨打湿”“低马尾略松散”这类暂时状态，同时保留原始发型锚点；若剧情确实发生盘发、散发、扎发、剪发等变化，必须在 narration 与 image_prompt 里都明确写出变化原因和变化后的具体新发型。
29. image_prompt 的场景行不能只写地点标签、关系标签或剧情标签，必须把场景翻译成可复现的空间骨架：空间类型、功能区、3 到 6 个稳定空间锚点、至少 1 个材质或装修锚点、至少 1 个朝向或位置关系。
30. 不要把山名、城名、寺名、湖名、桥名、街名、村名、殿名、楼名等专有地点名称，当成模型会自动理解的视觉锚点。专有地点只属于叙事层；到了 image_prompt 和 video_prompt，你必须先把它翻译成可见结构，例如山脊走势、坡面植被、低矮屋脊、城墙轮廓、飞檐层级、塔身剪影、水岸形状、桥体结构、石阶、院墙、材质和空间层次。名字最多只能作为补充，绝不能代替可见描述。
31. 禁止在 image_prompt 和 video_prompt 中写“X方向”“Y一带”“通往Z的路”“本应是X的位置”“右上方留出某地”这类把叙事坐标或地名当视觉锚点的表达。必须改写成画面里真正看得见的地形、建筑、屋脊、树线、水系和空间层次；远景地标尤其要写成可见轮廓，而不是只写名字。
32. 场景骨架必须服从故事时代、地域、文明语境、社会阶层、建筑类型和场合。你必须据此推理建筑、家具、器物、照明、景观、地面、门窗、墙面与陈设，禁止古代镜头混入现代建筑、现代灯具、电梯厅、玻璃幕墙、城市草坪等不合语境元素，也禁止现代镜头无缘由混入古代器物、宫灯、木格窗、石阶院落等不合语境元素。
33. 场景锁定必须以“正向锁定”为主，而不是在提示词里逐项点名不想要的东西。只要故事语境存在时代、地域、民族聚落、宗教建筑、历史风貌、阶层空间、架空文明等高误判风险，场景行就必须显式写出时间锚点、地域或文明锚点、建筑类型锚点、屋顶和门窗样式、墙体与道路材料、照明和器物系统、空间骨架；例如该写“夯土墙、青黑瓦、石板坡路、木栅栏、油灯火盆”，就直接正向写清。不要在约束行里逐项枚举“不要柏油路、不要汽车、不要玻璃幕墙、不要电线杆”这类具体错误物体，因为点名错误物体会反向激活模型。约束行只允许补充高层级的语境一致性，例如“保持明代川西南山地砖木聚落语境，建筑、道路、器物、照明与服化保持同一时代地域系统”；必须按当前故事语境自行推理，不能机械套用同一组词。
33. 人物民族、国别或文化身份不会自动替代场景建筑风格。即使主体写了“彝族男性”“苗族女性”“宫廷女性”“现代白领”，场景行也仍然必须单独写清对应的村寨、院落、城镇、宫室、会所、街区或宗教建筑的可见结构、屋顶形式、门窗样式、墙体材料、道路材质和环境陈设；不要指望模型自动把人物身份推理成场景建筑。
34. 如果当前镜头是建立镜头、中远景、群像镜头或远景镜头，场景行的信息量必须不低于主体行。此时主体行保留足够识别人物的稳定锚点即可，更多字数要优先分配给地形、建筑、材质、器物、远景轮廓、道路和门窗细节，避免模型把远景和建筑自动补成默认现代样式。最终“构图”行不要写“建立镜头、关系镜头、事件级镜头、反应镜头、信息镜头、收束镜头”这类分类词，只写景别、机位、人物位置关系、运动通道和视线方向这些直接可见事实。
35. 如果远景、中景或背景里存在村寨、城镇、寺院、宫室、院落、街区、码头、驿站、军营、集市、山村、湖岸聚落等建筑群，必须写出它们的屋顶形制、墙体材料、门窗样式、道路或庭院材质、栏杆围墙、炊烟灯火或旗帜挂饰等可见建筑线索，不能只写“低矮土房”“远处村落”“寺庙方向”“古城轮廓”。
36. 如果同一集有多个镜头发生在同一地点，你必须复用同一组空间骨架关键词，只改变镜头角度、人物站位、局部光线和前景道具；禁止同一处建筑空间在连续镜头中突然变成完全不同的结构、材料和照明逻辑。连续对峙、交谈、阻拦、追问镜头还要尽量保持左右站位、视线方向和动作方向稳定。禁止写“同一场景”“同上场景”“继续在原地”这种省略表达。
37. image_prompt 的“约束”行必须显式包含：禁止字幕、禁止画面文字、禁止水印、禁止界面元素；若当前镜头存在高误判风险，可以再补一句最高层级的语境一致性约束，例如保持某时代、地域、文明、建筑和服化系统一致，但不要逐项列出具体不想出现的错误物体名。
38. 项目画风属于系统内部预设，不会传给你，也不允许你在 image_prompt 中返回“风格：”一行或任何画风描述；你只负责返回画面内容本身，系统会在提交 ComfyUI 时单独拼接项目画风。
39. image_prompt 正文必须保持画风中性。禁止在主体、场景、构图、光影里写“写实、摄影、剧照、插画、动画、动漫、国漫、二次元、3D、CG、渲染、电影级、电影感、胶片感、真实皮肤纹理、真实毛孔、真实体积感”这类画风、媒介、渲染、质感词；这些内容全部交给系统顶部拼接的项目画风去控制。
40. image_prompt 只能写模型能直接视觉理解的内容：外观、服装、姿态、表情、动作、空间、光线、材质、构图。对人物优先写当前机位真正看得见的骨相、轮廓、身高、体型、情绪、服装与动作；不要为了复用角色资产而把看不见的正脸五官硬塞进背身、低头、侧身或跪伏镜头。发型要先写基础发型锚点，再在需要时补临时发丝状态。对场景优先写地形、建筑、植被、水系、空间骨架和材质，再写氛围。光影行只写客观光源、时段、照射方向、色温和明暗关系，不要借光影行夹带画风与质感。禁止写“女主、男主、反派、二叔、打脸、爽感、反转、权斗、社死、算计、掌控全局、梨花带雨”这类故事标签、抽象判断或文学化修辞；必须改写成可见画面。如果这一镜后续视频需要明显动作、互动或受控运镜，首帧图必须写成动作发生前半拍到一拍的稳定起点，把重心、手臂位置、视线目标、道具准备状态、可运动空间和可动环境锚点写出来，不要把首帧写成动作已经做完的结果态。scene 本身要承担建立、关系、行为、反应、信息揭露或收束中的一种主要镜头功能，构图和动作起点必须服务这个镜头功能。
41. 如果当前 scene 中存在雨丝、烟雾、蒸汽、烛火、灯焰、火光、水面、涟漪、布帘、纱幔、旗帜、落叶、草叶、挂饰、灰尘等天然可动环境元素，你必须在 image_prompt 中主动点出 1 到 3 个最重要的可动环境锚点，并写清它们处于可继续运动的状态，例如“雨丝斜落”“烛火轻晃”“布帘被风掀起一角”“水面反光破碎”。不要把这些元素只当静态名词写过去。
42. 如果 scene 中存在背景人物、围观者、陪衬人物或群体人物，你必须让他们的服装、发型、年龄层、时代感、地域感、阶层感、职业感和所处场合一致，不能出现穿帮。背景人物不能穿出与故事时代、地点、身份冲突的衣着，也不能突然带出现代或古代错位元素。若某一组背景人物在画面里持续可见，例如侍卫、宫女、官员、随从、士兵、店员、同学、宾客、围观者，你必须给这组人写出稳定的组服化锚点，例如主色块、层级、材质或统一制式，让模型知道他们是一组同系统人物，而不是每个人自由变色。
43. video_prompt 必须严格使用固定英文标签模板，且必须正好是 5 行；标签只能是英文，标签后的正文内容使用中文：
    Style:
    Phase 1 (起止秒数):
    Audio:
    Phase 2 (起止秒数):
    Audio:
44. video_prompt 的时间范围必须和 duration_seconds 严格对应，阶段1 从 0.0 秒开始，阶段2 必须接续到 duration_seconds 结束。
45. video_prompt 不要写角色名字。若画面里有角色，必须使用“画面位置 + 明确年龄 + 可见外观锚点”的方式指代角色，例如“左侧中国女性，24岁，黑色长直发，深色礼服”“右侧中国男性，30岁，下巴干净无胡须，深蓝西装”；若需要补年龄阶段，也必须放在明确年龄之后。不要用数据库姓名指代谁在动，也不要只写“青年男性”“青年女性”“华夏女子”这类会让模型重新猜脸的模糊指代。
46. video_prompt 也不要把山名、城名、寺名、湖名、桥名、方向词和叙事坐标词，当成模型会自动理解的动作或环境锚点。若需要指向远景地标，必须改写成可见轮廓和空间关系，例如远处层叠屋脊、山体缺口、水岸线、寺院飞檐剪影、沿坡树线，而不是“某寺方向”“某城那边”“某山的位置”。
47. video_prompt 只能写模型能直接理解的动作、表情、镜头变化、环境变化和环境音。禁止写“女主、男主、反派、二叔、反转感、压迫感、爽感、修罗场、录音口型声、AI换脸感、黑料感、打脸感”这类故事标签、抽象判断或模型无法落地的组合词；也禁止把“建立镜头、关系镜头、行为镜头、反应镜头、信息镜头、收束镜头、事件级镜头、主位、次位、背景反应位、视线互锁、轴线稳定、信息核心、回忆感、压迫感、生活感、中等强度、微到中微、受控”这类导演术语、抽象风格判断或控制词原样写进最终 video_prompt。它们只用于你内部思考，最终输出时必须翻译成直接可见、可执行的动作和镜头事实。
48. video_prompt 必须按事件自然速度写动作，不要把速度默认压慢。静态观察、犹豫、悄声试探、慢性情绪沉淀这类镜头可以慢；日常走步、转身、伸手、开门、取物、靠近、后退等普通行为应写成正常速度；追拦、扑救、冲刺、跌退、甩物、闯入、奔逃、坍塌、洪水倒灌、地震、动物扑冲这类事件必须写成符合剧情的正常偏快或明显快速速度。除非剧情本身慢，不要默认使用“缓慢、轻微、极轻、慢速、轻推、轻跟、稍停半拍”这类降速词。禁止无缘由奔跑、乱打、镜头乱飘。嘴部动作必须与当前说话模式一致：不开口模式只允许情绪性口部变化；开口模式只允许当前主说话者承担清晰说话口型，其他人物不得多人同时抢口型。
49. video_prompt 可以写镜头语言，但要用直接的物理结果描述，而不是抽象的导演术语或默认慢速词。只有在剧情确实慢下来时，才写慢推、慢跟或停顿；普通动作就写正常速度跟进、顺势推进、短促转向、顺着人物动作跟一段、被冲击带得一晃后找稳；强事件则允许快跟、急停、短促震动和快速找稳。禁止大幅甩镜、环绕乱飞、没有叙事目的的推拉摇移。每个 phase 应围绕 1 个主导事件组织，可以包含 1 个主导人物动作、1 到 2 个重要联动反应、若干弱背景反应和 1 个主导环境或镜头变化；不要在同一个 phase 里堆满三四个人互不相关的独立大动作。若有群体或背景人物，必须写清他们看向谁、朝向哪里，不要只写“众人骚动”而没有视线中心。除非剧情明确要求人物真正离开画面、追出画面或从画外再入，否则动作应尽量在当前构图内完成，不要让主位人物无缘由走出镜头边界又重新回到画面；尤其是双人关系镜头、对峙镜头、告别镜头和阻拦镜头，应优先保持关键两人持续留在画面内，让动作在同框关系里完成。
50. video_prompt 的表现优先级必须固定为：先写叙事中心人物动作，再写与当前事件直接相关的重要联动反应，最后才补环境动态。环境动态只能增强画面生命力，不能取代人物动作和联动反应。群体或背景人物默认共享同一个戏剧中心，例如看向闯入者、说话者、高位者、被审问者或冲突中心；除非故事明确要求，否则不要让背景人物看镜头。
51. 若画面里存在雨丝、烟雾、蒸汽、烛火、灯焰、火光、水面、涟漪、布帘、纱幔、旗帜、落叶、草叶、挂饰、灰尘等可动环境元素，video_prompt 的每个 phase 都必须至少让其中 1 个环境元素继续参与变化；不要把环境写成完全静止的背景板。
52. 若画面里存在背景人物或重要联动人物，video_prompt 可以写他们的联动反应，例如转头、停步、交换目光、肩线收紧、后撤半步、衣摆晃动、群体压低骚动，但他们的服化必须继续符合当前故事时代、地点、阶层和场合，不能突然穿帮；同时要明确他们的主要视线目标与身体方向，不要让背景人物无缘由正对镜头。若某一组背景人物在首帧图里已经有统一服色或制式，例如侍卫深青护甲、官员绯红官袍、宫女浅青襦裙、宾客深色礼服，你必须在 video_prompt 的联动反应里默认延续这组服化，不要让同组背景人物在视频阶段自由换色、换层级、换材质。
52.1 若背景人物或群体人物在剧情里本来就绑定了席位、队列、站班、门侧、廊下守位、桌边座位、殿下班列、课堂座位、会场座区、宴席席位、仪仗位置或其他固定站位，他们默认应原地维持位置关系，双脚不离位，不换位，不穿越前景；只允许原地弱反应，例如视线收紧、轻微侧头、下颌绷紧、肩线收紧、袖摆轻晃、呼吸变化、同步收声或极轻微躬身。除非剧情明确要求他们跟进、围拢、奔逃、让路、扑上前、出列、换位、离席或穿越前景，否则不要让背景人物无缘由走来走去、穿越主体前景、互换站位、改换前后排、进出画面，或做出会被模型理解成明显走步的动作。
53. 如果当前镜头属于地震、坍塌、洪水、火势、风暴、奔逃、群体惊慌、动物冲刺、器物猛震、桥面颤动、船身大晃等“事件级镜头”，video_prompt 绝不能退化成只有微表情和轻微环境摆动。你必须明确写出事件本身怎样带动画面：冲击来自哪里、谁被带动、什么结构在震、什么环境在涌、群体怎样联动、镜头是如何被冲击带着晃一下后重新找稳。事件动作应按自然速度甚至偏快速度去写，不要再默认减速。允许受冲击带动的短促震动、短促回摆、急停后找稳、顺动作方向的短促跟随、短距离快跟或快停；禁止把灾变镜头写成完全静止，也禁止失控甩镜乱飞。
54. 如果当前镜头主位是动物、牲畜、飞鸟、犬只、马匹、猛兽或其他非人主体，必须把它当成真正叙事中心来写，不能默认它们只做极弱背景微动。你必须写清身体动力学，例如重心、步态、四肢发力、颈部或头部转向、耳尾状态、扑跳、冲刺、急停、低伏蓄力、扑翅、踏蹄、挣脱方向，并写清人和环境如何被它带动或如何对它反应。
55. video_prompt 是纯正向动作提示词。Style、Phase 和 Audio 只写模型应当执行的动作、表情、镜头、环境和声音，不要在 video_prompt 里写“禁止字幕、禁止画面文字、禁止水印、禁止界面元素、禁止采访感、人物脸部不可改变、人物年龄感不可改变、人物体型不可改变、禁止变成另一个人”这类固定否定句；字幕、文字、水印、UI 这类固定规避项由系统工作流负向节点单独处理，人物稳定性则靠首帧图锚点、角色锚点和 blocking 来保证。
%s
%s
60. total_scenes 必须等于 scenes 数组长度，并且请把 total_scenes 放在顶层 JSON 最前面，尽早输出，方便系统在流式日志里尽早看到总镜头数。
61. 返回的 scene_id 必须从 1 开始递增，不允许重复。

最终 JSON 结构必须至少为：
{
  "total_scenes": 1,
  "characters": [
    {
      "name": "",
      "gender": "",
      "age": "",
      "height": "",
      "era": "",
      "country": "",
      "appearance": ""
    }
  ],
  "scenes": [
    {
      "scene_id": 1,
      "duration_seconds": 8,
      "narration": "",
      "dialogue": "",
      "image_prompt": "",
      "video_prompt": ""
    }
  ],
  "episode_memory": {
    "story_summary": "",
    "ending_state": "",
    "character_status": [
      {
        "name": "",
        "status": ""
      }
    ],
      "open_threads": []
  }
}`, generationModeLabel, existingCharacterHardRules, narrationModeRule, speechModeSystemRules, audioModeRule, episodeContinuityHardRules)

	userSections := []string{
		fmt.Sprintf(`请根据以下输入，一次性完整生成本集内容。

项目信息：
- project_name: %s
- project_description: %s
- episode: %d

剧本全文：
%s`,
			strings.TrimSpace(project.Name),
			strings.TrimSpace(project.Description),
			req.Episode,
			strings.TrimSpace(req.Plot),
		),
	}

	if hasExistingCharacters {
		userSections = append(userSections, fmt.Sprintf("已有角色资产：\n%s", existingJSON))
	}
	if hasPreviousEpisode {
		userSections = append(userSections, fmt.Sprintf("上一集结构化记忆：\n%s", previousContextJSON))
	}

	extraRequirements := []string{
		"顶层先返回 total_scenes，再返回 characters、scenes、episode_memory，方便流式日志尽早看到总镜头数。",
		"appearance 只写永久人物锚点；当前镜头状态只能写进 scene 的 image_prompt 主体行。",
		"appearance 是角色的固定身份块；scene 里重复人物时，永久人物锚点部分必须尽量沿用 appearance 的原有关键词和顺序，不要换同义词。",
		"appearance 是角色的固定身份块，但 scene 不是照抄完整正脸设定；scene 必须先判断当前镜头机位、朝向、遮挡和动作，只复用当前真正看得见的稳定锚点。",
		"每个角色都必须返回明确身高 height，并且 appearance 里也要把身高作为固定人物锚点写清；scene 中只要角色出现，就必须把该角色的身高一并重复出来。",
		"每个角色都必须返回明确年龄 age，并且 appearance 里还要把可见年龄锚点写清；scene 中只要角色出现，就必须把该角色的明确年龄一并重复出来，不能让模型自己猜人物老幼。若需要补年龄阶段，必须放在明确年龄之后，不能写成“20岁青年感”这种模糊短语。",
		"角色脸部不要模板化，不要批量生成同一种“高鼻梁、薄唇、狭长眼、冷白皮”脸；必须主动拉开角色之间的脸部结构差异。若是同国别、同年龄层、同性别角色，至少主动拉开五个脸部维度。",
		"appearance 必须写得足够细，尤其是发型、发际线、鬓角、额头、眉骨、眼距、眼睑、鼻梁、鼻尖、唇形、下巴、颧骨、下颌线、身高、年龄状态这些会影响 z-image 出图的结构特征。",
		"image_prompt 的主体行不要写角色名字；必须写“国别或文化身份 + 性别”的连贯身份短语，再写人物锚点和当前镜头状态，例如“中国女性”“中国男性”，不要写成“女性，中国”。",
		"image_prompt 的主体行里，角色身份短语后必须紧跟明确年龄，再写明确身高，例如“中国女性，24岁，约一米七二”；若仍需补年龄阶段，只能写成“中国女性，24岁，年轻成年，约一米七二”。不要只写“年轻感”“青年感”“少女感”“成熟感”“高挑”“娇小”“偏高”，也不要写“华夏青年女性”“华夏青年男性”这类宽泛文学化身份短语。",
		"只要是成年人男性且当前镜头能看见下半张脸、上唇或下巴区域，就必须在 appearance 与 scene 主体行里显式写出胡须状态，例如“下巴干净无胡须”“上唇无胡茬”或具体胡须样式；不要让模型自己决定这一镜有没有胡子。",
		"人物身份锚点要优先使用模型更稳定的表达，例如中国女性、中国古代男性、东亚女性；不要把“华夏”这类文学化文化词单独当成核心身份锚点。",
		"image_prompt 里每个出场人物都要根据当前镜头的可见性挑选锚点：正面或近正面镜头至少重复 6 个稳定锚点，其中至少 4 个是脸部锚点；侧脸、背身、跪拜、俯身、遮挡或远景镜头则优先写朝向、发型、头脸轮廓、身高比例、肩背姿态、服装层次、动作方向和可见识别点，不要硬凑不可见的正脸细节。",
		"即使是叙事中心人物，也不要默认正对镜头看镜头。除非当前镜头明确是第一人称视角、直视观众、镜中自视、监控正拍或人物明确对“镜头位置里的对象”说话，否则叙事中心人物也应当看向对手、目标物、门口、高位者、离场方向或情绪指向目标。",
		"主体行里要主动写清朝向与可见关系，例如正面、半侧脸、三分之二侧身、背身回头、低头半侧脸、俯身、面朝高台跪伏；不要把本应跪向皇帝、高位者、门口、祭坛或远处目标的人物写成正对镜头站着。",
		"多人或群像镜头里，必须明确谁是叙事中心、谁是重要联动人物、谁是背景反应层，以及他们各自看向谁；除非当前镜头明确是第一人称视角、直视观众、肖像式定格或人物明确对镜头内“观众位置”说话，否则背景人物和群演不要看镜头。",
		"关系镜头、反应镜头和信息镜头里，叙事中心人物的目光默认应落在镜头旁的戏剧情境目标上，而不是落在镜头中心；若要表现强情绪，优先用离镜视线、三分之二侧身、过肩或反打去内部推理，但最终 prompt 只写看得见的朝向和视线事实，不要把这些术语原样写进去。",
		"单镜头里只允许 1 个叙事中心人物使用完整的“脸部 + 发型 + 服装层级”高密度描述；其余人物降密度，只写当前可见关键锚点与视线目标，避免不同人物之间串发型、串服装、串身份。",
		"如果单个镜头里既想看清高位者或说话者，又想看清跪拜者、回应者、被压制者，而正面摆位会破坏剧情方向，就应该改用侧拍、过肩、反打、高低机位，或者直接拆成相邻镜头，不要硬塞进一个全员正对镜头的画面。",
		"如果当前镜头本质上是双人关系、阻拦、追问、情绪拉扯、离场对峙或告别，且两人都对推进重要，优先内部推理成稳定双人同框、中景双人、同侧并行、前后层次双人或一前一后关系镜头；不要默认切成碎裂正反打，也不要把其中一人轻易放到画外。",
		"发型默认属于永久人物锚点；同一段连续剧情、同一地点连续镜头、同一时间段内，如果剧情没有明确原因，必须复用同一套发型关键词，不要随意改变分线、长度、卷直程度、刘海形态、束发状态和发量蓬松度。",
		"如果某个角色的基础发型本身由多层结构组成，例如“高颅顶 + 偏侧分 + 半束半披 + 细碎斜刘海”，连续镜头里必须按同一结构顺序复写，不要丢层、拆层或互换层级。",
		"如果只是临时头发状态变化，只能在当前镜头状态里追加“发尾被风吹乱”“额前碎发被雨打湿”“低马尾略松散”这类短语，同时保留原始发型锚点；如果剧情明确换发型，必须把原因和新发型写清。",
		"当前镜头状态里的服装不能只写“西装”“礼服”“长裙”“外套”，必须至少写清颜色、材质、版型或剪裁、长度或层次；如果服装是视觉重点，再补领口、肩线、袖型、裙摆、裤型和关键装饰。",
		"如果服装存在内层、打底、中层、外披、披帛、斗篷、披肩或短褂等层级，必须固定按“贴身层 -> 中间层 -> 外层”顺序写清；不要把上一镜的内外层颜色或材质在下一镜写反。",
		"同一段连续剧情、同一地点连续镜头、同一时间段内，如果角色没有明确换装，必须复用同一套服装关键词，不要上一镜是“黑色丝绸浴袍”，下一镜只剩“深色袍子”。",
		"image_prompt 不能只写静态站姿。如果这一镜后续视频需要明显动作、互动或运镜，首帧图必须写成动作发生前半拍到一拍的稳定起点：把重心、手臂位置、视线目标、道具准备状态、可运动空间、后续可动环境锚点提前写进去；不要把首帧写成动作已经完成的结果态。",
		"如果剧情动作本身决定了人物朝向，例如跪拜、叩首、伏地、背身离场、回头、侧身阻拦、俯身查看、面朝高位者或面朝门口，首帧图必须让朝向服从动作逻辑与导演视角；不要为了展示角色脸而破坏剧情方向。",
		"群像、朝堂、法庭、祭坛、课堂、会议、审讯、宴会等多人镜头里，还必须让背景人物和群演共享同一个主要视线目标，例如闯入者、说话者、高位者或冲突中心；不要让他们无缘由看向镜头外的观众位置。",
		"如果 scene 里存在雨丝、烟雾、蒸汽、烛火、灯焰、火光、水面、涟漪、布帘、纱幔、落叶、草叶、挂饰、灰尘等可动环境元素，image_prompt 必须主动写出 1 到 3 个最重要的可动环境锚点，并写清它们处于可继续运动的状态，不要把环境写成纯静态布景。",
		"image_prompt 的场景行不能只写地点标签、关系标签或剧情标签；后面必须补足可复现的空间骨架：空间类型、功能区、3 到 6 个稳定空间锚点、材质或装修锚点、朝向或位置关系。",
		"场景锁定必须以正向锁定为主，不要靠点名错误物体来反向控制。你必须写清时间锚点、地域或文明锚点、建筑类型、屋顶和门窗样式、墙体与道路材料、照明与器物系统、空间骨架；比如古代山地聚落就直接写土路、石阶、夯土墙、青黑瓦、木栅栏、油灯、炊烟等正确结构。约束行只写高层级语境一致性，例如“保持明代川西南山地砖木聚落语境，建筑、道路、器物、照明与服化保持同一时代地域系统”；不要逐项写“不要柏油路、不要汽车、不要玻璃幕墙、不要电线杆”，因为这类具体错误物体名会反向激活模型。现代、古代、未来都按同一原则处理：正向写对的系统，不逐项点名错的系统。",
		"人物民族、国别或文化身份不会自动替代场景建筑风格。即使主体写了“彝族男性”“宫廷女性”“现代白领”，场景行也仍然必须单独写清对应的村寨、院落、街区、宫室、会所、宗教建筑或聚落结构，不要指望模型自己从人物身份推断建筑。",
		"如果当前镜头是建立镜头、中远景、群像或远景镜头，场景行的信息量必须不低于主体行；主体行保留足够识别人物的稳定锚点即可，把更多字数优先给地形、建筑、材质、器物、远景轮廓、道路和门窗细节，避免模型把远景自动补成默认现代样式。",
		"如果远景、中景或背景里存在村寨、城镇、寺院、宫室、院落、街区、码头、驿站、军营、集市、山村、湖岸聚落等建筑群，必须写出它们的屋顶形制、墙体材料、门窗样式、道路或庭院材质、栏杆围墙、炊烟灯火或旗帜挂饰等可见建筑线索，尤其要把最容易被模型自动补错的道路、墙体、窗面和照明写成正向事实，不能只写“远处村落”“低矮土房”“古城轮廓”“寺庙方向”。",
		"不要把山名、城名、寺名、湖名、桥名、街名、村名等专有地点名称，当成模型会自动理解的视觉锚点；如果原文里有这些名字，你必须先把它们翻译成可见的山体走势、屋脊层级、城墙轮廓、飞檐剪影、水岸线、桥体结构、树线和材质关系，名字最多只能当补充。",
		"不要写“X方向”“Y一带”“通往Z的路”“本应是X的位置”“右上方留出某地”这类叙事坐标词；必须改写成画面里真正看得见的远景轮廓、建筑结构、树线、水系和空间层次。",
		"场景骨架必须服从故事时代、地域、文明语境、社会阶层、建筑类型和场合；你要据此推理建筑、家具、器物、照明、景观、地面、门窗、墙面与陈设，避免古代混入现代建筑、电器和城市景观，也避免现代无缘由混入古代建筑和器物。",
		"如果同一集多个 scene 发生在同一地点，必须复用同一组空间骨架关键词，只改镜头、站位、局部光线和前景道具；不要把同一个建筑空间写成完全不同的结构、材质和照明逻辑。",
		"如果 scene 里有背景人物、围观者、陪衬人物或群体人物，你必须让他们的服装、发型、年龄层、阶层感、时代感、地域感和场合一致；不要让背景人物穿出与故事时代、地点、阶层冲突的衣着，也不要让背景人物抢主位。",
		"如果 scene 里有一整组持续可见的背景人物，例如侍卫、宫女、官员、随从、宾客、学生、士兵或围观者，必须给这组人写出统一的组服化锚点，例如共同的主色块、层级、制式、材质或头饰系统，避免后续视频里同组人自由变色。",
		"项目画风是系统内部预设，不传给你；不要返回“风格：”这一行，也不要把任何画风描述写进 image_prompt。",
		"image_prompt 正文必须保持画风中性；不要写“写实、摄影、剧照、插画、动画、动漫、国漫、二次元、3D、CG、渲染、电影级、电影感、胶片感、真实皮肤纹理、真实毛孔、真实体积感”这类画风、媒介、渲染、质感词。",
		"image_prompt 只能写模型能直接看懂的画面内容；不要写“女主、男主、反派、打脸、爽感、反转、权斗、社死、算计、掌控全局、梨花带雨”这类故事标签、抽象判断或文学化修辞，必须改写成可见的表情、姿态、动作、光线、材质和构图；光影行只写客观光源、时段、照射方向、色温和明暗关系。",
		"先提取完整故事，再压缩成适合剧集解说的版本，必须让整集故事整体通畅，不能只抽燃点。",
		"你必须先把剧本还原成完整 story beats，再按导演式 coverage 把 beats 组织成 scene；每个 scene 最好承担一种主要镜头功能：建立、关系、行为、反应、信息揭露或收束。",
		"scenes 要服务“旁白讲故事”，每个 scene 必须有推进、有信息、有情绪，同时保留必要衔接信息。",
		"每个 scene 的 duration_seconds 自行判断，范围 3 到 13 秒，并根据中文旁白自然语气、断点和停顿来决定。你必须先给候选 scene 写出 narration 草稿，再在内部按正常中文解说速度真实朗读这条 narration，之后才能确定时长；不要只按字数平均，年份、数字、地名、人名、官职、专有名词、顿号、引号、转折和情绪留白都会拉长口播时长。",
		"不开口镜头里，长对白、长争执、长信息揭露必须先提炼成解说式精华，再拆到多个 scene；有对白镜头里，则让 dialogue 承担真正需要听见的台词，narration 只保留桥接和结果，不要让两者重复说同一层信息。",
		"narration 必须按镜头时长控制信息量，讲不完就拆分新 scene，优先保证配音自然、剪辑放得下、故事讲得完；如果内部试讲时需要赶语速、吞字、省掉自然停顿、跳过专有名词解释，或一句 narration 同时塞入太多背景说明、动作、结果和反应，说明这个 scene 仍然过满，必须继续拆镜。即使秒数勉强够，只要一句 narration 同时承担两个以上重信息块，也必须继续拆镜，不要把多个关键推进点硬压成一句全包式旁白；只要真实口播推演后会超过当前时长，尤其超过 13 秒或接近 15 秒，就必须继续拆镜。",
		"scene 数量由 LLM 自行推理，必须以“故事讲通”为准，不要预设固定数量。",
		"优先保留推动因果、人物目标变化、关键动作、结果和必要反应，删除冗余原文修辞和重复表达。",
		sceneSegmentationGuidance,
		"首帧图提示词写当前静态画面与动作起点，不要偷懒省略人物锚点、当前镜头状态、动作准备状态和可动环境锚点。",
		"同一地点连续 scene 要保持同一组空间骨架；连续对峙、交谈、阻拦、追问 scene 要尽量保持左右站位、视线方向和动作方向一致。",
		"构图要体现镜头功能：建立镜头更适合交代空间，关系和行为镜头更适合中景或双人构图，反应和信息镜头更适合近景、特写或局部特写。",
		"video_prompt 固定使用英文标签 Style / Phase 1 / Audio / Phase 2 / Audio，只有标签后的正文内容写中文。",
		"video_prompt 是纯正向动作描述；字幕、文字、水印、UI 这类固定规避项由系统工作流负向节点处理，不要写进 Style、Phase 或 Audio。",
		"video_prompt 不要写角色名字；如果镜头里有多人，必须用“左侧中国女性，24岁，黑色长直发”“右侧中国男性，30岁，下巴干净无胡须，深蓝西装”这类位置加明确年龄和可见外观锚点指代谁在动；若需要补年龄阶段，也只能放在明确年龄之后。",
		"video_prompt 也不要把地名、寺名、山名、湖名和方向词当成模型会自动理解的远景锚点；如果需要交代远景或地标，只能写可见轮廓、层级和空间关系，不要写“某山那边”“某寺方向”“某城位置”。",
		"video_prompt 也只能写模型能直接看懂的内容；不要写“女主、男主、反派、二叔、爽感、反转感、修罗场、录音口型声、AI换脸感、黑料感”这类故事标签或抽象组合词。",
		"video_prompt 不要默认写成微动，也不要默认写慢。你必须先根据剧情事件判断自然速度：静观、犹豫和压抑可慢；普通走步、转身、开门、伸手、取物按正常速度；追拦、扑救、甩物、闯入、奔逃、灾变和动物扑冲按正常偏快或明显快速速度写。",
		"video_prompt 可以写镜头语言，但请用模型直接能执行的运动结果来写，例如“镜头顺着人物前冲跟一段”“镜头被震得一晃后立刻找稳”“镜头在人物停步时一起停住”，不要把“建立镜头、关系镜头、收束镜头、信息镜头、回忆感、压迫感、中等强度、微到中微、轻微、极轻、受控”这类内部判断词原样写进最终输出。",
		"每个 phase 应围绕 1 个主导事件组织，可以包含 1 个主导人物动作、1 到 2 个重要联动反应、若干弱背景反应和 1 个主导环境或镜头变化；不要在一个 phase 里塞满多人互不相关的大动作。",
		"video_prompt 的优先级必须固定为：先保证叙事中心人物动作成立，再保证重要联动反应成立，最后再补环境动态；环境动态不能替代人物表演。",
		"除非剧情明确要求人物真正离开画面、追出画面或从画外再入，否则 video_prompt 应尽量让关键人物在当前构图内完成动作，不要把人物无缘由带出画面边界后又重新回到镜头里；双人关系镜头尤其要优先保持两人同框。",
		"若 video_prompt 里存在群体、背景人物或围观者，必须交代他们的主要视线目标与朝向，例如“左右官员同时看向闯入者”“后排士兵朝高台微微转头”“围观者的目光集中到倒下的人身上”；不要只写“众人骚动”。",
		"如果当前镜头属于地震、坍塌、洪水、火势、风暴、群体奔逃、集体惊慌、动物冲刺、器物猛震、桥面颤动、船身大晃等事件级镜头，video_prompt 不能只写“轻微震动”“轻微推近”“轻微摆动”。你必须写清：冲击从哪里来、结构怎样震、尘浪或水体怎样推进、群体或动物怎样被带动、镜头如何被冲击带得短促一晃后重新找稳，而且动作速度必须符合事件本身，不要人为减速。",
		"如果当前镜头主位是动物、牲畜、犬只、马匹、飞鸟、鱼群或其他非人主体，必须把它当成真正叙事中心来写，明确身体动力学，例如四肢步态、重心、颈部转向、耳尾状态、扑跳、冲刺、急停、挣脱、低伏蓄力、扑翅、踏蹄；不要把动物默认写成几乎不动的背景。",
		"如果 scene 里存在可动环境元素，video_prompt 的每个 phase 都必须至少写 1 个环境动态，让雨、烟、火、水、帘、叶、灰尘、蒸汽这类元素继续活着；不要让环境完全静止。",
		"如果有背景人物或重要联动人物，video_prompt 可以写他们的联动反应，但优先写成视线变化、轻微侧头、肩线收紧、袖摆轻晃、呼吸变化或极轻微躬身这类原地反应；不要轻易给背景人物写走步、后撤、前压、转身离位、拱手出列这类会让模型把他们活化成明显移动人物的动作，除非剧情明确推动他们移动。",
		"如果有一整组持续可见的背景人物，例如侍卫、宫女、官员、士兵、随从或围观者，video_prompt 必须默认延续他们在首帧图中的统一服色、层级和制式，不要让同组人物在视频里自由换色或互换内外层；若他们本来属于班列、守位、席位或仪仗系统，还要默认他们双脚不离位，只保留边缘层级里的原地弱反应。",
		"视频提示词要写“动作如何发生和如何停住”，不要写成“动作结果已经完成”；嘴部动作和台词承载必须服从当前说话模式：不开口时只保留情绪性口部变化，开口时只允许当前主说话者承担清晰说话口型，其他人物不要多人抢口型。",
		"Audio 行只承担当前 phase 的声音结果：不开口镜头写环境音；有对白镜头只在单行 Audio 内写“中文台词 + 环境音”的组合，不要把旁白写进 Audio。",
		"不要返回任何解释，只返回 JSON。",
	}

	if hasExistingCharacters {
		extraRequirements = append(extraRequirements, "本次已有角色资产，只能把它们作为锁定资产参与续写和 scene 描述；旧角色不要再次返回到 characters 数组，characters 只返回本集新出现的新角色。")
	} else {
		extraRequirements = append(extraRequirements, "本次没有已有角色资产；请把本集涉及的正式角色完整生成为新角色，不要虚构旧角色来源或历史角色关系。")
	}

	if hasPreviousEpisode {
		extraRequirements = append(extraRequirements, "本次是续集生成，必须严格承接上一集 ending_state、character_status 和 open_threads，不能回到上一集开头重讲。")
	} else {
		extraRequirements = append(extraRequirements, "本次没有上一集结构化记忆；不要虚构 previous_episode_context、上一集结尾状态、历史悬念或既有人物关系。")
	}

	extraRequirements = append(extraRequirements, strings.TrimSpace(speechModeUserRules))

	userPrompt := strings.Join(userSections, "\n\n") + "\n\n额外要求：\n- " + strings.Join(extraRequirements, "\n- ")

	return systemPrompt, userPrompt, nil
}

func buildLightweightStoryPrompts(project models.Project, req models.AutoGenerateRequest, existingCharacters []lightweightStoryCharacter, previousEpisodeContext lightweightStoryEpisodeMemory) (string, string, error) {
	ctx, err := buildLightweightStoryPromptContext(project, req, existingCharacters, previousEpisodeContext)
	if err != nil {
		return "", "", err
	}
	switch normalizeAutoGenerateGenerationMode(req.GenerationMode, req.AllowCharacterSpeech) {
	case AutoGenerateModeStoryboard:
		systemPrompt, userPrompt := buildStoryboardLightweightStoryPrompts(ctx)
		return systemPrompt, userPrompt, nil
	case AutoGenerateModeHighQuality:
		systemPrompt, userPrompt := buildHighQualityLightweightStoryPrompts(ctx)
		return systemPrompt, userPrompt, nil
	default:
		systemPrompt, userPrompt := buildStandardLightweightStoryPrompts(ctx)
		return systemPrompt, userPrompt, nil
	}
}

func applyLightweightStoryContinuation(projectID uint, req models.AutoGenerateRequest, continueFromTaskID string, baseUserPrompt string, provider models.LLMProvider, taskID string) (string, *lightweightStoryPartialContext, error) {
	continueFromTaskID = strings.TrimSpace(continueFromTaskID)
	if continueFromTaskID == "" {
		return baseUserPrompt, nil, nil
	}

	task.GlobalTaskManager.UpdateTaskProgress(taskID, 32, "读取上次中断的实时流内容")
	partialCtx, err := loadLightweightStoryPartialContextChain(continueFromTaskID, "轻量剧情一次性生成")
	if err != nil {
		Log(
			LogLevelWarn,
			llmLogMessage("LLM 续写回退", provider),
			fmt.Sprintf("project=%d episode=%d continue_from_task_id=%s but no reusable stream content found: %v", projectID, req.Episode, continueFromTaskID, err),
		)
		return baseUserPrompt, nil, nil
	}

	task.GlobalTaskManager.UpdateTaskProgress(taskID, 36, "基于已收到内容构造续写请求")
	userPrompt, err := buildLightweightStoryContinuationUserPrompt(baseUserPrompt, partialCtx)
	if err != nil {
		return "", nil, err
	}
	return userPrompt, partialCtx, nil
}

func requestLightweightStoryOnce(provider models.LLMProvider, systemPrompt string, userPrompt string, taskID string) (string, error) {
	model, err := requireProviderModelName(provider)
	if err != nil {
		return "", err
	}

	req := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
	}

	return requestLLMContentStreaming(provider, req, 15*time.Minute, taskID, "轻量剧情一次性生成")
}

type lightweightStoryPartialContext struct {
	TotalScenes int
	Characters  []lightweightStoryCharacter
	Scenes      []lightweightStoryScene
	NextSceneID int
}

func loadLatestTaskLLMStreamContent(taskID string, label string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task id is required")
	}
	query := db.DB.Where("task_id = ?", taskID)
	if strings.TrimSpace(label) != "" {
		query = query.Where("label = ?", strings.TrimSpace(label))
	}
	var stream models.LLMStreamState
	if err := query.Order("updated_at desc").First(&stream).Error; err != nil {
		return "", err
	}
	return strings.TrimSpace(stream.Content), nil
}

func loadTaskContinueFromTaskID(taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", nil
	}

	var record models.Task
	if err := db.DB.Select("payload").First(&record, "id = ?", taskID).Error; err != nil {
		return "", err
	}
	if strings.TrimSpace(record.Payload) == "" {
		return "", nil
	}

	var payload lightweightStoryTaskPayload
	if err := json.Unmarshal([]byte(record.Payload), &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.ContinueFromTaskID), nil
}

func mergeLightweightStoryPartialContexts(base *lightweightStoryPartialContext, next *lightweightStoryPartialContext) *lightweightStoryPartialContext {
	if base == nil {
		if next == nil {
			return nil
		}
		clonedCharacters := append([]lightweightStoryCharacter{}, next.Characters...)
		clonedScenes := append([]lightweightStoryScene{}, next.Scenes...)
		return &lightweightStoryPartialContext{
			TotalScenes: next.TotalScenes,
			Characters:  clonedCharacters,
			Scenes:      clonedScenes,
			NextSceneID: next.NextSceneID,
		}
	}
	if next == nil {
		return base
	}

	charactersByName := make(map[string]lightweightStoryCharacter, len(base.Characters)+len(next.Characters))
	order := make([]string, 0, len(base.Characters)+len(next.Characters))
	for _, char := range base.Characters {
		name := strings.TrimSpace(char.Name)
		if name == "" {
			continue
		}
		charactersByName[name] = char
		order = append(order, name)
	}
	for _, char := range next.Characters {
		name := strings.TrimSpace(char.Name)
		if name == "" {
			continue
		}
		if _, exists := charactersByName[name]; !exists {
			order = append(order, name)
		}
		charactersByName[name] = char
	}
	mergedCharacters := make([]lightweightStoryCharacter, 0, len(order))
	seenNames := make(map[string]struct{}, len(order))
	for _, name := range order {
		if _, exists := seenNames[name]; exists {
			continue
		}
		seenNames[name] = struct{}{}
		mergedCharacters = append(mergedCharacters, charactersByName[name])
	}

	sceneByID := make(map[int]lightweightStoryScene, len(base.Scenes)+len(next.Scenes))
	ids := make([]int, 0, len(base.Scenes)+len(next.Scenes))
	for _, scene := range base.Scenes {
		if scene.SceneID <= 0 {
			continue
		}
		sceneByID[scene.SceneID] = scene
		ids = append(ids, scene.SceneID)
	}
	for _, scene := range next.Scenes {
		if scene.SceneID <= 0 {
			continue
		}
		if _, exists := sceneByID[scene.SceneID]; !exists {
			ids = append(ids, scene.SceneID)
		}
		sceneByID[scene.SceneID] = scene
	}
	sort.Ints(ids)
	mergedScenes := make([]lightweightStoryScene, 0, len(ids))
	seenSceneIDs := make(map[int]struct{}, len(ids))
	maxSceneID := 0
	for _, id := range ids {
		if _, exists := seenSceneIDs[id]; exists {
			continue
		}
		seenSceneIDs[id] = struct{}{}
		mergedScenes = append(mergedScenes, sceneByID[id])
		if id > maxSceneID {
			maxSceneID = id
		}
	}

	totalScenes := next.TotalScenes
	if totalScenes == 0 {
		totalScenes = base.TotalScenes
	}
	if totalScenes == 0 {
		totalScenes = len(mergedScenes)
	}

	return &lightweightStoryPartialContext{
		TotalScenes: totalScenes,
		Characters:  mergedCharacters,
		Scenes:      mergedScenes,
		NextSceneID: maxSceneID + 1,
	}
}

func loadLightweightStoryPartialContextChain(taskID string, label string) (*lightweightStoryPartialContext, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}

	visited := make(map[string]struct{})
	chain := make([]string, 0, 4)
	current := taskID
	for current != "" {
		if _, exists := visited[current]; exists {
			break
		}
		visited[current] = struct{}{}
		chain = append(chain, current)

		nextTaskID, err := loadTaskContinueFromTaskID(current)
		if err != nil {
			break
		}
		current = nextTaskID
	}

	var merged *lightweightStoryPartialContext
	for idx := len(chain) - 1; idx >= 0; idx-- {
		partialRaw, err := loadLatestTaskLLMStreamContent(chain[idx], label)
		if err != nil {
			continue
		}
		partialCtx, err := extractLightweightStoryPartialContext(partialRaw)
		if err != nil {
			continue
		}
		merged = mergeLightweightStoryPartialContexts(merged, partialCtx)
	}
	if merged == nil {
		return nil, fmt.Errorf("no reusable partial characters or scenes found")
	}
	return merged, nil
}

func findJSONValueStartForKey(raw string, key string) int {
	pattern := `"` + strings.TrimSpace(key) + `"`
	idx := strings.Index(raw, pattern)
	if idx < 0 {
		return -1
	}
	i := idx + len(pattern)
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\n' || raw[i] == '\r' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) || raw[i] != ':' {
		return -1
	}
	i++
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\n' || raw[i] == '\r' || raw[i] == '\t') {
		i++
	}
	if i >= len(raw) {
		return -1
	}
	return i
}

func extractPartialIntField(raw string, key string) int {
	start := findJSONValueStartForKey(raw, key)
	if start < 0 {
		return 0
	}
	end := start
	for end < len(raw) && raw[end] >= '0' && raw[end] <= '9' {
		end++
	}
	if end == start {
		return 0
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw[start:end]))
	if err != nil {
		return 0
	}
	return value
}

func extractJSONArrayObjectSlices(raw string, key string) ([]string, bool, error) {
	start := findJSONValueStartForKey(raw, key)
	if start < 0 {
		return nil, false, fmt.Errorf("key %s not found", key)
	}
	if raw[start] != '[' {
		return nil, false, fmt.Errorf("key %s is not an array", key)
	}

	items := []string{}
	inString := false
	escapeNext := false
	objectDepth := 0
	itemStart := -1
	for i := start + 1; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if escapeNext {
				escapeNext = false
				continue
			}
			if ch == '\\' {
				escapeNext = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			if objectDepth == 0 {
				itemStart = i
			}
			objectDepth++
		case '}':
			if objectDepth > 0 {
				objectDepth--
				if objectDepth == 0 && itemStart >= 0 {
					items = append(items, raw[itemStart:i+1])
					itemStart = -1
				}
			}
		case ']':
			if objectDepth == 0 {
				return items, true, nil
			}
		}
	}
	return items, false, nil
}

func extractLightweightStoryPartialContext(raw string) (*lightweightStoryPartialContext, error) {
	cleaned := strings.TrimSpace(cleanupLLMJSON(raw))
	if cleaned == "" {
		cleaned = strings.TrimSpace(raw)
	}
	if cleaned == "" {
		return nil, fmt.Errorf("empty partial response")
	}

	ctx := &lightweightStoryPartialContext{
		TotalScenes: extractPartialIntField(cleaned, "total_scenes"),
		Characters:  []lightweightStoryCharacter{},
		Scenes:      []lightweightStoryScene{},
		NextSceneID: 1,
	}

	if characterSlices, _, err := extractJSONArrayObjectSlices(cleaned, "characters"); err == nil {
		seen := make(map[string]struct{}, len(characterSlices))
		for _, slice := range characterSlices {
			var char lightweightStoryCharacter
			if err := json.Unmarshal([]byte(slice), &char); err != nil {
				continue
			}
			char.Name = strings.TrimSpace(char.Name)
			if char.Name == "" {
				continue
			}
			if _, exists := seen[char.Name]; exists {
				continue
			}
			seen[char.Name] = struct{}{}
			ctx.Characters = append(ctx.Characters, char)
		}
	}

	if sceneSlices, _, err := extractJSONArrayObjectSlices(cleaned, "scenes"); err == nil {
		sceneByID := make(map[int]lightweightStoryScene, len(sceneSlices))
		maxSceneID := 0
		for _, slice := range sceneSlices {
			var scene lightweightStoryScene
			if err := json.Unmarshal([]byte(slice), &scene); err != nil {
				continue
			}
			if scene.SceneID <= 0 {
				continue
			}
			sceneByID[scene.SceneID] = scene
			if scene.SceneID > maxSceneID {
				maxSceneID = scene.SceneID
			}
		}
		if len(sceneByID) > 0 {
			ids := make([]int, 0, len(sceneByID))
			for id := range sceneByID {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			ctx.Scenes = make([]lightweightStoryScene, 0, len(ids))
			for _, id := range ids {
				ctx.Scenes = append(ctx.Scenes, sceneByID[id])
			}
			ctx.NextSceneID = maxSceneID + 1
		}
	}

	if ctx.TotalScenes == 0 && len(ctx.Scenes) > 0 {
		ctx.TotalScenes = len(ctx.Scenes)
	}

	if len(ctx.Characters) == 0 && len(ctx.Scenes) == 0 {
		return nil, fmt.Errorf("no reusable partial characters or scenes found")
	}
	return ctx, nil
}

func buildLightweightStoryContinuationUserPrompt(baseUserPrompt string, partial *lightweightStoryPartialContext) (string, error) {
	if partial == nil {
		return baseUserPrompt, nil
	}

	completedPayload := map[string]interface{}{
		"total_scenes": partial.TotalScenes,
		"characters":   partial.Characters,
		"scenes":       partial.Scenes,
	}
	completedJSON, err := json.MarshalIndent(completedPayload, "", "  ")
	if err != nil {
		return "", err
	}

	instructions := []string{
		baseUserPrompt,
		"续写模式说明：",
	}
	if len(partial.Scenes) > 0 {
		instructions = append(instructions,
			fmt.Sprintf("你上一次已经成功生成并确认保留了 scene_id 1-%d 的内容。不要重复或改写这些已完成的 scenes。", partial.NextSceneID-1),
			fmt.Sprintf("本次只需要从 scene_id %d 开始继续补全剩余 scenes。", partial.NextSceneID),
		)
	} else {
		instructions = append(instructions, "上一次角色定义已经开始生成，但 scenes 尚未完整产出。本次从 scene_id 1 开始继续补全。")
	}
	if partial.TotalScenes > 0 {
		instructions = append(instructions, fmt.Sprintf("total_scenes 必须保持为 %d。", partial.TotalScenes))
	}
	if len(partial.Characters) > 0 {
		instructions = append(instructions, "characters 字段只返回缺失的新角色；如果前面的角色定义已经完整，本次返回 []。")
	} else {
		instructions = append(instructions, "上一次角色定义未完整产出，本次请返回完整 characters 数组。")
	}
	instructions = append(instructions,
		"scenes 字段只返回尚未完成的剩余 scenes，scene_id 必须从续写起点继续递增，不要重复前面已完成 scenes。",
		"episode_memory 字段必须返回完整最终版本。",
		"以下是已经成功收到并保留的历史内容，你必须把它当作最终定稿继续往后写：",
		string(completedJSON),
	)
	return strings.Join(instructions, "\n\n"), nil
}

func mergeLightweightStoryContinuation(partial *lightweightStoryPartialContext, tail *lightweightStoryResponse) *lightweightStoryResponse {
	if partial == nil {
		return tail
	}
	if tail == nil {
		return &lightweightStoryResponse{
			TotalScenes:   partial.TotalScenes,
			Characters:    append([]lightweightStoryCharacter{}, partial.Characters...),
			Scenes:        append([]lightweightStoryScene{}, partial.Scenes...),
			EpisodeMemory: emptyEpisodeMemory(),
		}
	}

	charactersByName := make(map[string]lightweightStoryCharacter, len(partial.Characters)+len(tail.Characters))
	order := make([]string, 0, len(partial.Characters)+len(tail.Characters))
	for _, char := range partial.Characters {
		name := strings.TrimSpace(char.Name)
		if name == "" {
			continue
		}
		charactersByName[name] = char
		order = append(order, name)
	}
	for _, char := range tail.Characters {
		name := strings.TrimSpace(char.Name)
		if name == "" {
			continue
		}
		if _, exists := charactersByName[name]; !exists {
			order = append(order, name)
		}
		charactersByName[name] = char
	}
	mergedCharacters := make([]lightweightStoryCharacter, 0, len(order))
	seenNames := make(map[string]struct{}, len(order))
	for _, name := range order {
		if _, exists := seenNames[name]; exists {
			continue
		}
		seenNames[name] = struct{}{}
		mergedCharacters = append(mergedCharacters, charactersByName[name])
	}

	sceneByID := make(map[int]lightweightStoryScene, len(partial.Scenes)+len(tail.Scenes))
	ids := make([]int, 0, len(partial.Scenes)+len(tail.Scenes))
	for _, scene := range partial.Scenes {
		if scene.SceneID <= 0 {
			continue
		}
		sceneByID[scene.SceneID] = scene
		ids = append(ids, scene.SceneID)
	}
	for _, scene := range tail.Scenes {
		if scene.SceneID <= 0 {
			continue
		}
		if _, exists := sceneByID[scene.SceneID]; !exists {
			ids = append(ids, scene.SceneID)
		}
		sceneByID[scene.SceneID] = scene
	}
	sort.Ints(ids)
	mergedScenes := make([]lightweightStoryScene, 0, len(ids))
	seenSceneIDs := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seenSceneIDs[id]; exists {
			continue
		}
		seenSceneIDs[id] = struct{}{}
		mergedScenes = append(mergedScenes, sceneByID[id])
	}

	totalScenes := tail.TotalScenes
	if totalScenes == 0 {
		totalScenes = partial.TotalScenes
	}
	if totalScenes == 0 {
		totalScenes = len(mergedScenes)
	}

	return &lightweightStoryResponse{
		TotalScenes:   totalScenes,
		Characters:    mergedCharacters,
		Scenes:        mergedScenes,
		EpisodeMemory: tail.EpisodeMemory,
	}
}

func parseStrictLightweightStoryResponse(raw string) (*lightweightStoryResponse, error) {
	trimmed := strings.TrimSpace(cleanupLLMJSON(raw))
	if trimmed == "" {
		trimmed = strings.TrimSpace(raw)
	}
	if trimmed == "" {
		return nil, fmt.Errorf("empty llm response")
	}
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil, fmt.Errorf("llm response must be JSON object only")
	}

	var payload lightweightStoryResponse
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, fmt.Errorf("invalid json: %v", err)
	}

	for i := range payload.Characters {
		payload.Characters[i].Name = strings.TrimSpace(payload.Characters[i].Name)
		payload.Characters[i].Gender = strings.TrimSpace(payload.Characters[i].Gender)
		payload.Characters[i].Age = strings.TrimSpace(payload.Characters[i].Age)
		payload.Characters[i].Height = strings.TrimSpace(payload.Characters[i].Height)
		payload.Characters[i].Era = strings.TrimSpace(payload.Characters[i].Era)
		payload.Characters[i].Country = strings.TrimSpace(payload.Characters[i].Country)
		payload.Characters[i].Appearance = strings.TrimSpace(payload.Characters[i].Appearance)
	}
	for i := range payload.Scenes {
		payload.Scenes[i].Narration = strings.TrimSpace(payload.Scenes[i].Narration)
		payload.Scenes[i].ImagePrompt = strings.TrimSpace(payload.Scenes[i].ImagePrompt)
		payload.Scenes[i].VideoPrompt = strings.TrimSpace(payload.Scenes[i].VideoPrompt)
	}
	payload.EpisodeMemory.StorySummary = strings.TrimSpace(payload.EpisodeMemory.StorySummary)
	payload.EpisodeMemory.EndingState = strings.TrimSpace(payload.EpisodeMemory.EndingState)
	if payload.EpisodeMemory.CharacterStatus == nil {
		payload.EpisodeMemory.CharacterStatus = []lightweightStoryEpisodeCharacterStatus{}
	}
	if payload.EpisodeMemory.OpenThreads == nil {
		payload.EpisodeMemory.OpenThreads = []string{}
	}
	for i := range payload.EpisodeMemory.CharacterStatus {
		payload.EpisodeMemory.CharacterStatus[i].Name = strings.TrimSpace(payload.EpisodeMemory.CharacterStatus[i].Name)
		payload.EpisodeMemory.CharacterStatus[i].Status = strings.TrimSpace(payload.EpisodeMemory.CharacterStatus[i].Status)
	}
	for i := range payload.EpisodeMemory.OpenThreads {
		payload.EpisodeMemory.OpenThreads[i] = strings.TrimSpace(payload.EpisodeMemory.OpenThreads[i])
	}

	return &payload, nil
}

func coerceJSONScalarToString(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}

	var asNumber json.Number
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		return asNumber.String(), nil
	}

	var asBool bool
	if err := json.Unmarshal(raw, &asBool); err == nil {
		if asBool {
			return "true", nil
		}
		return "false", nil
	}

	return "", fmt.Errorf("expected string-compatible scalar")
}

func nonEmptyTrimmedLines(input string) []string {
	rawLines := strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

type structuredVideoPrompt struct {
	Style       string
	Phase1Range string
	Phase1Start float64
	Phase1End   float64
	Phase1Body  string
	Audio1      string
	Phase2Range string
	Phase2Start float64
	Phase2End   float64
	Phase2Body  string
	Audio2      string
}

type flowingVideoPrompt struct {
	Body  string
	Audio string
}

func parsePhaseLine(line string, expectedIndex int) (string, float64, float64, string, error) {
	var timeRange string
	var content string

	englishPrefix := fmt.Sprintf("Phase %d (", expectedIndex)
	if !strings.HasPrefix(line, englishPrefix) || !strings.Contains(line, "):") {
		return "", 0, 0, "", fmt.Errorf("video_prompt phase %d line must use Phase %d (x-y): format", expectedIndex, expectedIndex)
	}
	rest := strings.TrimPrefix(line, englishPrefix)
	parts := strings.SplitN(rest, "):", 2)
	if len(parts) != 2 {
		return "", 0, 0, "", fmt.Errorf("video_prompt phase %d line is invalid", expectedIndex)
	}
	timeRange = strings.TrimSpace(parts[0])
	content = strings.TrimSpace(parts[1])
	if content == "" {
		return "", 0, 0, "", fmt.Errorf("video_prompt phase %d content must not be empty", expectedIndex)
	}
	rangeParts := strings.SplitN(timeRange, "-", 2)
	if len(rangeParts) != 2 {
		return "", 0, 0, "", fmt.Errorf("video_prompt phase %d time range must be start-end seconds", expectedIndex)
	}
	start, err := strconv.ParseFloat(strings.TrimSpace(rangeParts[0]), 64)
	if err != nil {
		return "", 0, 0, "", fmt.Errorf("video_prompt phase %d start second is invalid", expectedIndex)
	}
	end, err := strconv.ParseFloat(strings.TrimSpace(rangeParts[1]), 64)
	if err != nil {
		return "", 0, 0, "", fmt.Errorf("video_prompt phase %d end second is invalid", expectedIndex)
	}
	if end <= start {
		return "", 0, 0, "", fmt.Errorf("video_prompt phase %d end second must be greater than start second", expectedIndex)
	}
	return timeRange, start, end, content, nil
}

func parseStructuredVideoPrompt(prompt string) (*structuredVideoPrompt, error) {
	lines := nonEmptyTrimmedLines(prompt)
	if len(lines) != 5 {
		return nil, fmt.Errorf("video_prompt must contain exactly 5 non-empty lines")
	}

	requiredPrefixes := []string{
		"Style:",
		"Phase 1 (",
		"Audio:",
		"Phase 2 (",
		"Audio:",
	}
	for idx, prefix := range requiredPrefixes {
		if !strings.HasPrefix(lines[idx], prefix) {
			return nil, fmt.Errorf("video_prompt line %d must use fixed Style/Phase/Audio prefixes", idx+1)
		}
	}

	style := strings.TrimSpace(strings.TrimPrefix(lines[0], "Style:"))
	if style == "" {
		return nil, fmt.Errorf("video_prompt style must not be empty")
	}
	phase1Range, phase1Start, phase1End, phase1Body, err := parsePhaseLine(lines[1], 1)
	if err != nil {
		return nil, err
	}
	audio1 := strings.TrimSpace(strings.TrimPrefix(lines[2], "Audio:"))
	if audio1 == "" {
		return nil, fmt.Errorf("video_prompt audio 1 must not be empty")
	}
	phase2Range, phase2Start, phase2End, phase2Body, err := parsePhaseLine(lines[3], 2)
	if err != nil {
		return nil, err
	}
	audio2 := strings.TrimSpace(strings.TrimPrefix(lines[4], "Audio:"))
	if audio2 == "" {
		return nil, fmt.Errorf("video_prompt audio 2 must not be empty")
	}

	return &structuredVideoPrompt{
		Style:       style,
		Phase1Range: phase1Range,
		Phase1Start: phase1Start,
		Phase1End:   phase1End,
		Phase1Body:  phase1Body,
		Audio1:      audio1,
		Phase2Range: phase2Range,
		Phase2Start: phase2Start,
		Phase2End:   phase2End,
		Phase2Body:  phase2Body,
		Audio2:      audio2,
	}, nil
}

func parseFlowingVideoPrompt(prompt string) (*flowingVideoPrompt, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(prompt, "\r\n", "\n"))
	if trimmed == "" {
		return nil, fmt.Errorf("video_prompt must not be empty")
	}

	lines := nonEmptyTrimmedLines(trimmed)
	if len(lines) == 0 {
		return nil, fmt.Errorf("video_prompt must not be empty")
	}

	audioPrefixes := []string{"Audio:", "Audio：", "音效：", "音效:", "声音：", "声音:"}
	audioLineIndex := -1
	audioPrefix := ""
	for idx, line := range lines {
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "style:") || strings.HasPrefix(lower, "style：") ||
			strings.HasPrefix(lower, "phase ") || strings.HasPrefix(lower, "phase:") || strings.HasPrefix(lower, "phase：") ||
			(strings.HasPrefix(lower, "audio:") || strings.HasPrefix(lower, "audio：")) {
			// handled below; only the final line may be the Audio section in flowing mode
		}
		for _, prefix := range audioPrefixes {
			if strings.HasPrefix(line, prefix) {
				audioLineIndex = idx
				audioPrefix = prefix
				break
			}
		}
	}
	if audioLineIndex < 0 {
		return nil, fmt.Errorf("video_prompt must include a final Audio: section in flowing mode")
	}
	if audioLineIndex != len(lines)-1 {
		return nil, fmt.Errorf("video_prompt Audio section must be the final non-empty line in flowing mode")
	}
	for _, line := range lines[:audioLineIndex] {
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "style:") || strings.HasPrefix(lower, "style：") ||
			strings.HasPrefix(lower, "phase ") || strings.HasPrefix(lower, "phase:") || strings.HasPrefix(lower, "phase：") {
			return nil, fmt.Errorf("video_prompt must use continuous narrative form instead of Style/Phase labels in flowing mode")
		}
		for _, prefix := range audioPrefixes {
			if strings.HasPrefix(line, prefix) {
				return nil, fmt.Errorf("video_prompt Audio section must only appear once and stay at the end in flowing mode")
			}
		}
	}
	body := strings.Join(lines[:audioLineIndex], " ")
	audio := strings.TrimSpace(strings.TrimPrefix(lines[audioLineIndex], audioPrefix))
	if body == "" {
		return nil, fmt.Errorf("video_prompt narrative body must not be empty in flowing mode")
	}
	if audio == "" {
		return nil, fmt.Errorf("video_prompt Audio section must not be empty in flowing mode")
	}

	return &flowingVideoPrompt{
		Body:  strings.Join(nonEmptyTrimmedLines(body), " "),
		Audio: strings.Join(nonEmptyTrimmedLines(audio), " "),
	}, nil
}

func validateLightweightStoryResponse(payload *lightweightStoryResponse, existingCharacters []lightweightStoryCharacter, generationMode string) error {
	_ = normalizeAutoGenerateGenerationMode(generationMode, false)
	if payload == nil {
		return fmt.Errorf("story payload is nil")
	}
	if len(payload.Scenes) == 0 {
		return fmt.Errorf("scenes array must not be empty")
	}

	existingByName := make(map[string]lightweightStoryCharacter, len(existingCharacters))
	for _, char := range existingCharacters {
		existingByName[char.Name] = char
	}

	outputNames := make(map[string]lightweightStoryCharacter, len(payload.Characters))
	for _, char := range payload.Characters {
		if char.Name == "" {
			return fmt.Errorf("character name is required")
		}
		if _, exists := outputNames[char.Name]; exists {
			return fmt.Errorf("duplicate character name: %s", char.Name)
		}
		if _, exists := existingByName[char.Name]; exists {
			return fmt.Errorf("existing character %s must not be returned in characters; only return newly appeared characters", char.Name)
		}

		outputNames[char.Name] = char
	}

	sceneIDs := make(map[int]struct{}, len(payload.Scenes))
	expectedSceneID := 1
	for _, scene := range payload.Scenes {
		if scene.SceneID <= 0 {
			return fmt.Errorf("scene_id must be greater than 0")
		}
		if _, exists := sceneIDs[scene.SceneID]; exists {
			return fmt.Errorf("duplicate scene_id: %d", scene.SceneID)
		}
		sceneIDs[scene.SceneID] = struct{}{}
		expectedSceneID++

		if scene.DurationSeconds <= 0 {
			return fmt.Errorf("scene %d duration_seconds must be greater than 0", scene.SceneID)
		}

		if scene.ImagePrompt == "" {
			return fmt.Errorf("scene %d image_prompt is required", scene.SceneID)
		}
		if strings.TrimSpace(scene.VideoPrompt) == "" {
			return fmt.Errorf("scene %d video_prompt is required", scene.SceneID)
		}
	}

	return nil
}

func inferAllowCharacterSpeechFromPayload(payload *lightweightStoryResponse) bool {
	return autoGenerateModeAllowsCharacterSpeech(inferGenerationModeFromPayload(payload))
}

func buildLegacyVideoFingerprintFromPrompt(videoPrompt string, durationSeconds int) (string, error) {
	if durationSeconds <= 0 {
		return "", fmt.Errorf("duration_seconds must be greater than 0")
	}

	var payload VideoFingerprintPayload
	if parsed, err := parseStructuredVideoPrompt(videoPrompt); err == nil {
		payload = VideoFingerprintPayload{
			RecommendedFPS:       24,
			TotalDurationSeconds: durationSeconds,
			PromptPosZH:          strings.TrimSpace(videoPrompt),
			PromptNegZH:          "",
			PromptNegEN:          getFixedLTXVideoNegativePromptEN(),
			StyleZH:              parsed.Style,
			PlayerDescZH:         strings.TrimSpace(parsed.Phase1Body + "；" + parsed.Phase2Body),
			PhasesZH: []VideoFingerprintPhase{
				{
					Index:     1,
					TimeRange: parsed.Phase1Range + "秒",
					Content:   parsed.Phase1Body,
					Audio:     parsed.Audio1,
				},
				{
					Index:     2,
					TimeRange: parsed.Phase2Range + "秒",
					Content:   parsed.Phase2Body,
					Audio:     parsed.Audio2,
				},
			},
		}
	} else if flowing, flowingErr := parseFlowingVideoPrompt(videoPrompt); flowingErr == nil {
		timeRange := fmt.Sprintf("0.0-%.1f秒", float64(durationSeconds))
		payload = VideoFingerprintPayload{
			RecommendedFPS:       24,
			TotalDurationSeconds: durationSeconds,
			PromptPosZH:          strings.TrimSpace(videoPrompt),
			PromptNegZH:          "",
			PromptNegEN:          getFixedLTXVideoNegativePromptEN(),
			StyleZH:              "连续叙事式镜头描述",
			PlayerDescZH:         flowing.Body,
			PhasesZH: []VideoFingerprintPhase{
				{
					Index:     1,
					TimeRange: timeRange,
					Content:   flowing.Body,
					Audio:     flowing.Audio,
				},
			},
		}
	} else {
		timeRange := fmt.Sprintf("0.0-%.1f秒", float64(durationSeconds))
		rawPrompt := strings.TrimSpace(videoPrompt)
		payload = VideoFingerprintPayload{
			RecommendedFPS:       24,
			TotalDurationSeconds: durationSeconds,
			PromptPosZH:          rawPrompt,
			PromptNegZH:          "",
			PromptNegEN:          getFixedLTXVideoNegativePromptEN(),
			StyleZH:              "原始镜头提示词",
			PlayerDescZH:         rawPrompt,
			PhasesZH: []VideoFingerprintPhase{
				{
					Index:     1,
					TimeRange: timeRange,
					Content:   rawPrompt,
					Audio:     "",
				},
			},
		}
	}

	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func saveEpisodeMemoryTx(tx *gorm.DB, projectID uint, episode int, memory lightweightStoryEpisodeMemory) error {
	characterStatusJSON, err := json.Marshal(memory.CharacterStatus)
	if err != nil {
		return err
	}
	openThreadsJSON, err := json.Marshal(memory.OpenThreads)
	if err != nil {
		return err
	}

	var record models.EpisodeMemory
	err = tx.Where("project_id = ? AND episode = ?", projectID, episode).First(&record).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if err == gorm.ErrRecordNotFound {
		record = models.EpisodeMemory{
			ProjectID: projectID,
			Episode:   episode,
			CreatedAt: time.Now(),
		}
	}

	record.StorySummary = strings.TrimSpace(memory.StorySummary)
	record.EndingState = strings.TrimSpace(memory.EndingState)
	record.CharacterStatusJSON = string(characterStatusJSON)
	record.OpenThreadsJSON = string(openThreadsJSON)
	record.UpdatedAt = time.Now()
	return tx.Save(&record).Error
}

func persistLightweightStoryPayload(projectID uint, req models.AutoGenerateRequest, payload *lightweightStoryResponse, existingCharacterRecords []models.Character) error {
	existingByName := make(map[string]models.Character, len(existingCharacterRecords))
	for _, record := range existingCharacterRecords {
		if name := strings.TrimSpace(record.Name); name != "" {
			existingByName[name] = record
		}
	}

	sort.Slice(payload.Scenes, func(i, j int) bool {
		return payload.Scenes[i].SceneID < payload.Scenes[j].SceneID
	})

	return db.DB.Transaction(func(tx *gorm.DB) error {
		for _, character := range payload.Characters {
			if _, exists := existingByName[character.Name]; exists {
				continue
			}

			record := models.Character{
				ProjectID:        projectID,
				Name:             character.Name,
				Gender:           character.Gender,
				Age:              character.Age,
				BodyHeight:       character.Height,
				Era:              character.Era,
				Country:          character.Country,
				Appearance:       character.Appearance,
				IsLocked:         true,
				Description:      character.Appearance,
				FaceFingerprint:  "",
				Fingerprint:      "",
				PositivePrompt:   "",
				NegativePrompt:   "",
				Width:            0,
				Height:           0,
				Seed:             0,
				OptimizeClothing: false,
				RefImage:         "",
				UseRefImage:      false,
				Status:           "draft",
				GeneratedImage:   "",
				CreatedAt:        time.Now(),
				UpdatedAt:        time.Now(),
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
		}

		for _, scene := range payload.Scenes {
			videoFingerprint, err := buildLegacyVideoFingerprintFromPrompt(scene.VideoPrompt, scene.DurationSeconds)
			if err != nil {
				return err
			}

			record := models.Shot{
				ProjectID:              projectID,
				Episode:                req.Episode,
				SceneID:                scene.SceneID,
				SceneNumber:            scene.SceneID,
				DurationSeconds:        scene.DurationSeconds,
				Name:                   "",
				LocationID:             "",
				OutlineRef:             "",
				SceneGoal:              "",
				CharacterBlocking:      "",
				CameraLock:             "",
				Description:            "",
				ImagePrompt:            scene.ImagePrompt,
				VideoPrompt:            scene.VideoPrompt,
				Narration:              scene.Narration,
				BackgroundAudio:        "",
				Dialogue:               "",
				VideoFingerprint:       videoFingerprint,
				ImagePositivePrompt:    marshalLocalizedPromptText(scene.ImagePrompt, ""),
				ImageNegativePrompt:    "",
				ImageWidth:             0,
				ImageHeight:            0,
				ImageSeed:              0,
				ImageStatus:            "draft",
				GeneratedImage:         "",
				ImageGeneratedWorkflow: "",
				VideoPositivePrompt:    "",
				VideoNegativePrompt:    getFixedLTXVideoNegativePromptEN(),
				VideoWidth:             0,
				VideoHeight:            0,
				VideoSeed:              0,
				VideoStatus:            "draft",
				GeneratedVideo:         "",
				VideoGeneratedWorkflow: "",
				CreatedAt:              time.Now(),
				UpdatedAt:              time.Now(),
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
		}

		if err := saveEpisodeMemoryTx(tx, projectID, req.Episode, payload.EpisodeMemory); err != nil {
			return err
		}

		return nil
	})
}

func setAutoGenerateDraftStage(projectID uint, req models.AutoGenerateRequest, stage string, charactersJSON string, scenesJSON string, episodeMemoryJSON string, completedScenes int, lastError string) error {
	if projectID == 0 {
		return nil
	}

	var draft models.AutoGenerateDraft
	err := db.DB.Where("project_id = ?", projectID).First(&draft).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	if err == gorm.ErrRecordNotFound {
		draft = models.AutoGenerateDraft{
			ProjectID: projectID,
			CreatedAt: time.Now(),
		}
	}

	projectEmpty, inspectErr := isProjectContentEmpty(projectID)
	if inspectErr != nil {
		projectEmpty = false
	}

	draft.Plot = strings.TrimSpace(req.Plot)
	draft.Episode = req.Episode
	draft.GenerationMode = normalizeAutoGenerateGenerationMode(req.GenerationMode, req.AllowCharacterSpeech)
	draft.TagIDsJSON = encodeAutoGenerateTagIDs(req.TagIDs)
	draft.AllowCharacterSpeech = autoGenerateModeAllowsCharacterSpeech(draft.GenerationMode)
	draft.SingleMode = false
	draft.Mode = resolveAutoGenerateMode(projectEmpty)
	draft.Stage = strings.TrimSpace(stage)
	draft.CharactersJSON = strings.TrimSpace(charactersJSON)
	draft.OutlineJSON = ""
	draft.ScenesJSON = strings.TrimSpace(scenesJSON)
	draft.EpisodeMemoryJSON = strings.TrimSpace(episodeMemoryJSON)
	draft.CompletedScenes = completedScenes
	draft.LastError = strings.TrimSpace(lastError)
	draft.UpdatedAt = time.Now()

	return db.DB.Save(&draft).Error
}

func setAutoGenerateDraftCurrentTaskID(projectID uint, taskID string) error {
	if projectID == 0 {
		return nil
	}
	return db.DB.Model(&models.AutoGenerateDraft{}).
		Where("project_id = ?", projectID).
		Updates(map[string]interface{}{
			"current_task_id": strings.TrimSpace(taskID),
			"updated_at":      time.Now(),
		}).Error
}

func runLightweightStoryGeneration(projectID uint, req models.AutoGenerateRequest, taskID string, continueFromTaskID string) (*lightweightStoryResponse, error) {
	if projectID == 0 {
		return nil, fmt.Errorf("invalid project id")
	}
	if strings.TrimSpace(req.Plot) == "" {
		return nil, fmt.Errorf("plot is required")
	}
	if req.Episode <= 0 {
		return nil, fmt.Errorf("episode must be greater than 0")
	}

	task.GlobalTaskManager.UpdateTaskProgress(taskID, 5, "检查项目与集数状态")

	var project models.Project
	if err := db.DB.Preload("ArtStyle").First(&project, projectID).Error; err != nil {
		return nil, fmt.Errorf("project not found")
	}

	var existingEpisodeScenes int64
	if err := db.DB.Model(&models.Scene{}).Where("project_id = ? AND episode = ?", projectID, req.Episode).Count(&existingEpisodeScenes).Error; err != nil {
		return nil, err
	}
	if existingEpisodeScenes > 0 {
		return nil, fmt.Errorf("episode %d already has scenes; please delete or reset the episode before regenerating", req.Episode)
	}

	existingCharacterRecords, existingCharacters, err := loadExistingStoryCharacters(projectID)
	if err != nil {
		return nil, err
	}
	previousEpisodeContext, err := loadPreviousEpisodeContext(projectID, req.Episode)
	if err != nil {
		return nil, err
	}

	if err := setAutoGenerateDraftStage(projectID, req, "generating", "", "", "", 0, ""); err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(taskID, 15, "读取提示词知识库并构造请求")

	systemPrompt, userPrompt, err := buildLightweightStoryPrompts(project, req, existingCharacters, previousEpisodeContext)
	if err != nil {
		return nil, err
	}

	var provider models.LLMProvider
	if err := db.DB.Where("is_active = ?", true).First(&provider).Error; err != nil {
		return nil, fmt.Errorf("no active LLM provider found")
	}

	userPrompt, continuationPartial, err := applyLightweightStoryContinuation(projectID, req, continueFromTaskID, userPrompt, provider, taskID)
	if err != nil {
		return nil, err
	}

	Log(
		LogLevelInfo,
		llmLogMessage("LLM Request", provider),
		fmt.Sprintf("Starting lightweight story generation for project=%d episode=%d", projectID, req.Episode),
	)
	Log(
		LogLevelInfo,
		llmLogMessage("LLM Request Prompt", provider),
		fmt.Sprintf("System:\n%s\n\nUser:\n%s", systemPrompt, userPrompt),
	)

	task.GlobalTaskManager.UpdateTaskProgress(taskID, 40, "正在调用 LLM 一次性生成角色与镜头")

	raw, err := requestLightweightStoryOnce(provider, systemPrompt, userPrompt, taskID)
	if err != nil {
		Log(
			LogLevelError,
			llmLogMessage("LLM Error", provider),
			fmt.Sprintf("Lightweight story generation failed: %v", err),
		)
		return nil, err
	}

	Log(
		LogLevelInfo,
		llmLogMessage("LLM 完整返回(轻量剧情一次性生成)", provider),
		raw,
	)

	task.GlobalTaskManager.UpdateTaskProgress(taskID, 68, "校验 LLM 返回结构")

	payload, err := parseStrictLightweightStoryResponse(raw)
	if err != nil {
		Log(
			LogLevelError,
			llmLogMessage("LLM 返回解析失败(轻量剧情一次性生成)", provider),
			err.Error(),
		)
		return nil, err
	}
	if continuationPartial != nil {
		payload = mergeLightweightStoryContinuation(continuationPartial, payload)
	}
	if err := validateLightweightStoryResponse(payload, existingCharacters, req.GenerationMode); err != nil {
		Log(
			LogLevelError,
			llmLogMessage("LLM 返回校验失败(轻量剧情一次性生成)", provider),
			err.Error(),
		)
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(taskID, 82, "写入角色与镜头数据")

	err = persistLightweightStoryPayload(projectID, req, payload, existingCharacterRecords)
	if err != nil {
		return nil, err
	}

	charactersJSON, err := json.MarshalIndent(payload.Characters, "", "  ")
	if err != nil {
		return nil, err
	}
	scenesJSON, err := json.MarshalIndent(map[string]interface{}{
		"total_scenes": payload.TotalScenes,
		"scenes":       payload.Scenes,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	episodeMemoryJSON, err := json.MarshalIndent(payload.EpisodeMemory, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := setAutoGenerateDraftStage(projectID, req, "completed", string(charactersJSON), string(scenesJSON), string(episodeMemoryJSON), payload.TotalScenes, ""); err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(taskID, 100, "角色与镜头已入库")
	return payload, nil
}
