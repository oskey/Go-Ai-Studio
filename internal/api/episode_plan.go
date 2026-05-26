package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"kt-ai-studio/internal/models"
)

type EpisodePlanLocationWindowAnchor struct {
	Position      string   `json:"position"`
	OutsideView   string   `json:"outside_view"`
	HeightLogic   string   `json:"height_logic"`
	MustNotAppear []string `json:"must_not_appear"`
}

type EpisodePlanLocationVisualAnchor struct {
	Walls    string                          `json:"walls"`
	Floor    string                          `json:"floor"`
	Table    string                          `json:"table"`
	Window   EpisodePlanLocationWindowAnchor `json:"window"`
	Lighting string                          `json:"lighting"`
}

type EpisodePlanLocationAsset struct {
	LocationID   string                          `json:"location_id"`
	LocationName string                          `json:"location_name"`
	SpaceType    string                          `json:"space_type"`
	VisualAnchor EpisodePlanLocationVisualAnchor `json:"visual_anchor"`
	Constraints  []string                        `json:"constraints"`
}

type EpisodePlanPropVisualAnchor struct {
	Category  string `json:"category"`
	Material  string `json:"material"`
	Color     string `json:"color"`
	Shape     string `json:"shape"`
	Placement string `json:"placement"`
}

type EpisodePlanPropAsset struct {
	PropID       string                      `json:"prop_id"`
	PropName     string                      `json:"prop_name"`
	LocationID   string                      `json:"location_id"`
	SpaceRole    string                      `json:"space_role"`
	VisualAnchor EpisodePlanPropVisualAnchor `json:"visual_anchor"`
	Constraints  []string                    `json:"constraints"`
}

type EpisodePlanBlockingRule struct {
	Anchor   string `json:"anchor"`
	Facing   string `json:"facing"`
	Relation string `json:"relation"`
}

type EpisodePlanCameraRule struct {
	AxisLocked      bool     `json:"axis_locked"`
	SpatialRelation string   `json:"spatial_relation"`
	Rules           []string `json:"rules"`
}

type EpisodePlanSceneOutlineItem struct {
	SceneNumber           int      `json:"scene_number"`
	SceneName             string   `json:"scene_name"`
	LocationID            string   `json:"location_id"`
	SceneGoal             string   `json:"scene_goal"`
	Characters            []string `json:"characters"`
	BlockingPlan          string   `json:"blocking_plan"`
	PropContinuity        []string `json:"prop_continuity"`
	DialogueLines         []string `json:"dialogue_lines"`
	MustUseLocation       bool     `json:"must_use_location"`
	MustNotCreateNewSpace bool     `json:"must_not_create_new_space"`
}

type EpisodePlanContinuityMemory struct {
	SpaceRules    []string `json:"space_rules"`
	PropRules     []string `json:"prop_rules"`
	BlockingRules []string `json:"blocking_rules"`
	CameraRules   []string `json:"camera_rules"`
}

type EpisodePlanResponse struct {
	EpisodeID        int                                           `json:"episode_id"`
	Characters       []GeneratedCharacter                          `json:"characters"`
	LocationAssets   []EpisodePlanLocationAsset                    `json:"location_assets"`
	PropAssets       []EpisodePlanPropAsset                        `json:"prop_assets"`
	BlockingRules    map[string]map[string]EpisodePlanBlockingRule `json:"blocking_rules"`
	CameraRules      map[string]EpisodePlanCameraRule              `json:"camera_rules"`
	SceneOutline     []EpisodePlanSceneOutlineItem                 `json:"scene_outline"`
	ContinuityMemory EpisodePlanContinuityMemory                   `json:"continuity_memory"`
}

var unstableDecorBackgroundPropTerms = []string{
	"香炉", "线香", "香薰", "香薰机", "蜡烛", "烛台", "花瓶", "摆件", "装饰盘", "装饰托盘", "盆栽", "绿植",
}

var vagueDecorPlacementTerms = []string{
	"角落", "一旁", "旁边", "附近", "边上", "靠边", "一侧", "旁侧", "周围",
}

var firstFrameSequentialActionMarkers = []string{
	"动作链", "动作结果", "作为动作结果", "连续动作", "依次", "先后",
}

var firstFrameArrivalStateMarkers = []string{
	"门口出现", "移门滑开", "推门", "进门", "进入", "走近", "走到", "来到", "到车门边", "拉开车门", "开门",
}

var firstFrameSettledStateMarkers = []string{
	"对坐", "入座", "坐回", "坐定", "落座", "已坐", "手边菜单", "手边茶杯", "桌面原位",
}

var balancedDialogueCoverageMarkers = []string{
	"对坐结构", "双人对坐", "同轴双人", "two-shot", "双方同框", "两人同框均衡",
}

var focusedSingleCoverageMarkers = []string{
	"speaker-led single", "reverse single", "反打", "over-the-shoulder", "过肩", "单人主位", "单人镜头", "听位反应", "dirty frame", "边缘虚化", "边缘弱存在",
}

var focusIsolationMarkers = []string{
	"浅景深", "背景虚化", "前景虚化", "陪体虚化", "边缘虚化", "焦点落在说话者", "焦点只在说话者", "说话者清晰", "非说话者虚化", "background blur",
}

var weakPresenceMarkers = []string{
	"弱存在", "边缘弱存在", "边缘存在", "前景弱存在", "陪体", "听位轮廓", "模糊边缘", "边缘模糊",
}

var visibleAnchorMarkers = []string{
	"后脑", "肩线", "肩背", "上半身", "半身", "轮廓", "剪影", "手部", "手臂", "小腿", "脚尖", "床边轮廓", "桌边轮廓",
	"机身", "主体", "前面板", "顶盖", "灯带", "网孔", "底座圈", "设备主体",
}

var coverageSignatureGroups = []struct {
	Signature string
	Markers   []string
}{
	{Signature: "insert", Markers: []string{"insert", "特写", "设备特写", "手部特写", "道具特写", "POV", "主观视角"}},
	{Signature: "ots", Markers: []string{"over-the-shoulder", "过肩", "dirty single", "dirty frame", "脏边框"}},
	{Signature: "reaction", Markers: []string{"listener reaction", "听位反应", "闭口听位", "读后反应"}},
	{Signature: "two-shot", Markers: []string{"two-shot", "master", "双人对坐", "双方同框", "两人同框均衡"}},
	{Signature: "single", Markers: []string{"speaker-led single", "reverse single", "单人主位", "单人镜头", "主位单人"}},
}

var readingSourceMarkers = []string{
	"平板", "平板电脑", "邮件", "邮箱", "收件箱", "消息", "短信", "聊天记录", "通知", "文件", "文档", "合同", "信件", "纸条", "屏幕内容", "屏幕界面",
}

var readingCoverageMarkers = []string{
	"insert", "设备特写", "读屏特写", "过肩读屏", "主观视角", "POV", "reader reaction", "阅读主位", "读后反应", "第一视角", "低声自语", "轻声念出", "压低嗓音自语", "默读后轻声",
}

const dialogueCharsPerSecond = 5.2
const maxDialogueChunkNaturalSeconds = 5.8
const preferredDialogueSceneNaturalSeconds = 8.2
const maxDialogueChunkRuneCount = 32

