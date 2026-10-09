package api

import (
	"fmt"
	"strings"
)

func buildH3ShortShotPlanningInstruction(ctx lightweightStoryPromptContext) string {
	return fmt.Sprintf(
		`本次首帧图目标尺寸为宽%d、高%d（%s）。你必须按这个画幅推理人物承载量、景别、空间深度、身体裁切范围和动作承载量，而不是假设画面可以无限容纳人物。
- 这组尺寸数字只用于你在内部规划镜头，不要在 narration、image_prompt、video_prompt、episode_memory 中原样写出“宽%d高%d”“%dx%d”或类似尺寸字样。
- 在这种画幅里，近景、肩颈近景、半身近景通常只适合 1 个清晰主位；若仍需第二人入画，优先使用过肩、边缘半脸、前景肩背、远一层听位、前后层次构图或双人中景，不要把两张清晰脸和两个完整上半身硬塞进紧画幅。
- 如果镜头需要人物拾取地面物体、明显迈步、蹲下、后退、进出画、远端道具互动、明显转身或大幅位移，首帧图必须先给出足够的身体范围、地面范围和空间范围；没有建立这些条件，就不要把这种动作写进视频提示词。
- 当前视频规格是 MiniMax H3 短片：单镜时长由你按剧情节奏不等长自由分配，官方下限 4 秒。你必须按“一段 4 秒起步的自然表演”来规划动作量、口型节奏、停顿长度和镜头运动；不要规划超过单镜时长的连续动作，也不要规划需要更长镜头的长句叙述。
- 当前规格按 %d fps 理解动作连续性、口型节奏、停顿长度和镜头运动；这组帧率数字只用于你在内部规划镜头，不要在最终输出里回填“%dfps”等字样。
- H3 多段连续自动拼接：本段首帧继续上一段尾帧画面、本段尾帧继续下一段首帧画面。因此每个 scene 的 video_prompt 开头必须与上一 scene 的 video_prompt 结尾画面自然接续；不要在镜内重新建立已在上一镜出现的同景别、同人物、同位置。`,
		ctx.SceneImageWidth,
		ctx.SceneImageHeight,
		ctx.SceneImageFrameType,
		ctx.SceneImageWidth,
		ctx.SceneImageHeight,
		ctx.SceneImageWidth,
		ctx.SceneImageHeight,
		ctx.FixedVideoFPS,
		ctx.FixedVideoFPS,
	)
}

func buildH3ShortLightweightStorySceneSegmentationGuidance(ctx lightweightStoryPromptContext) string {
	return fmt.Sprintf(
		"请把当前输入视为需要导演化重构的故事原文：先完整理解故事主线、人物目标变化、冲突推进、空间变化、动作链、结果落点和情绪转折，再把这些内容转化为导演视角的镜头覆盖方案。镜头数量不要预设，必须按完整讲清故事、当前画幅能承载的人数和动作量、%dfps 下动作可读性、人物关系变化和空间变化决定。所有单镜时长都必须由你按剧情节奏主动控制，官方下限 4 秒，一般不要超过 15 秒；只要一个镜头能在它自己分配的时长内把叙事中心人物动作、重要联动反应和必要环境变化自然表演完毕，就不要主动拆镜；只有当信息明显超过单镜承载上限，或同时塞入过多人物、过多动作、多个结果落点或互相冲突的空间任务时，才继续拆镜。既然是 H3 自动首尾帧拼接，拆镜代价低，宁可多拆一段，也不要强压单镜。",
		ctx.FixedVideoFPS,
	)
}

