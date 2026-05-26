package api

import (
	"fmt"
	"regexp"
	"strings"
)

func buildStoryboardLightweightStorySceneSegmentationGuidance(ctx lightweightStoryPromptContext) string {
	return "当前输入已经是分镜脚本、拍摄脚本或镜头清单。你的任务不是重写故事，而是把每个原镜头翻译成 z-image 能稳定画出首帧、LTX2.3 能从首帧继续生成的视频提示词。它们不理解角色名字、上一镜、文学概括和隐含逻辑，只理解当前提示词内可见或可听的事实。你必须优先沿用原镜号顺序、原场景切换、原对白归属、原机位意图、原景别意图、原动作逻辑和原时间安排，不要把用户已经写好的镜头结构打散重组。若原镜头写了明确时间范围，例如 0-3 秒、34-38 秒，这个原镜头的总时长就是硬约束：若保持为单个 scene，duration_seconds 必须等于该时长；若确实必须拆成相邻多个 scene，这些新 scene 的时长总和也必须等于该原镜头的总时长。若原镜头没有明确时间范围，则 duration_seconds 由你自行判断，范围只能是 3 到 15 秒，并且必须由你主动控制在 15 秒以内，严禁任何形式超过 15 秒；只要 LTX2.3 能在 15 秒内自然表演完毕，就优先在原镜头内压缩视觉描述，不要因为对白、动作、运镜或叙事贪多而主动拆镜。只有当当前镜头的首帧图和后续视频必须与另一位主角形成清晰可见的正面联动，而当前机位又无法同时建立两位主角的可见身份锚点与互动关系时，才允许局部拆分；非主角、背景人物、弱反应人物或画面边缘次要人物，不构成拆镜理由。"
}

var storyboardQuotedDialoguePattern = regexp.MustCompile(`「([^」]+)」|“([^”]+)”|"([^"]+)"`)

var storyboardSpeechHintKeywords = []string{
	"道", "问", "吼", "喃喃", "呵斥", "嘶吼", "冰声", "低声", "沉声",
	"缓缓", "虚弱", "清冷", "冷笑", "说道", "开口", "交代", "吐出",
	"回应", "喊", "呢喃", "轻声", "喝道", "怒喝", "怒道", "沉声道",
}

func extractStoryboardQuotedDialogues(plot string) []string {
	mainPlot := plot
	if idx := strings.Index(mainPlot, "漫剧适配注意事项"); idx >= 0 {
		mainPlot = mainPlot[:idx]
	}

	matches := storyboardQuotedDialoguePattern.FindAllStringSubmatchIndex(mainPlot, -1)
	lines := make([]string, 0, len(matches))
	for _, match := range matches {
		candidate := ""
		start := 0
		if len(match) >= 4 && match[2] >= 0 && match[3] >= 0 {
			candidate = strings.TrimSpace(mainPlot[match[2]:match[3]])
			start = match[2]
		}
		if candidate == "" && len(match) >= 6 && match[4] >= 0 && match[5] >= 0 {
			candidate = strings.TrimSpace(mainPlot[match[4]:match[5]])
			start = match[4]
		}
		if candidate == "" && len(match) >= 8 && match[6] >= 0 && match[7] >= 0 {
			candidate = strings.TrimSpace(mainPlot[match[6]:match[7]])
			start = match[6]
		}
		if candidate == "" {
			continue
		}
		prefixStart := start - 20
		if prefixStart < 0 {
			prefixStart = 0
		}
		prefix := mainPlot[prefixStart:start]
		looksLikeSpeech := strings.Contains(prefix, "：") || strings.Contains(prefix, ":")
		if !looksLikeSpeech {
			for _, hint := range storyboardSpeechHintKeywords {
				if strings.Contains(prefix, hint) {
					looksLikeSpeech = true
					break
				}
			}
		}
		if !looksLikeSpeech {
			continue
		}
		lines = append(lines, candidate)
	}
	return lines
}