func trimEpisodePlanStringSlice(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func normalizeEpisodePlanResponse(plan *EpisodePlanResponse) *EpisodePlanResponse {
	if plan == nil {
		return nil
	}
	for idx := range plan.Characters {
		plan.Characters[idx].Name = strings.TrimSpace(plan.Characters[idx].Name)
		plan.Characters[idx].Gender = strings.TrimSpace(plan.Characters[idx].Gender)
		plan.Characters[idx].FaceFingerprint = strings.TrimSpace(plan.Characters[idx].FaceFingerprint)
		plan.Characters[idx].Description = strings.TrimSpace(plan.Characters[idx].Description)
		plan.Characters[idx].Fingerprint = strings.TrimSpace(plan.Characters[idx].Fingerprint)
		plan.Characters[idx].PromptPosZH = strings.TrimSpace(plan.Characters[idx].PromptPosZH)
		plan.Characters[idx].PromptNegZH = strings.TrimSpace(plan.Characters[idx].PromptNegZH)
	}
	for idx := range plan.LocationAssets {
		asset := &plan.LocationAssets[idx]
		asset.LocationID = strings.TrimSpace(asset.LocationID)
		asset.LocationName = strings.TrimSpace(asset.LocationName)
		asset.SpaceType = strings.TrimSpace(asset.SpaceType)
		asset.VisualAnchor.Walls = strings.TrimSpace(asset.VisualAnchor.Walls)
		asset.VisualAnchor.Floor = strings.TrimSpace(asset.VisualAnchor.Floor)
		asset.VisualAnchor.Table = strings.TrimSpace(asset.VisualAnchor.Table)
		asset.VisualAnchor.Lighting = strings.TrimSpace(asset.VisualAnchor.Lighting)
		asset.VisualAnchor.Window.Position = strings.TrimSpace(asset.VisualAnchor.Window.Position)
		asset.VisualAnchor.Window.OutsideView = strings.TrimSpace(asset.VisualAnchor.Window.OutsideView)
		asset.VisualAnchor.Window.HeightLogic = strings.TrimSpace(asset.VisualAnchor.Window.HeightLogic)
		asset.VisualAnchor.Window.MustNotAppear = trimEpisodePlanStringSlice(asset.VisualAnchor.Window.MustNotAppear)
		asset.Constraints = trimEpisodePlanStringSlice(asset.Constraints)
	}
	for idx := range plan.PropAssets {
		asset := &plan.PropAssets[idx]
		asset.PropID = strings.TrimSpace(asset.PropID)
		asset.PropName = strings.TrimSpace(asset.PropName)
		asset.LocationID = strings.TrimSpace(asset.LocationID)
		asset.SpaceRole = strings.TrimSpace(asset.SpaceRole)
		asset.VisualAnchor.Category = strings.TrimSpace(asset.VisualAnchor.Category)
		asset.VisualAnchor.Material = strings.TrimSpace(asset.VisualAnchor.Material)
		asset.VisualAnchor.Color = strings.TrimSpace(asset.VisualAnchor.Color)
		asset.VisualAnchor.Shape = strings.TrimSpace(asset.VisualAnchor.Shape)
		asset.VisualAnchor.Placement = strings.TrimSpace(asset.VisualAnchor.Placement)
		asset.Constraints = trimEpisodePlanStringSlice(asset.Constraints)
	}
	for locationID, rules := range plan.BlockingRules {
		if rules == nil {
			continue
		}
		trimmedLocationID := strings.TrimSpace(locationID)
		if trimmedLocationID != locationID {
			delete(plan.BlockingRules, locationID)
			plan.BlockingRules[trimmedLocationID] = rules
			locationID = trimmedLocationID
		}
		for characterName, rule := range rules {
			trimmedCharacterName := strings.TrimSpace(characterName)
			if trimmedCharacterName != characterName {
				delete(rules, characterName)
			}
			rule.Anchor = strings.TrimSpace(rule.Anchor)
			rule.Facing = strings.TrimSpace(rule.Facing)
			rule.Relation = strings.TrimSpace(rule.Relation)
			rules[trimmedCharacterName] = rule
		}
	}
	for locationID, rule := range plan.CameraRules {
		trimmedLocationID := strings.TrimSpace(locationID)
		if trimmedLocationID != locationID {
			delete(plan.CameraRules, locationID)
		}
		rule.SpatialRelation = strings.TrimSpace(rule.SpatialRelation)
		rule.Rules = trimEpisodePlanStringSlice(rule.Rules)
		plan.CameraRules[trimmedLocationID] = rule
	}
	for idx := range plan.SceneOutline {
		item := &plan.SceneOutline[idx]
		item.SceneName = strings.TrimSpace(item.SceneName)
		item.LocationID = strings.TrimSpace(item.LocationID)
		item.SceneGoal = strings.TrimSpace(item.SceneGoal)
		item.BlockingPlan = strings.TrimSpace(item.BlockingPlan)
		item.Characters = trimEpisodePlanStringSlice(item.Characters)
		item.PropContinuity = trimEpisodePlanStringSlice(item.PropContinuity)
		item.DialogueLines = trimEpisodePlanStringSlice(item.DialogueLines)
	}
	plan.ContinuityMemory.SpaceRules = trimEpisodePlanStringSlice(plan.ContinuityMemory.SpaceRules)
	plan.ContinuityMemory.PropRules = trimEpisodePlanStringSlice(plan.ContinuityMemory.PropRules)
	plan.ContinuityMemory.BlockingRules = trimEpisodePlanStringSlice(plan.ContinuityMemory.BlockingRules)
	plan.ContinuityMemory.CameraRules = trimEpisodePlanStringSlice(plan.ContinuityMemory.CameraRules)
	return plan
}

func parseEpisodePlanResponse(raw string) (*EpisodePlanResponse, error) {
	cleaned := cleanupLLMJSON(raw)
	var plan EpisodePlanResponse
	if err := json.Unmarshal([]byte(cleaned), &plan); err != nil {
		return nil, err
	}
	return normalizeEpisodePlanResponse(&plan), nil
}

func firstMatchingKeyword(text string, keywords []string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(trimmed, keyword) {
			return keyword
		}
	}
	return ""
}

func buildLocationAssetAnchorText(asset EpisodePlanLocationAsset) string {
	parts := []string{
		asset.VisualAnchor.Walls,
		asset.VisualAnchor.Floor,
		asset.VisualAnchor.Table,
		asset.VisualAnchor.Window.Position,
		asset.VisualAnchor.Window.OutsideView,
		asset.VisualAnchor.Window.HeightLogic,
		strings.Join(asset.VisualAnchor.Window.MustNotAppear, " "),
		asset.VisualAnchor.Lighting,
		strings.Join(asset.Constraints, " "),
	}
	return strings.Join(trimEpisodePlanStringSlice(parts), " ")
}

func buildPropAssetAnchorText(asset EpisodePlanPropAsset) string {
	parts := []string{
		asset.PropName,
		asset.SpaceRole,
		asset.VisualAnchor.Category,
		asset.VisualAnchor.Material,
		asset.VisualAnchor.Color,
		asset.VisualAnchor.Shape,
		asset.VisualAnchor.Placement,
		strings.Join(asset.Constraints, " "),
	}
	return strings.Join(trimEpisodePlanStringSlice(parts), " ")
}

func buildPropAnchorTextByLocation(propAssets []EpisodePlanPropAsset) map[string]string {
	result := make(map[string]string, len(propAssets))
	for _, asset := range propAssets {
		locationID := strings.TrimSpace(asset.LocationID)
		if locationID == "" {
			continue
		}
		anchorText := buildPropAssetAnchorText(asset)
		if anchorText == "" {
			continue
		}
		if existing := strings.TrimSpace(result[locationID]); existing != "" {
			result[locationID] = existing + " " + anchorText
			continue
		}
		result[locationID] = anchorText
	}
	return result
}

func buildVideoFingerprintVisibleText(payload *VideoFingerprintPayload) string {
	if payload == nil {
		return ""
	}
	parts := []string{payload.StyleZH, payload.PlayerDescZH}
	for _, phase := range payload.PhasesZH {
		parts = append(parts, phase.Content)
	}
	return strings.Join(trimEpisodePlanStringSlice(parts), " ")
}

func validateEpisodePlanResponse(plan *EpisodePlanResponse) error {
	warnings := collectEpisodePlanValidationWarnings(plan)
	if len(warnings) > 0 {
		return fmt.Errorf("%s", warnings[0])
	}
	return nil
}