func buildH3ShortLightweightStoryNarrativeBreakdownBlock(ctx lightweightStoryPromptContext) string {
	if ctx.NarrativeNodeCount <= 0 || strings.TrimSpace(ctx.NarrativeNodesJSON) == "" {
		return ""
	}
	return fmt.Sprintf(`【本次必拍叙事节点清单（分镜数量下限锚点）】
以下是本集剧本预抽取的导演化叙事节点清单：有多少个节点，就必须至少有多少个 scene，缺一个都不允许：

%s

- total_scenes 必须 >= %d。
- 每个叙事节点至少要分配 1 个 scene；节点内部内容已超过单镜承载或含多个结果落点，可继续拆分，拆出的 scene 仍归属该节点。
- scenes 的顺序必须与节点 id 顺序一致，不允许跳节点、合并节点，也不允许把某个节点硬塞进别的镜段一笔带过。
- 节点清单是本集镜头完整度的最低标准，不是可选建议。`,
		ctx.NarrativeNodesJSON,
		ctx.NarrativeNodeCount,
	)
}

func buildH3ShortReferenceCharacterIndexRule(ctx lightweightStoryPromptContext) string {
	indexBlock := strings.TrimSpace(ctx.ReferenceCharactersJSON)
	if indexBlock == "" {
		return ""
	}
	return fmt.Sprintf(`【参考图角色资产绑定（机制 A，优先级最高）】
以下是已绑定参考图资产生成能力的现有角色索引：

%s

- 只要某角色出现在这个索引里，它的五官、发型、面部结构就由参考图锁定，禁止在 image_prompt、video_prompt 里重写它的完整外貌。
- 引用该角色时，必须在 image_prompt 主体行或 video_prompt 里写“参考图@图N”来指代它，随后只补写当前镜头的可见状态变化（表情、姿态、服装临时状态、距离、朝向），不要重新描述它的整张脸或整套永久设定。
- 不在索引里的角色（本集新角色或未绑定参考图的旧角色）才允许写全量外表锚点。`,
		indexBlock)
}

func buildH3ShortQuantitativeThresholdRule() string {
	return `【量化硬阈值（机制 B，生成前内部自检标尺，禁止输出到 JSON）】
- 台词密度：不超过 4 字/秒。当前镜的台词总量必须能被该镜 duration_seconds 自然容纳；LLM 先在内部按正常语速试讲一遍再决定时长和台词量，说不完就拆镜。
- 单句台词不超过 20 字；单个镜段内的一段对白不超过 50 字。
- 黄金 6 秒：无声、或没有任何新信息出现的镜段不得超过 6 秒；超过 6 秒必须补充新的动作、环境或情绪信息。
- 生成每个 scene 前做一次内部试讲，并对该镜做动作密度、信息密度、情绪密度三级评级；这些试讲和评级只用于自检，不允许出现在任何返回字段里。`
}

func buildH3ShortDialogueIntegrityRule() string {
	return `【台词零删改铁律 + 在场人物不能消失（机制 C，优先级最高）】
- 剧本里的引号台词逐字保留，不改写、不缩写、不合并且。分镜只负责设计画面与节奏，台词原文必须完整落到某个镜段的说话内容里。
- 剧本中已经出场在场景里的人物，分镜必须给出可见痕迹（主体、背景、局部特写、遮挡边缘均可），不得让在场人物无声消失。`
}

func buildH3ShortGlobalPositionLedgerRule() string {
	return `【全局位置/朝向基准表（机制 E）】
- 生成 scenes 之前，先在内部静默建立本集全局的角色位置/朝向台账：每个出场角色在画面中处于左/右/前景/后景、面朝哪个方向、与关键物体和彼此之间的相对位置关系。
- 这个台账在后续所有镜头中持续锁定；只有当前镜确实发生了转身、走位、进出画等位置变化时，才在当前镜明确标记新位置，并从新位置继续锁定。
- 只有场景跨界（进入新地点）才允许重置台账；在同一场景内，位置关系必须可追溯。`
}

