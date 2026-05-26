package api

import (
	"fmt"
	"strings"

	"kt-ai-studio/internal/models"
)

type llmSpecializedRuleMatch struct {
	Tags  []string
	Block string
}

func buildScenePromptSpecializedRuleMatch(scene models.Scene) llmSpecializedRuleMatch {
	contextText := buildScenePromptContextText(scene)
	tags := detectSceneLLMContextTags(contextText, len(scene.Characters) == 2)
	if len(tags) == 0 {
		sceneName := strings.TrimSpace(scene.Name)
		if sceneName == "" {
			sceneName = "未命名镜头"
		}
		Log(LogLevelWarn, "LLM 场景提示词专属优化未命中", fmt.Sprintf("scene=%d episode=%d scene_number=%d name=%s", scene.ID, scene.Episode, scene.SceneNumber, sceneName))
		return llmSpecializedRuleMatch{}
	}

	Log(LogLevelInfo, "LLM 场景提示词专属优化命中", fmt.Sprintf("scene=%d tags=%s", scene.ID, strings.Join(describeSceneLLMContextTags(tags), "，")))
	return llmSpecializedRuleMatch{
		Tags:  tags,
		Block: renderScenePromptSpecializedRuleBlock(tags),
	}
}

func buildScenePromptContextText(scene models.Scene) string {
	var builder strings.Builder
	builder.WriteString(scene.Name)
	builder.WriteString("\n")
	builder.WriteString(scene.Description)
	builder.WriteString("\n")
	builder.WriteString(scene.VideoFingerprint)
	builder.WriteString("\n")
	for _, char := range scene.Characters {
		builder.WriteString(char.Name)
		builder.WriteString("\n")
		builder.WriteString(char.FaceFingerprint)
		builder.WriteString("\n")
		builder.WriteString(char.Fingerprint)
		builder.WriteString("\n")
	}
	return builder.String()
}

func detectSceneLLMContextTags(contextText string, hasTwoPersonShot bool) []string {
	text := strings.ToLower(contextText)
	tags := make([]string, 0, 8)

	addTag := func(tag string, cond bool) {
		if !cond {
			return
		}
		for _, existing := range tags {
			if existing == tag {
				return
			}
		}
		tags = append(tags, tag)
	}

	addTag("modern_urban", containsAny(text,
		"现代", "都市", "公交", "车厢", "司机", "站台", "雨棚", "办公室", "电梯", "楼道", "病房", "西装", "手机", "车牌",
	))
	addTag("bus_scene", containsAny(text,
		"公交", "公交车", "车厢", "站台", "雨刮", "挡风玻璃", "驾驶区", "驾驶台", "方向盘", "后视镜", "司机",
	))
	addTag("driver_area", containsAny(text,
		"驾驶区", "驾驶台", "驾驶位", "司机", "方向盘", "前挡风玻璃", "后视镜",
	))
	addTag("tight_space", containsAny(text,
		"驾驶区", "车厢", "过道", "电梯", "窄", "狭窄", "楼道", "走廊", "小房间", "驾驶舱", "吧台内侧",
	))
	addTag("outside_observer", containsAny(text,
		"窗外", "门外", "站台外", "雨棚下", "街对面", "楼下", "远观", "远处", "车外", "云上",
	))
	addTag("anonymous_background", containsAny(text,
		"剪影", "人影", "模糊", "伞下", "遮住", "遮挡", "看不清", "不露清晰正脸", "只露腿部", "匿名", "背光",
	))
	addTag("carrier_closeup", containsAny(text,
		"车牌", "车灯", "后视镜", "方向盘", "门把", "窗框", "扶手", "仪表盘", "座椅边角", "超近景", "局部特写",
	))
	addTag("two_person_shot", hasTwoPersonShot)

	return tags
}

func describeSceneLLMContextTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	labels := make([]string, 0, len(tags))
	for _, tag := range tags {
		labels = append(labels, sceneLLMContextTagLabel(tag))
	}
	return labels
}