func collectEpisodePlanValidationWarnings(plan *EpisodePlanResponse) []string {
	warnings := make([]string, 0)
	seenWarnings := make(map[string]struct{})
	addWarning := func(format string, args ...interface{}) {
		message := strings.TrimSpace(fmt.Sprintf(format, args...))
		if message == "" {
			return
		}
		if _, ok := seenWarnings[message]; ok {
			return
		}
		seenWarnings[message] = struct{}{}
		warnings = append(warnings, message)
	}
	if plan == nil {
		addWarning("episode plan is nil")
		return warnings
	}
	if plan.EpisodeID <= 0 {
		addWarning("episode_plan is missing episode_id")
	}
	if len(plan.SceneOutline) == 0 {
		addWarning("episode_plan is missing scene_outline")
	}
	locationIDs := make(map[string]EpisodePlanLocationAsset, len(plan.LocationAssets))
	propAnchorTextByLocation := make(map[string]string, len(plan.LocationAssets))
	for _, asset := range plan.LocationAssets {
		if asset.LocationID == "" {
			addWarning("location_assets contains empty location_id")
			continue
		}
		if _, ok := locationIDs[asset.LocationID]; ok {
			addWarning("location_assets contains duplicate location_id %q", asset.LocationID)
		}
		locationIDs[asset.LocationID] = asset
	}
	propIDs := make(map[string]struct{}, len(plan.PropAssets))
	for _, asset := range plan.PropAssets {
		if asset.PropID == "" {
			addWarning("prop_assets contains empty prop_id")
			continue
		}
		if _, ok := propIDs[asset.PropID]; ok {
			addWarning("prop_assets contains duplicate prop_id %q", asset.PropID)
		}
		propIDs[asset.PropID] = struct{}{}
		if asset.LocationID != "" {
			if _, ok := locationIDs[asset.LocationID]; !ok {
				addWarning("prop_assets references unknown location_id %q", asset.LocationID)
			}
		}
		anchorText := buildPropAssetAnchorText(asset)
		if strings.TrimSpace(anchorText) != "" && strings.TrimSpace(asset.LocationID) != "" {
			if existing := strings.TrimSpace(propAnchorTextByLocation[asset.LocationID]); existing != "" {
				propAnchorTextByLocation[asset.LocationID] = existing + " " + anchorText
			} else {
				propAnchorTextByLocation[asset.LocationID] = anchorText
			}
		}
		if term := firstMatchingKeyword(anchorText, unstableDecorBackgroundPropTerms); term != "" {
			if vague := firstMatchingKeyword(asset.VisualAnchor.Placement, vagueDecorPlacementTerms); vague != "" {
				addWarning("prop_assets[%s] decorative prop %q has vague placement %q; either lock it to an exact fixed spot or omit it entirely", asset.PropID, term, vague)
			}
		}
	}
	for _, asset := range plan.LocationAssets {
		anchorText := buildLocationAssetAnchorText(asset)
		if term := firstMatchingKeyword(anchorText, unstableDecorBackgroundPropTerms); term != "" {
			if !strings.Contains(propAnchorTextByLocation[asset.LocationID], term) {
				addWarning("location_assets[%s] mentions unstable decorative background prop %q outside prop_assets; move it to a fixed prop_asset with exact placement or omit it entirely", asset.LocationID, term)
			}
		}
	}
	characterNames := make(map[string]struct{}, len(plan.Characters))
	characterAppearsOnScreen := make(map[string]bool, len(plan.Characters))
	for _, char := range plan.Characters {
		name := strings.TrimSpace(char.Name)
		if name == "" {
			addWarning("characters contains empty name")
			continue
		}
		if err := validateEpisodePlanCharacter(char); err != nil {
			addWarning("%v", err)
		}
		characterNames[name] = struct{}{}
	}
	for idx, item := range plan.SceneOutline {
		expectedSceneNumber := idx + 1
		if item.SceneNumber != expectedSceneNumber {
			addWarning("scene_outline scene_number must be contiguous from 1, got %d at index %d", item.SceneNumber, idx)
		}
		if item.SceneName == "" {
			addWarning("scene_outline[%d] is missing scene_name", item.SceneNumber)
		}
		if item.LocationID == "" {
			addWarning("scene_outline[%d] is missing location_id", item.SceneNumber)
			continue
		}
		if _, ok := locationIDs[item.LocationID]; !ok {
			addWarning("scene_outline[%d] references unknown location_id %q", item.SceneNumber, item.LocationID)
		}
		if item.SceneGoal == "" {
			addWarning("scene_outline[%d] is missing scene_goal", item.SceneNumber)
		}
		if item.BlockingPlan == "" {
			addWarning("scene_outline[%d] is missing blocking_plan", item.SceneNumber)
		}
		if item.PropContinuity == nil {
			addWarning("scene_outline[%d] is missing prop_continuity", item.SceneNumber)
		}
		if len(item.PropContinuity) == 0 && strings.TrimSpace(propAnchorTextByLocation[item.LocationID]) != "" {
			addWarning("scene_outline[%d] has stable prop_assets at location %q but prop_continuity is empty", item.SceneNumber, item.LocationID)
		}
		if item.DialogueLines == nil {
			addWarning("scene_outline[%d] is missing dialogue_lines", item.SceneNumber)
		}
		for _, line := range item.DialogueLines {
			speaker, dialogueText := splitDialogueSpeakerAndText(line)
			if speaker == "" || dialogueText == "" {
				addWarning("scene_outline[%d] dialogue_lines contains invalid dialogue line %q", item.SceneNumber, line)
				continue
			}
			if hasWrappedDialogueQuotes(dialogueText) {
				addWarning("scene_outline[%d] dialogue_lines should not keep outer dialogue quote marks %q", item.SceneNumber, line)
			}
			if _, ok := characterNames[speaker]; !ok {
				if isGenericOffscreenVoiceLabel(speaker) {
					continue
				}
				if isLikelyRelationshipOrTitleAlias(speaker) {
					addWarning("scene_outline[%d] dialogue_lines uses unresolved relationship/title speaker %q; resolve it to a formal character name in characters", item.SceneNumber, speaker)
				} else {
					addWarning("scene_outline[%d] dialogue_lines references unknown speaker %q", item.SceneNumber, speaker)
				}
			}
			if tooLong, seconds, runeCount := isDialogueChunkTooLong(line); tooLong {
				addWarning("scene_outline[%d] dialogue chunk %q is too long for a single spoken beat (estimated %.1fs, %d chars); split it into shorter consecutive chunks upstream", item.SceneNumber, dialogueText, seconds, runeCount)
			}
		}
		dialogueEstimate, dialogueSpeakerCount, dialogueSpeakerSwitches := estimateDialogueTiming(item.DialogueLines)
		if dialogueEstimate > float64(maxVideoTotalDurationSeconds) {
			addWarning("scene_outline[%d] dialogue_lines likely exceed single-shot natural duration (estimated %.1fs)", item.SceneNumber, dialogueEstimate)
		}
		if dialogueEstimate > preferredDialogueSceneNaturalSeconds {
			addWarning("scene_outline[%d] dialogue-heavy shot is likely too long for natural LTX speech coverage (estimated %.1fs); split it into more shots upstream", item.SceneNumber, dialogueEstimate)
		}
		if dialogueSpeakerCount > 1 && dialogueSpeakerSwitches >= 2 {
			addWarning("scene_outline[%d] dialogue exchange is likely too dense for a single shot and should be split upstream", item.SceneNumber)
		}
		if item.MustUseLocation == false {
			addWarning("scene_outline[%d] must_use_location must be true", item.SceneNumber)
		}
		if item.MustNotCreateNewSpace == false {
			addWarning("scene_outline[%d] must_not_create_new_space must be true", item.SceneNumber)
		}
		for _, characterName := range item.Characters {
			characterAppearsOnScreen[characterName] = true
			if _, ok := characterNames[characterName]; !ok {
				addWarning("scene_outline[%d] references unknown character %q", item.SceneNumber, characterName)
			}
			locationRules := plan.BlockingRules[item.LocationID]
			if locationRules == nil {
				addWarning("blocking_rules is missing location_id %q", item.LocationID)
				continue
			}
			if _, ok := locationRules[characterName]; !ok {
				addWarning("blocking_rules[%s] is missing character %q", item.LocationID, characterName)
			}
		}
		if _, ok := plan.CameraRules[item.LocationID]; !ok {
			addWarning("camera_rules is missing location_id %q", item.LocationID)
		}
	}
	for _, char := range plan.Characters {
		name := strings.TrimSpace(char.Name)
		if name == "" {
			continue
		}
		if characterAppearsOnScreen[name] {
			continue
		}
		if isLikelyRelationshipOrTitleAlias(name) {
			addWarning("characters contains off-screen-only relationship/title character %q; unseen remote/voice-only speakers should stay out of characters and use a generic off-screen voice label instead", name)
		}
	}
	for _, text := range append(append([]string{}, plan.ContinuityMemory.SpaceRules...), plan.ContinuityMemory.PropRules...) {
		if term := firstMatchingKeyword(text, unstableDecorBackgroundPropTerms); term != "" {
			found := false
			for _, propText := range propAnchorTextByLocation {
				if strings.Contains(propText, term) {
					found = true
					break
				}
			}
			if !found {
				addWarning("continuity_memory mentions unstable decorative background prop %q outside prop_assets; omit it or define it as a fixed prop_asset", term)
			}
		}
	}
	return warnings
}