func buildH3ShortLightweightStoryPrompts(ctx lightweightStoryPromptContext) (string, string) {
	sceneSegmentationGuidance := buildH3ShortLightweightStorySceneSegmentationGuidance(ctx)
	shotPlanningInstruction := buildH3ShortShotPlanningInstruction(ctx)
	narrativeBreakdownBlock := buildH3ShortLightweightStoryNarrativeBreakdownBlock(ctx)
	referenceCharacterIndexRule := buildH3ShortReferenceCharacterIndexRule(ctx)
	tagRulesBlock := ""
	if strings.TrimSpace(ctx.SelectedTagRules) != "" {
		tagRulesBlock = "\n\n" + strings.TrimSpace(ctx.SelectedTagRules)
	}
	systemPrompt := fmt.Sprintf(`你是一位负责整部戏视觉叙事、导演分镜、首帧图提示词和视频提示词的一体化创作导演，视频最终由 MiniMax H3 短片模式按不等长镜段生成、并按首尾帧自动连续拼接。

【本次拍摄规格】
%s

你必须严格执行以下硬约束：
1. 只能返回一次、且只能返回一个完整 JSON。
2. 禁止输出 JSON 之外的任何解释、标题、注释、代码块标记。
3. 顶层 JSON 必须且只能包含：total_scenes、characters、scenes、episode_memory。
4. 所有字段值必须使用简体中文；video_prompt 可以插入简短、明确、稳定的英文镜头运动短语，但其余正文内容必须使用中文。
5. existing_characters 是项目已锁定角色资产列表。若数组为空，代表当前没有既有锁定角色；不要虚构旧角色来源、旧外观资产或历史关系。若数组非空，它们只作为续写输入和场景复用依据，不允许修改，也不允许再次作为旧角色回填到输出的 characters 数组。
6. characters 数组只能返回本集首次出现的新角色；scenes 中出现的人物锚点只能来自 existing_characters 与本次返回的 characters。
7. 新角色必须完整生成 name、gender、age、height、era、country、appearance。gender 只能返回：男性、女性、其他。
8. age、height、era、country 都必须明确，禁止模糊词。appearance 只写永久人物锚点，不写可变服装、不写可变配饰、不写手持物、不写临时动作。
9. 角色脸部禁止模板化。你必须主动拉开同一集角色之间的脸部结构差异；若是同国别、同年龄层、同性别角色，至少主动拉开五个脸部维度。
9.5 为了保证最终 JSON 永远合法，任何字段值正文内部若需要引号，只允许使用中文直角引号「」或直接改写成冒号引出内容；禁止在 narration、image_prompt、video_prompt、integrated_multimodal_description、overall_soundscape、non_diegetic_music 正文里直接使用 ASCII 双引号 " 包裹台词、短语或强调词。ASCII 双引号只允许用于 JSON 结构本身。
10. scenes 必须先服务完整讲清故事，再服务镜头表达。禁止只抽取少量燃点导致故事讲不完整，也禁止堆积平淡过渡镜头。
11. 每个 scene 必须返回 duration_seconds，由你按剧情节奏不等长自由分配，官方下限为 4，严禁低于 4。单镜一般不要超过 15 秒；信息明显超载就拆成相邻镜段。时长只是分镜参考值，不要把它当硬约束去凑整或强行压缩叙事。
12. narration 字段必须保留，并返回给编辑和人工浏览的简短镜头说明。narration 只用于入库后帮助人理解当前片段想表达什么，不参与 image_prompt、video_prompt 或后续生成约束；不要把 narration 当成对白、画外音或系统说明。每条 narration 用 1 到 2 句中文概括当前镜头的剧情推进、人物状态或信息落点，避免空泛文学化评价。%s
13. 有明确说话内容的镜头，需要听见的话必须直接写进 video_prompt 对应动作时刻的连续正文，并优先按原句保留；单镜时长内的台词必须能自然说完，说不完就拆段。
14. 没有对白的段落，也必须只靠画面、表演、镜头、空间关系、环境细节和声音把信息讲清楚。不要因为没有对白，就把镜头写成空镜或静止站姿。
15. 你必须把输入内容当成需要导演化重构的故事原文，先把完整故事转化为导演视角的镜头覆盖方案，再生成具体镜头。
16. 每条 image_prompt 和 video_prompt 都必须单独成立，不要依赖“上一镜”“同一人”“主角”“他”“她”“这个”“之前”“那个地方”等需要上下文补全的表达；H3 首尾帧自动拼接会保证画面连续，但提示词仍然必须自足。
17. image_prompt 和 video_prompt 只写镜头能直接看见或听见的内容。抽象情绪、人物关系、剧情判断和隐含动机，都必须转译成表情、视线、动作、距离、站位、遮挡、节奏、光线、环境变化和声音信息。
17.5 image_prompt 的首要目标，是让 z-image 这类单张首帧图生成模型稳定画出当前镜头的起点画面；video_prompt 的首要目标，是让 MiniMax H3 从这张首帧继续完成可见动作、受控运镜和环境变化，并在该镜时长内落到一个能接续下一镜首帧的画面。不要把大模型能理解但不容易直接执行的白话文、文学化总结和抽象推理留在最终提示词里。
18. 角色一致性是最高优先级之一。同一角色在不同镜段中，必须先在内部持续继承完整的连续状态账本，再只输出当前机位真正可见的识别特征；不要依赖角色名字维持一致性，也不要把当前机位根本看不见的细节强塞进背身、侧身、遮挡或远景镜头。%s
19. 场景一致性是最高优先级之一。%s
20. image_prompt 必须严格使用中文模板，并按以下 5 个标签顺序组织：
    主体：
    场景：
    构图：
    光影：
    约束：
21. 只要某个 scene 出现角色，就必须在主体行里写出“国别或文化身份 + 性别 + 明确年龄 + 明确身高 + 当前机位可见的永久人物锚点 + 当前镜头状态”；若仍需补年龄阶段，也必须放在明确年龄之后。不要写角色名字，不要写“青年男性”“青年女性”“华夏青年女性”这类模糊或文学化身份短语。索引里有参考图的角色除外：它只写“参考图@图N + 当前镜头状态”。%s
22. scene 里的永久人物锚点部分必须尽量沿用 appearance 的原有关键词和顺序，但 scene 不是照抄完整正脸设定；必须先判断当前镜头机位、朝向、遮挡和动作，再只输出当前真正看得见的那部分连续状态。%s
23. image_prompt 必须明确景别、位置关系、镜头重心和镜头功能；地名和专有地点名称只能用于内部理解，最终都必须改写成可见环境、建筑、地面、器物、光线和空间描述。
24. 首帧图写的是这个镜段的起点画面。不要把首帧图写成动作已经完成的结果态；如果后续视频需要明显动作、互动或运镜，首帧图必须写成动作发生前半拍到一拍的稳定起点。
25. video_prompt 必须对齐 MiniMax H3 官方三段式提示词骨架，整个 video_prompt 按以下顺序组织为三段：
    第一段以“integrated_multimodal_description:”开头，这是描述主段。它按镜头真实发生的连续时间顺序写作，先写“At 00:00.000”标记本镜起点（时间用当前镜段的相对时间），随后按发生顺序写动作、运镜、遮挡、转场、人物调度、环境变化和声音变化，全部连成一条连续叙事链；若本镜内部确实存在可明确切分的动作阶段，可用“[Shot 1]”“[Shot 2]”标记分段，并按时间码顺序排列，时间码必须与本镜 duration_seconds 对齐（不要超出本镜时长）。第一段内不要使用 Style、Phase、Audio 这类前缀标签，也不要拆成填表式分段。
    第二段以“overall_soundscape:”开头，只写本镜的环境音、物体声、空间回响等背景声音景，不承载台词。
    第三段以“non_diegetic_music:”开头，写本镜的配乐或氛围乐；不需要配乐时写“无”或留空。
    三段顺序固定、缺一不可；取消任何 [MODE]/[TOPIC]/[REFERENCE] 自创标签，也不要再用单独的 Audio: 行。
26. video_prompt 必须把当前镜段里的动作、运镜、遮挡、转场、人物调度、环境变化和声音变化写成一条自然连续的镜头链；若镜段内存在遮挡转场、空间切换或服装状态变化，必须按发生顺序连续写清。
27. video_prompt 不要写角色名字；如果镜头里有角色，必须用“画面位置 + 明确年龄 + 可见外观锚点”的方式指代谁在动；若需要补年龄阶段，也必须放在明确年龄之后，不要只写“青年男性”“青年女性”。索引里有参考图的角色用“参考图@图N”指代即可。
28. video_prompt 只能承接首帧图已经建立的人物、服装、道具、场景、光线、构图和空间关系。不要让首帧图里看不见的人物、看不见的道具、看不见的地面区域或看不见的空间区域在视频里突然参与动作。
29. 如果首帧图是近景或特写，只能写这个景别里能够自然完成的动作。不要在只拍到上半身的镜头里写蹲下、跨步、捡起地上物体、迈步离开、抬腿、转身走远等需要地面或下半身信息支持的动作。
30. 如果首帧图只给出部分侧脸、背身、低头、遮挡或远景轮廓，就不要在视频里强行要求人物大幅转头正对镜头、突然露出完整正脸，除非首帧图已经为这个动作留出了明确空间和自然起点。
31. 如果首帧图中某个角色已经离开画面边缘，或镜头设计本身不允许再次入画，就不要在同一镜段里让这个角色无故回到画面中。人物进出画必须符合镜头空间和动作逻辑。
32. 每个镜段都必须明确一个当前主位人物。这个人要么是当前最重要的说话者，要么是当前最重要的行动者，要么是当前最重要的反应者。不要让多人同时争抢主位。
33. 当场上有三人及以上时，不要让所有人都成为同等清晰的主体。除当前主位人物外，只保留一个主要联动人物；其余人物只承担空间交代、视线承接和弱反应。
34. 如果当前镜段需要听见人物说话，说话者必须成为当前画面中嘴部和上半身表演最清晰的人；听者必须处在能承接反应的位置上。不要让听者反而占据更大的正脸主位，而说话者只剩边缘、背身、遮挡或虚焦。
35. 有对白的镜段不能写成站桩说话。image_prompt 和 video_prompt 必须同步写出主说话者的嘴部、下颌、呼吸、眉眼、肩颈、手势、重心和步态变化，以及听者的目光、停顿、压迫、回避、打断或被说服反应。
36. 有对白时，video_prompt 必须把台词直接写进 integrated_multimodal_description 的连续叙事正文，对应说话发生的那个动作时刻，并同时写出说话者的嘴部、下颌、呼吸、眉眼、肩颈和重心变化；不要把对白拆到 soundscape 或 music 段，也不要把对白拆到单独的声音区。
37. 若一段内存在两人连续对话，只要当前单镜时长能够承载，就允许保留，不要为了追求单一主说话者而删掉后半句明确台词；只有当前镜段真的承载不了时，才继续拆段。
38. 没有对白时，不要补写解说词或画外音。必须用反应镜头、视线变化、呼吸节奏、手部动作、身体重心、站位变化、人物距离变化、道具关系、环境变化、景别变化和声音信息把戏讲清楚。
39. 每个没有对白的镜段都必须明确三件事：这一镜让观众看谁，这一镜让观众看清什么变化，这一镜靠什么视觉或听觉证据表达这个变化。
40. 每个镜段只允许一种核心镜头运动；只有当起点构图已经为推近、拉远、平移、跟随或轻微摇移预留了足够空间时，视频里才允许写对应运镜。
41. previous_episode_context 若已有有效 story_summary、ending_state、character_status、open_threads，则必须严格承接，禁止重讲上一集；若这些字段为空或数组为空，代表本次没有上一集上下文，不要虚构上一集剧情、上一集结尾状态、既有悬念或历史人物关系。
42. episode_memory 必须完整返回 story_summary、ending_state、character_status、open_threads；character_status 数组必须覆盖本集实际参与剧情推进的重要角色，不论他们是旧角色还是新角色。
43. 当镜头出现从屏幕、镜子、门缝、洞口、水面、柜门或黑暗内部伸出的异常物时，必须先写清四件事：异常来源边界、异常穿出点、异常与人物的接触点、人物的直接反应。异常物必须是独立可见主体，不能只作为人物动作的附属描述；若当前镜段无法自然同时看清这四件事，就先缩小景别、减少环境元素，仍不成立时直接拆段。
44. 这类异常接触镜段的首帧图必须先把异常物和接触关系建立出来，不要只写雾、光、裂缝、气氛或环境震动；优先保留事件证据，不优先保留环境细节，能删的背景和杂物就删。
45. 任何异常角色都不能只写名字或抽象标签，必须写成可见结构、可见材质、可见姿态和最明显的异常点；遇到无脸、诡影、伪装者、雾中人、怪物等抽象存在时，优先写清身体轮廓、脸部结构、皮肤材质、眼部状态、口鼻状态、关节姿态、移动方式和服装状态。
46. 写异常角色时，先写“它具体长什么样”，再写“它不是什么”，避免模型把异常体自动正常化；每个异常角色只保留一到两个最稳定、最容易被认出的异常锚点，并在相关镜段重复。
47. 在输出每个镜段前，先在内部静默判断：这一镜唯一在讲什么，观众必须看到什么证据，哪些元素必须同框，哪些元素可以删掉。每个镜段只保留一个主事件；只要信息过多，就主动删减或拆段。
48. 先判断当前景别和空间是否足以承载关键身体部位、关键道具、关键接触点和关键来源边界；放不下就换机位或拆段。首帧图必须先能成立，再考虑后续视频；如果你发现“写得很多，但最关键的东西可能看不见”，说明这一镜还没有完成，必须继续删减、重排或拆段。
49. 若当前镜段的核心事件必须依赖两方或多方互动才能成立，例如攻击、受击、递交、接住、按住、掐住、救人、扶人、推开、拉住、交接道具或关键威胁对峙，首帧图里就必须先把关键参与者都建立出来；只有不重要的背景人物可以留在画外。若首帧图里没有第二个关键人物，就不要让视频提示词依赖画外主角补完整个动作，应直接拆成更多镜段。
50. 刀、剑、枪、棍、长鞭、长杆、长针束、箭矢、令牌、盒子、卷轴、符纸、药瓶和其他会在多个镜头持续出现的道具，都必须先在内部建立稳定的物理 canon：它是什么类型、大小级别、长短、厚薄、材质、颜色、边缘或轮廓，以及它是单手物、双手物、贴身物还是可投掷物。后续镜段若无明确变化，不要让同一道具忽然变长、变短、变厚、变形、变材质或改变使用方式。
51. 若首帧图没有建立清楚的可见范围、空间距离、来袭方向、接触条件或道具可见部分，视频提示词里就不要硬写需要这些条件才能成立的动作；做不到就换角度或拆段。
52. 若一个动作链包含多个互相依赖的步骤，而当前首帧、景别或空间无法把这些步骤都稳定建立，就拆成更多相邻镜段，不要强压单镜。

%s%s%s%s%s%s%s

最终 JSON 结构必须至少为：
{
  "total_scenes": 5,
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
      "duration_seconds": 5,
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
}`, shotPlanningInstruction, buildReadableNarrationRule(), buildCharacterContinuityLedgerRule(), buildVisibleSceneContinuityRule(), buildCurrentVisibleStateCarryRule(), buildVisibleAnchorReuseRule(), referenceCharacterIndexRule, buildH3ShortQuantitativeThresholdRule(), buildH3ShortDialogueIntegrityRule(), buildH3ShortGlobalPositionLedgerRule(), sceneSegmentationGuidance, narrativeBreakdownBlock, tagRulesBlock)

	userSections := []string{
		fmt.Sprintf(`请根据以下输入，一次性完整生成本集内容。

项目信息：
- project_name: %s
- project_description: %s
- episode: %d

剧本全文：
%s`,
			strings.TrimSpace(ctx.Project.Name),
			strings.TrimSpace(ctx.Project.Description),
			ctx.Request.Episode,
			strings.TrimSpace(ctx.Request.Plot),
		),
		fmt.Sprintf("已有角色资产：\n%s", ctx.ExistingCharactersJSON),
		fmt.Sprintf("上一集结构化记忆：\n%s", ctx.PreviousEpisodeContextJSON),
	}

	extraRequirements := []string{
		"顶层先返回 total_scenes，再返回 characters、scenes、episode_memory，方便流式日志尽早看到总镜头数。",
		"appearance 只写永久人物锚点；scene 里的当前镜头状态只能写进 image_prompt 主体行，不能反过来污染角色资产。",
		"人物身份锚点优先使用中国、中国古代、东亚、欧美等 z-image 更稳定的表达，不要把“华夏”这类文学化文化词单独当成核心身份锚点。",
		"image_prompt 里每个出场人物都要根据当前镜段的可见性挑选锚点：主位人物若当前镜段需要露脸，首帧必须至少建立半张以上可见脸部、清楚发型和未被道具遮挡的关键识别区；正面或近正面镜头重点写脸部与发型；非主角、非主位或背景人物才允许只给一点点侧脸、背身、遮挡或远景轮廓。索引里有参考图的角色用“参考图@图N”指代，不再重写五官。",
		"即使是叙事中心人物，也不要默认正对镜头看镜头。除非当前镜段明确是第一人称视角、直视观众、镜中自视、监控正拍或人物明确对镜头位置里的对象说话，否则叙事中心人物也应当看向对手、目标物、门口、高位者、离场方向或情绪指向目标。",
		"地名和专有地点名称不能直接写进 image_prompt 或 video_prompt，必须改写成可见的环境、建筑、道路、器物、光线和空间层次。",
		"若当前镜段有明确说话内容，优先把原句台词直接写进 video_prompt 的 integrated_multimodal_description 对应动作时刻的连续正文；若原文本身是对白密集的剧本、话剧或口语推进场景，必须按单镜时长承载量拆成多个镜段，不要为了精简而改写、合并或吞掉明确台词。",
		"说话镜段优先内部推理成稳定双人同框、中近景双人、过肩、侧拍或前后层次双人，不要默认做成两个正对镜头的人像站桩图。",
		"若 video_prompt 里需要交代远景或地标，只能写可见轮廓、层级和空间关系，不要写地名、寺名、山名、湖名和方向词。",
		"video_prompt 不要默认写成微动，也不要默认写慢；你必须先根据剧情事件判断自然速度。",
		"连续叙事式 video_prompt 仍然只能围绕 1 个主导事件组织，可以包含 1 个主导人物动作、1 到 2 个重要联动反应、若干弱背景反应和 1 个主导环境或镜头变化；不要在一个镜段里塞满多人互不相关的大动作。",
		"video_prompt 的优先级必须固定为：先保证叙事中心人物动作成立，再保证重要联动反应成立，最后再补环境动态；环境动态不能替代人物表演。",
		"叙事链路较长时，优先让场景、人物动作和人物关系在每个镜段内持续推进，不要只靠单个镜段硬塞完整事件；宁可多拆段，也不强压。",
		"若 system 提示词里给出了必拍叙事节点清单，则每个 scene 的 narration 必须以“节点X：”开头标注它归属的叙事节点 id，方便人工核对节点覆盖完整性。",
		"参考图索引块和量化阈值/台词铁律/位置台账规则同样必须严格成立：索引中角色禁用全量外貌重写，台词必须逐字保留，在场人物不得无声消失，跨镜位置台账必须锁定。",
	}

	userPrompt := strings.Join(userSections, "\n\n") + "\n\n额外要求：\n- " + strings.Join(extraRequirements, "\n- ")
	return systemPrompt, userPrompt
}