func buildStoryboardDialogueChecklist(plot string) string {
	lines := extractStoryboardQuotedDialogues(plot)
	if len(lines) == 0 {
		return "源脚本中未检测到明确引号台词。"
	}
	items := make([]string, 0, len(lines))
	for idx, line := range lines {
		items = append(items, fmt.Sprintf("%d. %s", idx+1, line))
	}
	lastLine := lines[len(lines)-1]
	return "源脚本中的明确引号台词清单（必须按顺序落到某个 scene 的说话内容里，不要漏掉，尤其不要漏掉最后一句）：\n" +
		strings.Join(items, "\n") +
		"\n特别提醒：源脚本最后一句明确台词是「" + lastLine + "」。这句必须在最终结果中真实出现，并且要落在全剧最后一个时序位置的说话内容里，不能被更早的闪回台词、提问台词或解释台词顶掉。"
}

func buildStoryboardLightweightStoryPrompts(ctx lightweightStoryPromptContext) (string, string) {
	sceneSegmentationGuidance := buildStoryboardLightweightStorySceneSegmentationGuidance(ctx)
	shotPlanningInstruction := buildHighQualityShotPlanningInstruction(ctx)
	tagRulesBlock := ""
	if strings.TrimSpace(ctx.SelectedTagRules) != "" {
		tagRulesBlock = "\n\n" + strings.TrimSpace(ctx.SelectedTagRules)
	}

	systemPrompt := fmt.Sprintf(`你是一位把现成分镜脚本翻译成首帧图提示词和 LTX 视频提示词的执行导演兼视觉翻译器。

【本次拍摄规格】
%s

	你必须严格执行以下硬约束：
	1. 只能返回一次、且只能返回一个完整 JSON。
	2. 禁止输出 JSON 之外的任何解释、标题、注释、代码块标记。
	3. 顶层 JSON 必须且只能包含：total_scenes、characters、scenes、episode_memory。
	4. 这个模式不是让你重写故事，而是把现成分镜脚本翻译成 z-image 首帧图和 LTX2.3 视频都能执行的视觉语言。
	5. 你必须优先保留原镜号顺序、原对白归属、原场景切换、原机位意图、原景别意图、原动作逻辑、原叙事节奏和原时间安排；不要把输入脚本改写成另一套分镜，也不要删掉原脚本已经明确写出的关键动作、关键反应和明确台词。
	6. characters 数组只返回本集首次出现的新角色；existing_characters 里的旧角色不能重复回填。
	7. 新角色必须完整返回 name、gender、age、height、era、country、appearance；gender 只能返回：男性、女性、其他。age、height、era、country 都必须明确，不要用“20岁出头”“30多岁”“高挑”“偏高”这种模糊说法；这几个字段在 JSON 里也必须是字符串，哪怕看起来像数字，也要写成例如 24、178cm 这样的字符串形式，而不是 JSON 数字。
	8. appearance 只写永久人物锚点，不写当前镜头服装、不写当前镜头伤口、不写当前镜头手持武器、不写当前镜头动作、不写临时血迹、不写临时道具，不要把剧本里的当场状态直接塞进角色资产。
	8.5 为了保证最终 JSON 永远合法，任何字段值正文内部若需要引号，只允许使用中文直角引号「」或直接改写成冒号引出内容；禁止在 narration、image_prompt、video_prompt、Audio 正文里直接使用 ASCII 双引号 " 包裹台词、短语或强调词。ASCII 双引号只允许用于 JSON 结构本身。
	9. narration 字段必须保留，并返回给编辑和人工浏览的简短镜头说明。narration 只用于入库后帮助人理解当前片段想表达什么，不参与 image_prompt、video_prompt、Audio 或后续生成约束；不要把 narration 当成对白、画外音或系统说明。每条 narration 用 1 到 2 句中文概括当前镜头的剧情推进、人物状态或信息落点，避免空泛文学化评价。%s
	10. 当前镜头若需要听见角色说话，需要听见的话必须直接写进 video_prompt 对应动作时刻的连续正文，并尽量按原顺序保留原句。
	11. 输入脚本里凡是已经明确写出的直接台词、引号台词、冒号后直说内容，都必须尽量按原顺序保留到某个 scene 的说话内容或当前镜头对应的表演中；除非脚本明确写出改成沉默、停顿、无声反应或纯动作表达，否则禁止删除明确台词。
	12. image_prompt 和 video_prompt 只写镜头能直接看见或听见的内容。任何白话文、文学化表达、抽象情绪、行业黑话、剧情判断、镜头意图说明，只要不能被相机直接看见或听见，就必须改写成表情、视线、动作、距离、站位、遮挡、景别、构图、光线、环境变化、口型、呼吸、停顿和声音；改写不了就删除。
	12.5 image_prompt 的首要目标，是让 z-image 这类单张首帧图生成模型稳定画出当前镜头起点；video_prompt 的首要目标，是让 LTX2.3 从这张首帧继续完成可见动作、接触、运镜和结果反应。不要把大模型能理解但 z-image 或 LTX2.3 不容易执行的白话文、文学化概括和创作备注留在最终提示词里。
	13. 不要在任何输出字段里暴露你的内部推理、执行说明或规则解释；不要写“本镜”“当前镜头不符合”“因此拆为”“同一镜允许”“为了满足约束”等元说明，只输出最终可执行结果。
	14. 每条 image_prompt 和 video_prompt 都必须单独成立，不要依赖“上一镜”“同一人”“主角”“他”“她”“这个”“之前”“那个地方”等需要上下文补全的表达。
	15. 人物一致性、场景一致性、道具一致性和空间关系一致性优先级最高。%s 同一角色必须稳定复用当前机位真正可见的识别锚点。
	16. image_prompt 必须严格使用中文模板，并按以下 5 个标签顺序组织；其中“约束”标签只允许写画幅、可见范围、必须看见的接触点、禁止出现项和必须保留的视觉事实，不要写“本镜以……为主”“突出……”或“强化……”这类创作说明或元说明：
	    主体：
	    场景：
	    构图：
	    光影：
	    约束：
	17. 整个 image_prompt 的任何一行都不要写角色名字。主体、场景、构图、光影、约束都只能使用国别或文化身份、性别、明确年龄、身高、当前机位真正可见的稳定锚点、画面位置、站位关系和当前镜头状态来指代人物；若仍需补年龄阶段，也必须放在明确年龄之后。不要写“青年男性”“青年女性”“华夏青年女性”这类模糊或文学化身份短语。%s
	18. 首帧图写的是这个镜头的起点画面。后续视频需要明显动作、互动、遮挡、转场或运镜时，首帧图必须先建立动作起点、人物位置、空间范围和当前机位真正可见的关键视觉事实；不要把关键条件留给视频补。
	19. video_prompt 必须使用连续叙事式结构：前面是一段按时间顺序连续推进的镜头正文，最后单独一行使用固定标签 Audio: 收束背景音、环境音、物体声和空间回响。
	20. video_prompt 不要使用 Style、Phase、音效分段模板；不要拆成填表式结构。
	21. video_prompt 不要写角色名字；如果镜头里有角色，必须用“左侧/右侧/前景/后景/近处/远处 + 明确年龄 + 可见外观锚点”的方式指代谁在动；若需要补年龄阶段，也必须放在明确年龄之后，不要只写“青年男性”“青年女性”。
	22. video_prompt 只能承接首帧图已经建立的人物、服装、道具、场景、光线、构图和空间关系。不要让首帧图里看不见的人物、道具、地面区域或空间区域在视频里突然参与动作。
	23. 若原镜头写了明确时间范围，必须优先把视觉描述压缩进这个时间内；不要因为信息很多就主动延长时长。
	24. 只有当当前镜头的首帧图和后续视频必须与另一位主角形成清晰可见的正面联动，而当前机位又无法同时建立两位主角的可见身份锚点、脸部特征或关键互动关系时，才允许拆成相邻更多 scene。若另一人只是背景人物、次要人物、弱反应人物或不需要清晰脸部特征，就不要因为他不完整可见而拆镜。
	25. 关键道具、手持武器、服装层次、破损、血迹、湿度、包扎、站位关系和光线方向都必须按剧情连续继承；没有明确剧情变化，不要无缘由改色、换装、换武器、改长度、改材质或改变携带方式。%s
	26. previous_episode_context 若已有有效 story_summary、ending_state、character_status、open_threads，则必须严格承接；若为空，不要虚构上一集剧情、上一集结尾状态或既有人物关系。
	27. episode_memory 必须完整返回 story_summary、ending_state、character_status、open_threads；character_status 只覆盖本集实际参与剧情推进的重要角色。

%s%s

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
      "narration": "一句给编辑和人工浏览的简短镜头说明，不参与后续生成。",
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
	}`, shotPlanningInstruction, buildReadableNarrationRule(), buildVisibleSceneContinuityRule(), buildCurrentVisibleStateCarryRule(), buildCharacterContinuityLedgerRule(), sceneSegmentationGuidance, tagRulesBlock)

	userSections := []string{
		fmt.Sprintf(`请根据以下分镜脚本，一次性完整生成本集内容。

- episode: %d

分镜脚本全文：
%s`,
			ctx.Request.Episode,
			strings.TrimSpace(ctx.Request.Plot),
		),
		fmt.Sprintf("已有角色资产：\n%s", ctx.ExistingCharactersJSON),
		fmt.Sprintf("上一集结构化记忆：\n%s", ctx.PreviousEpisodeContextJSON),
	}

	extraRequirements := []string{
		"顶层先返回 total_scenes，再返回 characters、scenes、episode_memory，方便流式日志尽早看到总镜头数。",
		"如果原镜头本身成立，就优先补可执行细节，不要重排镜头。",
		"如果原镜头写了明确时间范围，就把这个时间当成硬约束；优先压缩视觉描述，不要主动延长时长。",
		"只有当首帧必须与另一位主角形成清晰可见的正面联动、而当前机位又无法同时建立两位主角的身份锚点和互动关系时，才允许拆镜；否则优先保留原镜头。",
		"为了保证返回 JSON 合法，任何字段正文内部若需要引号，只使用中文直角引号「」；不要在 narration、image_prompt、video_prompt 或 Audio 正文里直接写 ASCII 双引号 \"。若需要引出台词或原话，优先改写成“低声说出：「某句中文」”或“开口问：「某句中文」”。",
		"整个 image_prompt 的任何一行都不要写角色名字；主体、场景、构图、光影、约束都只能用外观锚点、明确年龄、画面位置和站位关系指代人物。",
		"image_prompt 的“约束”行只能写可执行视觉约束，不要写“本镜以……为主”“突出……”或“强化……”这类元说明。",
		"人物身份锚点要优先使用 z-image 更稳定的表达，例如中国女性、中国古代男性、东亚女性；不要把“华夏”这类文学化文化词单独当成核心身份锚点。",
		"video_prompt 必须写成连续叙事式正文，并在最后单独用一行 Audio: 收束背景音、环境音、物体声和空间回响。",
		"有对白时，video_prompt 必须把台词直接写进对应动作时刻的连续正文，并写出说话者的口型、下颌、呼吸、眉眼、肩颈和听者反应；不要把台词写进 Audio。",
		"video_prompt 不要写角色名字和地名；如果需要指代人物或空间，只能写画面位置、明确年龄、可见锚点和可见环境关系；若需要补年龄阶段，也只能放在明确年龄之后。",
		"如果脚本某句话不能直接被 z-image 或 LTX2.3 理解，就把它翻译成视觉/听觉事实；如果翻译后仍不成立，就删掉。",
		"narration 字段必须保留，并返回给编辑和人工浏览的简短镜头说明；它只用于帮助人理解当前片段想表达什么，不参与 image_prompt、video_prompt、Audio 或后续生成约束，不要把它写成对白、画外音或系统说明。",
		"不要返回任何解释，只返回 JSON。",
	}

	userPrompt := strings.Join(userSections, "\n\n") + "\n\n额外要求：\n- " + strings.Join(extraRequirements, "\n- ")
	return systemPrompt, userPrompt
}