func validateEpisodePlanCharacter(char GeneratedCharacter) error {
	name := strings.TrimSpace(char.Name)
	gender := strings.TrimSpace(char.Gender)
	if name == "" {
		return fmt.Errorf("character is missing name")
	}
	switch gender {
	case "男性", "女性", "其他":
	default:
		return fmt.Errorf("character %q has invalid gender %q; only 男性/女性/其他 are allowed", name, gender)
	}
	if strings.TrimSpace(char.FaceFingerprint) == "" {
		return fmt.Errorf("character %q is missing face_fingerprint", name)
	}
	if strings.TrimSpace(char.Description) == "" {
		return fmt.Errorf("character %q is missing description", name)
	}
	if strings.TrimSpace(char.Fingerprint) == "" {
		return fmt.Errorf("character %q is missing fingerprint", name)
	}
	if strings.TrimSpace(char.PromptPosZH) == "" {
		return fmt.Errorf("character %q is missing prompt_pos_zh", name)
	}
	return nil
}

func constructEpisodePlanSystemPrompt() string {
	return ""
}

func constructEpisodePlanUserPrompt(req models.AutoGenerateRequest, existingCharacters []CharacterMemory, projectAnchorContext string, priorEpisodeContext string) string {
	return ""
}

func constructStrictSceneGenerationSystemPrompt(disableReferenceImages bool) string {
	return ""
}

func constructStrictSceneGenerationUserPrompt(plan EpisodePlanResponse, storyText string, existingScenes []GeneratedScene, startScene int, expectedTotalScenes int) string {
	return ""
}

func expectedSceneOutlineRef(sceneNumber int) string {
	return fmt.Sprintf("scene_outline[%d]", sceneNumber)
}

func promptPosScreenHasStrictOrder(text string) bool {
	labels := []string{
		"空间锁定：",
		"道具锁定：",
		"人物站位：",
		"镜头规则：",
		"角色外观：",
		"当前动作：",
	}
	lastIndex := -1
	for _, label := range labels {
		idx := strings.Index(text, label)
		if idx < 0 || idx <= lastIndex {
			return false
		}
		lastIndex = idx
	}
	return true
}

func sameOrderedStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if strings.TrimSpace(left[idx]) != strings.TrimSpace(right[idx]) {
			return false
		}
	}
	return true
}

func extractPromptPosScreenSection(text string, label string) string {
	labels := []string{
		"空间锁定：",
		"道具锁定：",
		"人物站位：",
		"镜头规则：",
		"角色外观：",
		"当前动作：",
	}
	start := strings.Index(text, label)
	if start < 0 {
		return ""
	}
	start += len(label)
	end := len(text)
	for _, candidate := range labels {
		if candidate == label {
			continue
		}
		if idx := strings.Index(text[start:], candidate); idx >= 0 {
			absolute := start + idx
			if absolute < end {
				end = absolute
			}
		}
	}
	return strings.TrimSpace(text[start:end])
}

func firstFrameActionChainMarker(text string) string {
	for _, label := range []string{"镜头规则：", "当前动作："} {
		section := extractPromptPosScreenSection(text, label)
		for _, marker := range firstFrameSequentialActionMarkers {
			if marker != "" && strings.Contains(section, marker) {
				return marker
			}
		}
	}
	return ""
}

func phaseOneBacktracksBeforeStillFrame(promptPos string, description string, payload *VideoFingerprintPayload) string {
	if payload == nil || len(payload.PhasesZH) == 0 {
		return ""
	}
	phaseOne := strings.TrimSpace(payload.PhasesZH[0].Content)
	if phaseOne == "" {
		return ""
	}
	stillText := strings.TrimSpace(description + " " + extractPromptPosScreenSection(promptPos, "当前动作："))
	if stillText == "" {
		return ""
	}
	arrivalMarker := firstMatchingKeyword(phaseOne, firstFrameArrivalStateMarkers)
	if arrivalMarker == "" {
		return ""
	}
	settledMarker := firstMatchingKeyword(stillText, firstFrameSettledStateMarkers)
	if settledMarker == "" {
		return ""
	}
	return fmt.Sprintf("phase 1 starts from arrival marker %q while still frame already implies settled state %q", arrivalMarker, settledMarker)
}

func singleSpeakerBalancedTwoShotWarning(promptPos string, payload *VideoFingerprintPayload, scene GeneratedScene) string {
	if len(scene.Characters) < 2 {
		return ""
	}
	_, dialogueSpeakerCount, _ := estimateDialogueTiming(scene.Dialogue)
	if dialogueSpeakerCount != 1 {
		return ""
	}
	combined := strings.TrimSpace(promptPos)
	if payload != nil {
		combined = strings.TrimSpace(combined + " " + payload.StyleZH + " " + payload.PlayerDescZH)
	}
	if firstMatchingKeyword(combined, balancedDialogueCoverageMarkers) == "" {
		return ""
	}
	if firstMatchingKeyword(combined, focusedSingleCoverageMarkers) != "" {
		if firstMatchingKeyword(combined, focusIsolationMarkers) != "" {
			return ""
		}
		return "single-speaker beat mentions focused coverage but still lacks shallow-depth / edge-presence isolation; keep the speaker sharp and reduce the non-speaker to blurred edge presence"
	}
	if firstMatchingKeyword(combined, focusIsolationMarkers) != "" {
		return "single-speaker beat still reads like a balanced two-shot; prefer speaker-led single / over-the-shoulder / listener reaction coverage instead of keeping both characters as equal frontal subjects"
	}
	return "single-speaker beat still reads like a balanced two-shot and lacks blur/depth isolation; prefer speaker-led single / over-the-shoulder / listener reaction coverage with the non-speaker reduced to blurred edge presence"
}

func readingSourceCoverageWarning(promptPos string, payload *VideoFingerprintPayload, scene GeneratedScene) string {
	if len(scene.Dialogue) == 0 {
		return ""
	}
	combined := strings.TrimSpace(scene.SceneGoal + " " + scene.Description + " " + promptPos)
	if payload != nil {
		combined = strings.TrimSpace(combined + " " + payload.StyleZH + " " + payload.PlayerDescZH)
	}
	if firstMatchingKeyword(combined, readingSourceMarkers) == "" {
		return ""
	}
	if firstMatchingKeyword(combined, readingCoverageMarkers) != "" {
		return ""
	}
	return "reading-source beat lacks device insert / POV / reader-reaction coverage; prefer tablet/screen/document insert, over-shoulder reading, or quiet read-after reaction instead of flat speaking into empty space"
}

func weakPresenceVisibilityWarning(promptPos string, scene GeneratedScene) string {
	if len(scene.Characters) < 2 {
		return ""
	}
	combined := strings.TrimSpace(
		extractPromptPosScreenSection(promptPos, "人物站位：") + " " +
			extractPromptPosScreenSection(promptPos, "角色外观：") + " " +
			extractPromptPosScreenSection(promptPos, "镜头规则："),
	)
	if combined == "" {
		return ""
	}
	if firstMatchingKeyword(combined, weakPresenceMarkers) == "" {
		return ""
	}
	if firstMatchingKeyword(combined, visibleAnchorMarkers) != "" {
		return ""
	}
	return "secondary role is described only as weak edge presence without a concrete visible anchor; if a formal role stays in scene.characters, specify shoulder/back/head silhouette, upper-body contour, hand, or device body so it does not disappear"
}