func sceneLLMContextTagLabel(tag string) string {
	switch tag {
	case "modern_urban":
		return "现代都市"
	case "bus_scene":
		return "公交车场景"
	case "driver_area":
		return "驾驶区"
	case "tight_space":
		return "紧凑空间"
	case "outside_observer":
		return "内外空间观察位"
	case "anonymous_background":
		return "匿名背景人影"
	case "carrier_closeup":
		return "载体局部特写"
	case "two_person_shot":
		return "双人镜头"
	default:
		return tag
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func renderSceneBatchSpecializedRuleBlock(tags []string) string {
	if len(tags) == 0 {
		return ""
	}

	lines := []string{"当前批次命中的专属优化规则（只对本批次生效，你必须直接落实到 description、fingerprint、prompt_pos_screen_zh 和 video_fingerprint 中）："}
	for index, tag := range tags {
		lines = append(lines, fmt.Sprintf("%d. %s", index+1, sceneBatchRuleForTag(tag)))
	}
	return strings.Join(lines, "\n")
}

func renderScenePromptSpecializedRuleBlock(tags []string) string {
	if len(tags) == 0 {
		return ""
	}

	lines := []string{"当前镜头命中的专属优化规则（只对这一镜生效，你必须直接落实到 prompt_pos_screen_*、prompt_neg_screen_* 和 player_desc 中）："}
	for index, tag := range tags {
		lines = append(lines, fmt.Sprintf("%d. %s", index+1, scenePromptRuleForTag(tag)))
	}
	return strings.Join(lines, "\n")
}

func sceneBatchRuleForTag(tag string) string {
	switch tag {
	case "modern_urban":
		return "当前批次属于现代都市语境时，只能使用现代交通工具、现代服装、现代室内陈设、现代灯光与现代城市设施的表达，不要混入古风、仙侠、年代错置或超现实器物。"
	case "bus_scene":
		return "如果当前批次持续发生在同一辆公交车或同一类公交车镜头链中，你必须继承同一辆车的车身主色、车型轮廓、车灯色温、车窗/车门/轮拱关系与车内外对应方向；建立镜头出现过的外观常量，后续局部特写与车内镜头都必须继续保留。"
	case "driver_area":
		return "如果当前批次命中公交车或车辆驾驶区，司机必须固定在驾驶座，方向盘和前挡风玻璃始终在司机正前方，过道只能位于驾驶区后方或侧后；乘客或对话者只能出现在物理允许的位置，不能跑到挡风玻璃外、与司机并坐在不可能的位置，或突然坐进后排和司机同框对视。"
	case "tight_space":
		return "如果当前批次发生在驾驶区、电梯、车厢过道、狭窄楼道、小办公室等紧凑空间，你必须减少单镜承载量，让 description 与 fingerprint 只保留物理成立的少量人物和动作；不要为了信息量把人物写进互相穿模、重叠或不可能站立的位置。"
	case "outside_observer":
		return "如果当前批次涉及窗内看窗外、门内看门外、车内看站台、楼上看楼下或云上看地面，必须把内外空间严格分开：当前主空间里的人物留在原空间，外部元素按外部比例与遮挡关系成立；需要看清外部元素时，优先拆成新的观察镜头或 cutaway。"
	case "anonymous_background":
		return "如果当前批次里的外部人影、伞下人影、背光人形、远处剪影只是匿名比例锚点，就必须保持匿名、遮挡、模糊、剪影级存在，不得把它们补成绑定正式角色的清晰脸，也不得因为参考图污染成长得像主角。"
	case "carrier_closeup":
		return "如果当前批次里出现车牌、车灯、后视镜、方向盘、门把、窗框、扶手、招牌或其它局部特写，你必须写清它附着在哪个已建立的大载体上，并保留一小圈载体常量，例如车头主色、车灯边缘、车漆反光或周围金属结构；不要把局部写成孤立静物。"
	case "two_person_shot":
		return "如果当前批次有双人镜头，两名绑定角色都必须分别落出主要可见锚点：脸部局部、体态轮廓、固定服装结构/主副配色、稳定装备归属中的大部分内容；不能只强化第一人、弱化第二人，也不能让第二个人只剩“听者/对面那人/陪体”这类弱描述。"
	default:
		return "请把当前命中的专属上下文直接翻译成更具体、更物理成立、更可执行的场景描述，不要让模型自行猜测。"
	}
}

func scenePromptRuleForTag(tag string) string {
	switch tag {
	case "modern_urban":
		return "当前镜头属于现代都市语境时，prompt_pos_screen_* 与 player_desc 只能使用现代人物、现代车辆、现代站台、现代办公室、现代病房、现代楼道等可见元素，不要混入古风、仙侠、年代错置或奇幻器物。"
	case "bus_scene":
		return "如果当前镜头属于同一辆公交车的连续镜头，prompt_pos_screen_* 与 player_desc 必须继续保留同一辆车的车身主色、车头轮廓、车灯色温、车窗/车门关系与车内外对应方向，不能让同一辆车在下一镜突然变色或换车型。"
	case "driver_area":
		return "如果当前镜头发生在驾驶区，prompt_pos_screen_* 与 player_desc 必须直接写明司机固定在驾驶座、方向盘和前挡风玻璃在正前方、过道位于驾驶区后方或侧后；不要把人物写到挡风玻璃外，也不要让司机和乘客处在物理上不可能的并坐或对视位置。"
	case "tight_space":
		return "如果当前镜头属于紧凑空间，prompt_pos_screen_* 与 player_desc 必须强调空间边界和有限站位，让人物、家具、方向盘、扶手、玻璃、门框等关系物理成立；不要为了多信息把人物挤进不可能站立、坐下或转身的位置。"
	case "outside_observer":
		return "如果当前镜头是窗内看窗外、门内看门外、车内看站台或远处观察镜头，prompt_pos_screen_* 与 player_desc 必须把主空间和外部空间分开描述：主空间人物留在原位，外部元素按外部距离和遮挡成立；不要让内外人物跨越不可能的边界。"
	case "anonymous_background":
		return "如果当前镜头里的窗外伞下、雨棚下、远处背光或门外人影只是匿名背景，prompt_pos_screen_* 与 player_desc 必须直接写成匿名、遮挡、模糊、剪影级存在，不得出现清晰正脸，也不得长成绑定正式角色的脸。"
	case "carrier_closeup":
		return "如果当前镜头是车牌、车灯、后视镜、方向盘、门把、窗框、扶手等局部特写，prompt_pos_screen_* 与 player_desc 必须写清它附着在同一辆车、同一张桌、同一扇门、同一面窗或同一套固定载体上，并保留周围一小圈载体常量，避免模型重新发明另一件物体。"
	case "two_person_shot":
		return "如果当前镜头有 2 名正式角色，prompt_pos_screen_* 与 player_desc 必须把两个人分别写开：各自的脸部/体态/服装/装备强锚点都要保留，并写清左右、前后、朝向、遮挡和视觉主次；不能只让第一张参考图支配第二个人。"
	default:
		return "请把当前命中的专属上下文直接落实成静态场景图可见锚点，不要留给模型自行脑补。"
	}
}