func coverageSignature(promptPos string, payload *VideoFingerprintPayload) string {
	combined := strings.TrimSpace(promptPos)
	if payload != nil {
		combined = strings.TrimSpace(combined + " " + payload.StyleZH + " " + payload.PlayerDescZH)
	}
	for _, group := range coverageSignatureGroups {
		if firstMatchingKeyword(combined, group.Markers) != "" {
			return group.Signature
		}
	}
	return ""
}

func singleVisibleRemoteMultiSpeakerWarning(promptPos string, payload *VideoFingerprintPayload, scene GeneratedScene) string {
	if len(scene.Characters) != 1 {
		return ""
	}
	dialogueSeconds, dialogueSpeakerCount, _ := estimateDialogueTiming(scene.Dialogue)
	if dialogueSpeakerCount <= 1 {
		return ""
	}
	combined := strings.TrimSpace(scene.SceneGoal + " " + scene.Description + " " + promptPos)
	if payload != nil {
		combined = strings.TrimSpace(combined + " " + payload.StyleZH + " " + payload.PlayerDescZH)
	}
	if !strings.Contains(combined, "电话") &&
		!strings.Contains(combined, "手机") &&
		!strings.Contains(combined, "耳机") &&
		!strings.Contains(combined, "蓝牙") &&
		!strings.Contains(combined, "对讲") &&
		!strings.Contains(combined, "远端") {
		return ""
	}
	if firstMatchingKeyword(combined, []string{"听位反应", "闭口听位", "设备特写", "主观视角", "过肩", "phone-listening reaction", "phone-speaking single"}) != "" {
		return ""
	}
	return fmt.Sprintf("single visible remote-communication beat carries %d dialogue speakers over %.1fs but lacks explicit listening/speaking split; keep the visible character in one role at a time and use device/listener coverage for the off-screen voice", dialogueSpeakerCount, dialogueSeconds)
}

func collectQuotedAudioSpeakerPrefixWarnings(payload *VideoFingerprintPayload, scene GeneratedScene) []string {
	if payload == nil || len(payload.PhasesZH) == 0 {
		return nil
	}
	warnings := make([]string, 0)
	seen := make(map[string]struct{})
	sceneCharacterSet := make(map[string]struct{}, len(scene.Characters))
	for _, characterName := range scene.Characters {
		trimmedName := strings.TrimSpace(characterName)
		if trimmedName == "" {
			continue
		}
		sceneCharacterSet[trimmedName] = struct{}{}
	}
	addWarning := func(format string, args ...interface{}) {
		message := strings.TrimSpace(fmt.Sprintf(format, args...))
		if message == "" {
			return
		}
		if _, ok := seen[message]; ok {
			return
		}
		seen[message] = struct{}{}
		warnings = append(warnings, message)
	}
	for _, phase := range payload.PhasesZH {
		for _, quoted := range extractQuotedDialogueTextsFromAudio(phase.Audio) {
			speaker, dialogueText := splitDialogueSpeakerAndText(quoted)
			if speaker == "" || strings.TrimSpace(dialogueText) == "" {
				continue
			}
			trimmedSpeaker := strings.TrimSpace(speaker)
			if _, ok := sceneCharacterSet[trimmedSpeaker]; ok {
				addWarning("scene %d video_fingerprint quoted dialogue must not include formal speaker prefix %q inside spoken text", scene.SceneNumber, trimmedSpeaker)
				continue
			}
			if isGenericOffscreenVoiceLabel(trimmedSpeaker) {
				addWarning("scene %d video_fingerprint quoted dialogue must not include off-screen speaker label %q inside spoken text", scene.SceneNumber, trimmedSpeaker)
				continue
			}
			if isLikelyRelationshipOrTitleAlias(trimmedSpeaker) {
				addWarning("scene %d video_fingerprint quoted dialogue must not include relationship/title speaker prefix %q inside spoken text", scene.SceneNumber, trimmedSpeaker)
			}
		}
	}
	return warnings
}

func splitDialogueSpeakerAndText(line string) (string, string) {
	trimmed := strings.TrimSpace(line)
	separators := []string{":", "："}
	for _, sep := range separators {
		if idx := strings.Index(trimmed, sep); idx > 0 {
			return strings.TrimSpace(trimmed[:idx]), strings.TrimSpace(trimmed[idx+len(sep):])
		}
	}
	return "", trimmed
}

func hasWrappedDialogueQuotes(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	pairs := [][2]string{
		{"“", "”"},
		{"「", "」"},
		{"『", "』"},
	}
	for _, pair := range pairs {
		if strings.HasPrefix(trimmed, pair[0]) && strings.HasSuffix(trimmed, pair[1]) {
			return true
		}
	}
	return false
}

func isLikelyRelationshipOrTitleAlias(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	for _, cue := range []string{"母亲", "妈妈", "父亲", "爸爸", "儿子", "女儿", "哥哥", "姐姐", "弟弟", "妹妹", "奶奶", "爷爷", "外婆", "外公", "叔叔", "阿姨", "伯母", "伯父", "姑姑", "舅舅", "婶婶", "老师", "医生", "护士", "司机", "秘书", "助理", "老板", "经理", "主任", "总监", "店员", "服务员", "服务生", "侍者", "前台", "同事", "警察", "保安"} {
		if strings.Contains(trimmed, cue) {
			return true
		}
	}
	return false
}

func isGenericOffscreenVoiceLabel(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	validPrefix := strings.HasPrefix(trimmed, "画外") ||
		strings.HasPrefix(trimmed, "远端") ||
		strings.HasPrefix(trimmed, "电话另一端") ||
		strings.HasPrefix(trimmed, "设备语音") ||
		strings.HasPrefix(trimmed, "系统提示音") ||
		strings.HasPrefix(trimmed, "门禁对讲里")
	if !validPrefix {
		return false
	}
	for _, cue := range []string{"男声", "女声", "声音", "语音", "提示音"} {
		if strings.Contains(trimmed, cue) {
			return true
		}
	}
	return false
}

func extractAudioDirectiveHeader(audio string) string {
	trimmed := strings.TrimSpace(audio)
	if trimmed == "" {
		return ""
	}
	if idx := strings.IndexAny(trimmed, "\"“「『"); idx >= 0 {
		return strings.TrimSpace(trimmed[:idx])
	}
	return trimmed
}

func estimateDialogueChunkTiming(line string) float64 {
	seconds, _, _ := estimateDialogueTiming([]string{line})
	return seconds
}

func isDialogueChunkTooLong(line string) (bool, float64, int) {
	_, dialogueText := splitDialogueSpeakerAndText(line)
	runeCount := utf8.RuneCountInString(dialogueText)
	seconds := estimateDialogueChunkTiming(line)
	return runeCount > maxDialogueChunkRuneCount || seconds > maxDialogueChunkNaturalSeconds, seconds, runeCount
}

func parsePhaseDurationSeconds(timeRange string) float64 {
	trimmed := strings.TrimSpace(timeRange)
	if trimmed == "" {
		return 0
	}
	parts := strings.Split(trimmed, "-")
	if len(parts) != 2 {
		return 0
	}
	start, errStart := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	end, errEnd := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if errStart != nil || errEnd != nil || end <= start {
		return 0
	}
	return end - start
}

func extractQuotedDialogueTextsFromAudio(audio string) []string {
	trimmed := strings.TrimSpace(audio)
	if trimmed == "" {
		return nil
	}
	pairs := [][2]string{
		{"\"", "\""},
		{"“", "”"},
		{"「", "」"},
	}
	result := make([]string, 0, 2)
	for _, pair := range pairs {
		searchFrom := 0
		for {
			start := strings.Index(trimmed[searchFrom:], pair[0])
			if start < 0 {
				break
			}
			start += searchFrom + len(pair[0])
			end := strings.Index(trimmed[start:], pair[1])
			if end < 0 {
				break
			}
			end += start
			quoted := strings.TrimSpace(trimmed[start:end])
			if quoted != "" {
				result = append(result, quoted)
			}
			searchFrom = end + len(pair[1])
			if searchFrom >= len(trimmed) {
				break
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return nil
}

func estimateAudioDialogueTiming(audio string) float64 {
	quotedTexts := extractQuotedDialogueTextsFromAudio(audio)
	if len(quotedTexts) == 0 {
		return 0
	}
	dialogueLines := make([]string, 0, len(quotedTexts))
	for _, text := range quotedTexts {
		dialogueLines = append(dialogueLines, "audio:"+text)
	}
	seconds, _, _ := estimateDialogueTiming(dialogueLines)
	return seconds
}

func estimateDialogueTiming(dialogue []string) (float64, int, int) {
	if len(dialogue) == 0 {
		return 0, 0, 0
	}
	speakerSet := make(map[string]struct{}, len(dialogue))
	seconds := 0.0
	lastSpeaker := ""
	speakerSwitches := 0
	for _, line := range dialogue {
		speaker, dialogueText := splitDialogueSpeakerAndText(line)
		if speaker != "" {
			speakerSet[speaker] = struct{}{}
		}
		if speaker != "" && lastSpeaker != "" && speaker != lastSpeaker {
			speakerSwitches++
			seconds += 0.35
		}
		if speaker != "" {
			lastSpeaker = speaker
		}
		runeCount := utf8.RuneCountInString(dialogueText)
		if runeCount > 0 {
			seconds += float64(runeCount) / dialogueCharsPerSecond
			seconds += 0.25
		}
		seconds += float64(strings.Count(dialogueText, "，")+strings.Count(dialogueText, ",")+strings.Count(dialogueText, "、")) * 0.18
		seconds += float64(strings.Count(dialogueText, "。")+strings.Count(dialogueText, ".")+strings.Count(dialogueText, "！")+strings.Count(dialogueText, "!")+strings.Count(dialogueText, "？")+strings.Count(dialogueText, "?")) * 0.42
		seconds += float64(strings.Count(dialogueText, "；")+strings.Count(dialogueText, ";")+strings.Count(dialogueText, "：")+strings.Count(dialogueText, ":")) * 0.22
		seconds += float64(strings.Count(dialogueText, "…")) * 0.25
		seconds += float64(strings.Count(dialogueText, "——")) * 0.35
	}
	if seconds > 0 {
		seconds += 0.35
	}
	return seconds, len(speakerSet), speakerSwitches
}

func validateDialogueCoverageInVideoFingerprint(payload *VideoFingerprintPayload, dialogue []string) error {
	if payload == nil || len(dialogue) == 0 {
		return nil
	}
	audioParts := make([]string, 0, len(payload.PhasesZH))
	for _, phase := range payload.PhasesZH {
		audio := strings.TrimSpace(phase.Audio)
		if audio != "" {
			audioParts = append(audioParts, audio)
		}
	}
	combinedAudio := strings.Join(audioParts, "\n")
	searchOffset := 0
	for _, line := range dialogue {
		_, dialogueText := splitDialogueSpeakerAndText(line)
		if dialogueText == "" {
			continue
		}
		idx := strings.Index(combinedAudio[searchOffset:], dialogueText)
		if idx < 0 {
			return fmt.Errorf("dialogue line %q was not found in video_fingerprint audio", dialogueText)
		}
		searchOffset += idx + len(dialogueText)
		if searchOffset > len(combinedAudio) {
			searchOffset = len(combinedAudio)
		}
	}
	return nil
}

func collectStrictSceneExpansionWarnings(scenes []GeneratedScene, plan EpisodePlanResponse, startScene int, expectedTotalScenes int) []string {
	if expectedTotalScenes <= 0 {
		expectedTotalScenes = len(plan.SceneOutline)
	}
	outlineByNumber := make(map[int]EpisodePlanSceneOutlineItem, len(plan.SceneOutline))
	propAnchorTextByLocation := buildPropAnchorTextByLocation(plan.PropAssets)
	for _, item := range plan.SceneOutline {
		outlineByNumber[item.SceneNumber] = item
	}
	warnings := make([]string, 0)
	seenWarnings := make(map[string]struct{})
	addWarning := func(format string, args ...interface{}) {
		message := strings.TrimSpace(fmt.Sprintf(format, args...))
		if message == "" {
			return
		}
		if _, ok := seenWarnings[message]; ok {
			return
		}
		seenWarnings[message] = struct{}{}
		warnings = append(warnings, message)
	}
	seenSceneNumbers := make(map[int]struct{}, len(scenes))
	for _, scene := range scenes {
		if scene.SceneNumber > 0 {
			seenSceneNumbers[scene.SceneNumber] = struct{}{}
		}
		if scene.SceneNumber < startScene {
			addWarning("scene %d is below requested start_scene %d", scene.SceneNumber, startScene)
		}
		if expectedTotalScenes > 0 && scene.SceneNumber > expectedTotalScenes {
			addWarning("scene %d exceeds planned total scenes %d", scene.SceneNumber, expectedTotalScenes)
		}
		outline, ok := outlineByNumber[scene.SceneNumber]
		if !ok {
			addWarning("scene %d does not exist in scene_outline", scene.SceneNumber)
			continue
		}
		if strings.TrimSpace(scene.Name) != strings.TrimSpace(outline.SceneName) {
			addWarning("scene %d scene_name does not match scene_outline", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.LocationID) != strings.TrimSpace(outline.LocationID) {
			addWarning("scene %d location_id does not match scene_outline", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.OutlineRef) != expectedSceneOutlineRef(scene.SceneNumber) {
			addWarning("scene %d outline_ref must be %q", scene.SceneNumber, expectedSceneOutlineRef(scene.SceneNumber))
		}
		if strings.TrimSpace(scene.SceneGoal) != strings.TrimSpace(outline.SceneGoal) {
			addWarning("scene %d scene_goal does not match scene_outline", scene.SceneNumber)
		}
		if !sameOrderedStrings(scene.Characters, outline.Characters) {
			addWarning("scene %d characters do not match scene_outline order", scene.SceneNumber)
		}
		if !sameOrderedStrings(scene.Dialogue, outline.DialogueLines) {
			addWarning("scene %d dialogue does not match scene_outline.dialogue_lines", scene.SceneNumber)
		}
		for _, line := range scene.Dialogue {
			_, dialogueText := splitDialogueSpeakerAndText(line)
			if tooLong, seconds, runeCount := isDialogueChunkTooLong(line); tooLong {
				addWarning("scene %d dialogue chunk %q is too long for a single spoken beat (estimated %.1fs, %d chars); first pass should split it further", scene.SceneNumber, dialogueText, seconds, runeCount)
			}
		}
		dialogueEstimate, dialogueSpeakerCount, dialogueSpeakerSwitches := estimateDialogueTiming(scene.Dialogue)
		if dialogueEstimate > float64(maxVideoTotalDurationSeconds) {
			addWarning("scene %d dialogue likely exceeds single-shot natural duration (estimated %.1fs)", scene.SceneNumber, dialogueEstimate)
		}
		if dialogueEstimate > preferredDialogueSceneNaturalSeconds {
			addWarning("scene %d dialogue-heavy shot is likely too long for natural LTX speech coverage (estimated %.1fs); first pass should split it further", scene.SceneNumber, dialogueEstimate)
		}
		if dialogueSpeakerCount > 1 && dialogueSpeakerSwitches >= 2 {
			addWarning("scene %d dialogue exchange is likely too dense for a single shot and should have been split upstream", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.CharacterBlocking) == "" {
			addWarning("scene %d is missing character_blocking", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.CameraLock) == "" {
			addWarning("scene %d is missing camera_lock", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.Description) == "" {
			addWarning("scene %d is missing description", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.PromptPosScreenZH) == "" {
			addWarning("scene %d is missing prompt_pos_screen_zh", scene.SceneNumber)
		} else if promptPosScreenHasStrictOrder(scene.PromptPosScreenZH) == false {
			addWarning("scene %d prompt_pos_screen_zh does not follow strict section order", scene.SceneNumber)
		} else if marker := firstFrameActionChainMarker(scene.PromptPosScreenZH); marker != "" {
			addWarning("scene %d prompt_pos_screen_zh contains sequential action-chain marker %q; scene prompt must stay on the first-frame still image moment", scene.SceneNumber, marker)
		} else if term := firstMatchingKeyword(scene.PromptPosScreenZH, unstableDecorBackgroundPropTerms); term != "" && !strings.Contains(propAnchorTextByLocation[scene.LocationID], term) {
			addWarning("scene %d prompt_pos_screen_zh introduces decorative background prop %q that is not locked in prop_assets", scene.SceneNumber, term)
		}
		if strings.TrimSpace(scene.VideoFingerprint) == "" {
			addWarning("scene %d is missing video_fingerprint", scene.SceneNumber)
			continue
		}
		payload, err := parseVideoFingerprintPayload(scene.VideoFingerprint)
		if err != nil {
			addWarning("scene %d video_fingerprint is invalid JSON: %v", scene.SceneNumber, err)
			continue
		}
		nameCheckScene := scene
		nameCheckScene.Characters = append([]string(nil), outline.Characters...)
		if err := validateVideoFingerprintAudioText(payload); err != nil {
			addWarning("scene %d video_fingerprint audio is invalid: %v", scene.SceneNumber, err)
		}
		for _, warning := range collectVideoFingerprintAudioSpeakerWarnings(payload, nameCheckScene) {
			addWarning("%s", warning)
		}
		for _, warning := range collectQuotedAudioSpeakerPrefixWarnings(payload, nameCheckScene) {
			addWarning("%s", warning)
		}
		if err := validateVideoFingerprintDuration(payload); err != nil {
			addWarning("scene %d video_fingerprint duration is invalid: %v", scene.SceneNumber, err)
		}
		if err := validateVideoFingerprintPhaseContent(payload); err != nil {
			addWarning("scene %d video_fingerprint phases are invalid: %v", scene.SceneNumber, err)
		}
		if mismatch := phaseOneBacktracksBeforeStillFrame(scene.PromptPosScreenZH, scene.Description, payload); mismatch != "" {
			addWarning("scene %d video_fingerprint phase 1 appears to rewind before the still-image start state: %s", scene.SceneNumber, mismatch)
		}
		if coverageWarning := singleSpeakerBalancedTwoShotWarning(scene.PromptPosScreenZH, payload, scene); coverageWarning != "" {
			addWarning("scene %d %s", scene.SceneNumber, coverageWarning)
		}
		if visibilityWarning := weakPresenceVisibilityWarning(scene.PromptPosScreenZH, scene); visibilityWarning != "" {
			addWarning("scene %d %s", scene.SceneNumber, visibilityWarning)
		}
		if readingWarning := readingSourceCoverageWarning(scene.PromptPosScreenZH, payload, scene); readingWarning != "" {
			addWarning("scene %d %s", scene.SceneNumber, readingWarning)
		}
		if remoteWarning := singleVisibleRemoteMultiSpeakerWarning(scene.PromptPosScreenZH, payload, scene); remoteWarning != "" {
			addWarning("scene %d %s", scene.SceneNumber, remoteWarning)
		}
		for _, phase := range payload.PhasesZH {
			phaseDuration := parsePhaseDurationSeconds(phase.TimeRange)
			audioDialogueSeconds := estimateAudioDialogueTiming(phase.Audio)
			if audioDialogueSeconds > maxDialogueChunkNaturalSeconds {
				addWarning("scene %d phase %d carries an overlong spoken chunk in audio (estimated %.1fs); upstream scene/dialogue chunking is still too coarse", scene.SceneNumber, max(phase.Index, 1), audioDialogueSeconds)
			}
			if phaseDuration > 0 && audioDialogueSeconds > phaseDuration+0.35 {
				addWarning("scene %d phase %d audio likely exceeds its time_range %s (speech estimate %.1fs)", scene.SceneNumber, max(phase.Index, 1), strings.TrimSpace(phase.TimeRange), audioDialogueSeconds)
			}
		}
		if term := firstMatchingKeyword(buildVideoFingerprintVisibleText(payload), unstableDecorBackgroundPropTerms); term != "" && !strings.Contains(propAnchorTextByLocation[scene.LocationID], term) {
			addWarning("scene %d video_fingerprint introduces decorative background prop %q that is not locked in prop_assets", scene.SceneNumber, term)
		}
		if err := validateDialogueCoverageInVideoFingerprint(payload, scene.Dialogue); err != nil {
			addWarning("scene %d video_fingerprint dialogue coverage is invalid: %v", scene.SceneNumber, err)
		}
		if err := validateSceneVideoFingerprintNames(payload, nameCheckScene); err != nil {
			addWarning("%v", err)
		}
	}
	for sceneNumber := startScene; expectedTotalScenes > 0 && sceneNumber <= expectedTotalScenes; sceneNumber++ {
		if _, ok := seenSceneNumbers[sceneNumber]; !ok {
			addWarning("scene_outline[%d] was not returned in final scenes", sceneNumber)
		}
	}
	if len(scenes) >= 3 {
		sortedScenes := append([]GeneratedScene(nil), scenes...)
		sort.SliceStable(sortedScenes, func(i, j int) bool {
			return sortedScenes[i].SceneNumber < sortedScenes[j].SceneNumber
		})
		runStart := 0
		runCount := 1
		runSignature := ""
		runLocationID := ""
		for idx, scene := range sortedScenes {
			var payload *VideoFingerprintPayload
			if strings.TrimSpace(scene.VideoFingerprint) != "" {
				if parsed, err := parseVideoFingerprintPayload(scene.VideoFingerprint); err == nil {
					payload = parsed
				}
			}
			signature := coverageSignature(scene.PromptPosScreenZH, payload)
			locationID := strings.TrimSpace(scene.LocationID)
			if idx == 0 {
				runSignature = signature
				runLocationID = locationID
				continue
			}
			if signature != "" && signature == runSignature && locationID != "" && locationID == runLocationID {
				runCount++
				if runCount >= 3 {
					addWarning("scenes %d-%d in location %q repeat the same %s coverage signature; vary singles / over-shoulder / insert / reaction coverage more aggressively to avoid static staging", sortedScenes[runStart].SceneNumber, scene.SceneNumber, locationID, signature)
				}
				continue
			}
			runStart = idx
			runCount = 1
			runSignature = signature
			runLocationID = locationID
		}
	}
	return warnings
}

func collectVideoFingerprintAudioSpeakerWarnings(payload *VideoFingerprintPayload, scene GeneratedScene) []string {
	if payload == nil || len(payload.PhasesZH) == 0 {
		return nil
	}
	warnings := make([]string, 0)
	seen := make(map[string]struct{})
	addWarning := func(format string, args ...interface{}) {
		message := strings.TrimSpace(fmt.Sprintf(format, args...))
		if message == "" {
			return
		}
		if _, ok := seen[message]; ok {
			return
		}
		seen[message] = struct{}{}
		warnings = append(warnings, message)
	}
	for _, phase := range payload.PhasesZH {
		header := extractAudioDirectiveHeader(phase.Audio)
		if header == "" {
			continue
		}
		for _, characterName := range scene.Characters {
			trimmedName := strings.TrimSpace(characterName)
			if trimmedName == "" {
				continue
			}
			if strings.Contains(header, trimmedName) {
				addWarning("scene %d video_fingerprint audio header contains forbidden character name %q", scene.SceneNumber, trimmedName)
			}
		}
		if isLikelyRelationshipOrTitleAlias(header) {
			addWarning("scene %d video_fingerprint audio header contains relationship/title label %q", scene.SceneNumber, header)
		}
	}
	return warnings
}

func validateStrictSceneExpansionAgainstPlan(scenes []GeneratedScene, plan EpisodePlanResponse, startScene int, expectedTotalScenes int) error {
	if expectedTotalScenes <= 0 {
		expectedTotalScenes = len(plan.SceneOutline)
	}
	outlineByNumber := make(map[int]EpisodePlanSceneOutlineItem, len(plan.SceneOutline))
	for _, item := range plan.SceneOutline {
		outlineByNumber[item.SceneNumber] = item
	}
	for _, scene := range scenes {
		if scene.SceneNumber < startScene {
			return fmt.Errorf("scene %d is below requested start_scene %d", scene.SceneNumber, startScene)
		}
		if expectedTotalScenes > 0 && scene.SceneNumber > expectedTotalScenes {
			return fmt.Errorf("scene %d exceeds planned total scenes %d", scene.SceneNumber, expectedTotalScenes)
		}
		outline, ok := outlineByNumber[scene.SceneNumber]
		if !ok {
			return fmt.Errorf("scene %d does not exist in scene_outline", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.Name) != strings.TrimSpace(outline.SceneName) {
			return fmt.Errorf("scene %d scene_name does not match scene_outline", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.LocationID) != strings.TrimSpace(outline.LocationID) {
			return fmt.Errorf("scene %d location_id does not match scene_outline", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.OutlineRef) != expectedSceneOutlineRef(scene.SceneNumber) {
			return fmt.Errorf("scene %d outline_ref must be %q", scene.SceneNumber, expectedSceneOutlineRef(scene.SceneNumber))
		}
		if strings.TrimSpace(scene.SceneGoal) != strings.TrimSpace(outline.SceneGoal) {
			return fmt.Errorf("scene %d scene_goal does not match scene_outline", scene.SceneNumber)
		}
		if !sameOrderedStrings(scene.Characters, outline.Characters) {
			return fmt.Errorf("scene %d characters do not match scene_outline order", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.CharacterBlocking) == "" {
			return fmt.Errorf("scene %d is missing character_blocking", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.CameraLock) == "" {
			return fmt.Errorf("scene %d is missing camera_lock", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.Description) == "" {
			return fmt.Errorf("scene %d is missing description", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.PromptPosScreenZH) == "" {
			return fmt.Errorf("scene %d is missing prompt_pos_screen_zh", scene.SceneNumber)
		}
		if promptPosScreenHasStrictOrder(scene.PromptPosScreenZH) == false {
			return fmt.Errorf("scene %d prompt_pos_screen_zh does not follow strict section order", scene.SceneNumber)
		}
		if strings.TrimSpace(scene.VideoFingerprint) == "" {
			return fmt.Errorf("scene %d is missing video_fingerprint", scene.SceneNumber)
		}
		payload, err := parseVideoFingerprintPayload(scene.VideoFingerprint)
		if err != nil {
			return fmt.Errorf("scene %d video_fingerprint is invalid JSON: %v", scene.SceneNumber, err)
		}
		if err := validateVideoFingerprintAudioText(payload); err != nil {
			return fmt.Errorf("scene %d video_fingerprint audio is invalid: %v", scene.SceneNumber, err)
		}
		if err := validateVideoFingerprintDuration(payload); err != nil {
			return fmt.Errorf("scene %d video_fingerprint duration is invalid: %v", scene.SceneNumber, err)
		}
		if err := validateVideoFingerprintPhaseContent(payload); err != nil {
			return fmt.Errorf("scene %d video_fingerprint phases are invalid: %v", scene.SceneNumber, err)
		}
		if err := validateSceneVideoFingerprintNames(payload, scene); err != nil {
			return err
		}
	}
	return nil
}

func buildEpisodePlanProjectAnchors(plan EpisodePlanResponse) []ProjectAnchorMemoryItem {
	locationNames := make(map[string]string, len(plan.LocationAssets))
	anchors := make([]ProjectAnchorMemoryItem, 0, len(plan.LocationAssets)+len(plan.PropAssets))
	for _, asset := range plan.LocationAssets {
		locationNames[asset.LocationID] = asset.LocationName
		anchors = append(anchors, ProjectAnchorMemoryItem{
			AnchorType: "location",
			AnchorKey:  asset.LocationID,
			Summary:    buildEpisodePlanLocationAnchorSummary(asset),
		})
	}
	for _, asset := range plan.PropAssets {
		anchors = append(anchors, ProjectAnchorMemoryItem{
			AnchorType: "prop",
			AnchorKey:  asset.PropID,
			Summary:    buildEpisodePlanPropAnchorSummary(asset, locationNames),
		})
	}
	return normalizeProjectAnchorMemoryItems(anchors)
}

func buildEpisodePlanLocationAnchorSummary(asset EpisodePlanLocationAsset) string {
	parts := []string{
		fmt.Sprintf("location_name=%s", asset.LocationName),
		fmt.Sprintf("space_type=%s", asset.SpaceType),
		fmt.Sprintf("walls=%s", asset.VisualAnchor.Walls),
		fmt.Sprintf("floor=%s", asset.VisualAnchor.Floor),
	}
	if strings.TrimSpace(asset.VisualAnchor.Table) != "" {
		parts = append(parts, fmt.Sprintf("table=%s", asset.VisualAnchor.Table))
	}
	if strings.TrimSpace(asset.VisualAnchor.Window.Position) != "" {
		parts = append(parts, fmt.Sprintf("window_position=%s", asset.VisualAnchor.Window.Position))
	}
	if strings.TrimSpace(asset.VisualAnchor.Window.OutsideView) != "" {
		parts = append(parts, fmt.Sprintf("outside_view=%s", asset.VisualAnchor.Window.OutsideView))
	}
	if strings.TrimSpace(asset.VisualAnchor.Window.HeightLogic) != "" {
		parts = append(parts, fmt.Sprintf("height_logic=%s", asset.VisualAnchor.Window.HeightLogic))
	}
	if strings.TrimSpace(asset.VisualAnchor.Lighting) != "" {
		parts = append(parts, fmt.Sprintf("lighting=%s", asset.VisualAnchor.Lighting))
	}
	if len(asset.VisualAnchor.Window.MustNotAppear) > 0 {
		parts = append(parts, fmt.Sprintf("window_must_not_appear=%s", strings.Join(asset.VisualAnchor.Window.MustNotAppear, "、")))
	}
	if len(asset.Constraints) > 0 {
		parts = append(parts, fmt.Sprintf("constraints=%s", strings.Join(asset.Constraints, "；")))
	}
	return strings.Join(parts, "；")
}

func buildEpisodePlanPropAnchorSummary(asset EpisodePlanPropAsset, locationNames map[string]string) string {
	parts := []string{
		fmt.Sprintf("prop_name=%s", asset.PropName),
	}
	if locationName := strings.TrimSpace(locationNames[asset.LocationID]); locationName != "" {
		parts = append(parts, fmt.Sprintf("location=%s", locationName))
	}
	if strings.TrimSpace(asset.LocationID) != "" {
		parts = append(parts, fmt.Sprintf("location_id=%s", asset.LocationID))
	}
	if strings.TrimSpace(asset.SpaceRole) != "" {
		parts = append(parts, fmt.Sprintf("space_role=%s", asset.SpaceRole))
	}
	if strings.TrimSpace(asset.VisualAnchor.Category) != "" {
		parts = append(parts, fmt.Sprintf("category=%s", asset.VisualAnchor.Category))
	}
	if strings.TrimSpace(asset.VisualAnchor.Material) != "" {
		parts = append(parts, fmt.Sprintf("material=%s", asset.VisualAnchor.Material))
	}
	if strings.TrimSpace(asset.VisualAnchor.Color) != "" {
		parts = append(parts, fmt.Sprintf("color=%s", asset.VisualAnchor.Color))
	}
	if strings.TrimSpace(asset.VisualAnchor.Shape) != "" {
		parts = append(parts, fmt.Sprintf("shape=%s", asset.VisualAnchor.Shape))
	}
	if strings.TrimSpace(asset.VisualAnchor.Placement) != "" {
		parts = append(parts, fmt.Sprintf("placement=%s", asset.VisualAnchor.Placement))
	}
	if len(asset.Constraints) > 0 {
		parts = append(parts, fmt.Sprintf("constraints=%s", strings.Join(asset.Constraints, "；")))
	}
	return strings.Join(parts, "；")
}

func sortSceneOutlineByNumber(items []EpisodePlanSceneOutlineItem) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].SceneNumber < items[j].SceneNumber
	})
}
