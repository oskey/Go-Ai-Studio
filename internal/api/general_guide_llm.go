package api

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"

	"github.com/gin-gonic/gin"
	"github.com/sashabaranov/go-openai"
	"gorm.io/gorm"
)

type generalGuidePlanScenesRequest struct {
	Content string `json:"content"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

type generalGuidePlanScenesTaskPayload struct {
	ProjectID uint   `json:"project_id"`
	Content   string `json:"content"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type generalGuidePlannedSceneResponse struct {
	Title               string `json:"title"`
	SceneType           string `json:"scene_type"`
	EnvironmentType     string `json:"environment_type"`
	NeedPresenter       bool   `json:"need_presenter"`
	UploadHeadline      string `json:"upload_headline"`
	ImagePreset         string `json:"image_preset"`
	UploadRequirement   string `json:"upload_requirement"`
	IntroText           string `json:"intro_text"`
	VideoPositivePrompt string `json:"video_positive_prompt"`
	DurationSeconds     int    `json:"duration_seconds"`
}

type generalGuidePlanScenesResponse struct {
	Scenes []generalGuidePlannedSceneResponse `json:"scenes"`
}

const generalGuideMinVideoDurationSeconds = 2
const generalGuideMaxVideoDurationSeconds = 20

func StartGeneralGuideProjectPlanScenes(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	var req generalGuidePlanScenesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式不正确"})
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先填写项目总文案"})
		return
	}
	if req.Width <= 0 || req.Height <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先选择正确的视频尺寸"})
		return
	}
	now := time.Now()
	if err := db.DB.Model(&models.GeneralGuideProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"auto_generate_content":    req.Content,
		"current_planning_task_id": "",
		"last_planning_error":      "",
		"updated_at":               now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存项目总文案失败"})
		return
	}
	payload := generalGuidePlanScenesTaskPayload{
		ProjectID: project.ID,
		Content:   req.Content,
		Width:     req.Width,
		Height:    req.Height,
	}
	t, err := task.GlobalTaskManager.AddTask("plan_general_guide_project", payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交场景规划任务失败"})
		return
	}
	_ = db.DB.Model(&models.GeneralGuideProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"current_planning_task_id": t.ID,
		"last_planning_error":      "",
		"updated_at":               time.Now(),
	}).Error
	c.JSON(http.StatusAccepted, gin.H{
		"message": "场景规划任务已提交",
		"task_id": t.ID,
	})
}

func HandlePlanGeneralGuideProjectTask(t *models.Task) (result interface{}, err error) {
	var payload generalGuidePlanScenesTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}
	projectID := payload.ProjectID
	defer func() {
		if r := recover(); r != nil {
			if projectID != 0 {
				_ = db.DB.Model(&models.GeneralGuideProject{}).Where("id = ?", projectID).Updates(map[string]interface{}{
					"last_planning_error":      fmt.Sprintf("规划任务异常中断: %v", r),
					"current_planning_task_id": "",
					"updated_at":               time.Now(),
				}).Error
			}
			panic(r)
		}
		if err != nil && projectID != 0 {
			_ = db.DB.Model(&models.GeneralGuideProject{}).Where("id = ?", projectID).Updates(map[string]interface{}{
				"last_planning_error":      err.Error(),
				"current_planning_task_id": "",
				"updated_at":               time.Now(),
			}).Error
		}
	}()

	var project models.GeneralGuideProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("综合讲解项目不存在")
	}
	applyGeneralGuideProjectTagIDs(&project)
	var scenes []models.GeneralGuideScene
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&scenes).Error; err != nil {
		return nil, err
	}

	systemPrompt, userPrompt, err := buildGeneralGuidePlanPrompts(project, payload.Content, scenes, payload.Width, payload.Height)
	if err != nil {
		return nil, err
	}

	var provider models.LLMProvider
	if err := db.DB.Where("is_active = ?", true).First(&provider).Error; err != nil {
		return nil, fmt.Errorf("请先配置并启用 LLM 引擎")
	}
	model, err := requireProviderModelName(provider)
	if err != nil {
		return nil, err
	}

	Log(LogLevelInfo, llmLogMessage("LLM Request", provider), fmt.Sprintf("Starting general guide scene planning for project=%d", project.ID))
	Log(LogLevelInfo, llmLogMessage("LLM Request Prompt", provider), fmt.Sprintf("System:\n%s\n\nUser:\n%s", systemPrompt, userPrompt))

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

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 18, "正在调用 LLM 规划综合讲解场景")
	raw, err := requestLLMContentStreaming(provider, req, 12*time.Minute, t.ID, "综合讲解场景规划")
	if err != nil {
		return nil, err
	}
	Log(LogLevelInfo, llmLogMessage("LLM 完整返回(综合讲解场景规划)", provider), raw)

	parsed, err := parseGeneralGuidePlanResponse(raw)
	if err != nil {
		return nil, err
	}
	if err := validateGeneralGuidePlanResponse(parsed); err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 52, "写回综合讲解场景规划")
	if err := persistGeneralGuidePlan(project.ID, payload.Content, parsed, payload.Width, payload.Height); err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 100, "综合讲解场景规划完成")
	_ = db.DB.Model(&models.GeneralGuideProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"current_planning_task_id": "",
		"last_planning_error":      "",
		"updated_at":               time.Now(),
	}).Error

	return gin.H{
		"project_id":  project.ID,
		"scene_count": len(parsed.Scenes),
	}, nil
}

func buildGeneralGuidePlanPrompts(project models.GeneralGuideProject, content string, existingScenes []models.GeneralGuideScene, videoWidth int, videoHeight int) (string, string, error) {
	selectedTags, err := listSelectedGeneralGuideTags(project.TagIDs)
	if err != nil {
		return "", "", err
	}
	tagRules := strings.TrimSpace(buildSelectedGeneralGuideTagRulesBlock(selectedTags))
	systemPrompt := `你是一个专门把“简短项目文案”拆解成综合讲解短视频场景规划的提示词工程助手。

你的任务是根据输入的一段简短说明，返回一组按顺序排列的场景。后续会按这些场景准备对应图片，再用图像合成和图生视频完成整套内容。

你必须严格遵守以下规则：
1. 只能返回 JSON。禁止输出解释、标题、注释、代码块。
2. 顶层 JSON 只能包含 scenes。
3. scenes 必须是数组，通常返回 4 到 8 个场景；如果内容特别简单，可以更少，但不要为了凑数生成冗余场景。
4. 在输出最终答案之前，必须先在内部自检一遍返回内容是否是合法 JSON：检查花括号和方括号是否配对、逗号是否完整、所有字符串是否正确闭合、所有键名和值是否都在正确位置、没有多余解释文字、没有代码块标记、没有乱码字符、没有未转义的双引号。如果不合法，先在内部重写，直到最终输出本身可以被标准 JSON 解析器一次性解析通过。
5. 每个场景对象必须包含：
   - title
   - scene_type
   - environment_type
   - need_presenter
   - upload_headline
   - upload_requirement
   - intro_text
   - video_positive_prompt
   - duration_seconds
   注意：上面这 9 个字段必须真实出现在每一个 scene 对象里，不能只在心里推断，不能省略 duration_seconds，不能缺键，不能把它写到别的字段里代替。
6. 每个 scene 都必须显式输出 duration_seconds 这个键；duration_seconds 不允许缺失，不允许写成 null，不允许写成 0，不允许写成字符串，必须直接写成 2 到 20 的整数。
7. scene_type 只能是 presenter_scene、material_scene、closing_scene 三种之一。
8. environment_type 只能是 indoor 或 outdoor 两种之一。你要自己判断当前场景更像室内还是室外，这个字段会决定后续图像合成的固定预设。
9. image_preset 属于程序内部逻辑，不需要你返回，也不要在正文里提到任何内部预设名。系统会根据 environment_type 和 need_presenter 自动选择更保守的合成方案。
10. need_presenter 为 false 时，代表这一行应该走纯素材直出，不做人物合成。
11. upload_headline 必须是中文短标题，直接点明“这一行应该上传什么图”，要一眼就能看懂，例如“上传主体外立面的正面照片”“上传室内主空间的完整照片”“上传主体近景主图”“上传柜台或接待区的正面照片”。不要写成抽象名词，不要写内部术语。
12. upload_requirement 必须明确告诉这一行该上传什么图片，要求尽量实用、可执行。
13. 上传要求应优先引导上传白天或明亮均匀光线下的图片，不要默认夜景；并提醒画面完整、无遮挡、不要大面积逆光。只有输入明确需要夜景或夜景是卖点时，才允许要求夜景。
14. 如果场景需要讲解人，你要假设后续会把讲解人合成到场景图里。此时更适合上传“空间完整、前景留有讲解人位置、适合半身人物站在前景”的图片。除非输入内容明确要求坐姿、桌边讲解或展示台讲解，否则不要主动把场景设计成坐姿。
15. 如果场景不需要讲解人，就返回 need_presenter=false，并让这一行对应纯素材图片。
16. 首个场景必须是需要讲解人的人物讲解场景，不能把第一个场景规划成纯素材场景，也不要让第一个场景缺少人物。
17. 首个场景在内容允许的情况下应优先规划成 outdoor 的室外开场；只有当输入事实明显不适合室外开场，或根本不存在合理室外入口时，才允许首个场景从 indoor 开始。
18. 如果首个场景已经是 outdoor 的室外开场，第二个场景在内容允许的情况下应优先切入 indoor 的室内主体信息，不要默认连续规划两个室外 presenter_scene。只有当输入事实明显要求继续在室外补充关键信息，或根本不存在合理室内主体内容时，才允许第二个场景继续使用 outdoor。
19. 纯素材场景必须尽量少用，整套 scenes 中最多允许 2 个 need_presenter=false 的纯素材场景；如果内容可以由人物讲解完成，就优先规划成人物讲解场景。
20. 纯素材场景只能承载能通过画面直接理解的视觉信息，例如外立面、主空间、设备、陈列、细节、环境氛围、客观展示。凡是需要靠讲解人口播才能成立的意思，例如转让原因、人物背景、价格是否划算、费用解释、优惠逻辑、付款方式、条件分析、真实性判断、主观结论或说服话术，都不能规划成纯素材场景，必须放到需要讲解人的 presenter_scene 或 closing_scene 中。
21. material_scene 的 intro_text 和 video_positive_prompt 只能描述镜头里看得见的主体、展示重点和视觉目的，不要写成“用素材配合说明……”“通过画面解释……”“用近景说明转让原因……”这类需要后配音或额外口播才能成立的表达。
22. intro_text 只用于快速概括这一行讲什么，必须是简短摘要，1 到 3 句中文，不要写成长篇口播稿。
23. 项目里设置的“讲解人性别”和“讲解人人设”只用于约束需要讲解人出镜的场景里，video_positive_prompt 中说话和出镜的讲解人必须始终与项目设置一致，避免视频生成后出现女性画面却像男性在说话、或男性画面却像女性在说话的违和感。这个性别和人设设置不是让你改写事实本身，也不是让你擅自把文案中的角色关系变成“老板娘本人出镜”“房东本人出镜”“店主本人亲口说”之类。除非输入事实明确要求某个身份本人出镜，否则 title、upload_headline、upload_requirement、intro_text 都不要擅自写成“老板娘亲口说”“房东本人讲”“店主现身介绍”这类身份设定。
24. 只要某个场景 need_presenter=true，video_positive_prompt 的第一句就必须先写清楚前景主体边界，并且主语必须与项目设置的讲解人性别一致。第一句应明确表达“前景中的一位与项目设置性别一致的讲解人始终留在当前画面内完成讲解，严禁离开画面”这层硬约束。不要省略性别，不要只写“讲解人”“人物”。这一句必须放在最前面，先声明边界，再继续写后面的镜头、表演和口播。同时还要让这个讲解人的说话语气、动作方式、表情风格、眼神状态和身体表现持续符合项目设置的人设。
25. video_positive_prompt 必须全文使用中文自然表达，必须是“连续叙事正文 + 最后一行 Audio:”的固定结构。每一个场景，包括 material_scene、presenter_scene、closing_scene，都必须包含最终 Audio: 小节，不能省略。
26. 台词如果出现在正文中，必须使用中文直角引号「」。
27. Audio: 必须是 video_positive_prompt 字符串里的最后一个非空行，前面必须有连续叙事正文；在 JSON 字符串中要写成类似“……稳定收尾。\nAudio: 街道环境音、轻微风声。”这种形式。不要把 Audio: 写到正文中间，不要写多个 Audio:，不要漏掉 Audio:。
28. Audio: 只允许写背景音、环境音、物体声和空间回响，不要把台词、旁白或解说复述写进 Audio:。
29. 下游模型是 LTX2.3，它只能理解当前提示词里直接可见或可听的事实，而且一旦人物走出当前画面、离开首帧可见区域，或在结尾继续往画外移动，就很容易生成出奇怪的变形和不稳定动作。video_positive_prompt 必须严格承接后续首帧图片里能看见的内容，不要写需要走出画面、离开当前场景、切到画外信息、突然多出新物体或新人物的动作。
26. 在 video_positive_prompt 里，尤其是关键动作、镜头变化、收尾停住这类硬约束句子中，必须反复使用与项目设置性别一致的明确前景主体称呼来表达动作，不要在这些关键句子里退回成模糊的“讲解人”“人物”，也不要用“她”“他”“她停住”“他转身”这类代词来表达关键动作，以免模型把动作执行错主体，或让主体走开。
27. 如果某个场景需要讲解人，默认你应理解合成后的首帧通常是“前景中的一位半身讲解人面对镜头讲解，背景主体完整保留”。不要写与这类基础构图明显冲突的大动作，也不要默认人物坐着。只有当输入事实本身明确要求坐姿讲解、桌边介绍、展示台介绍时，才允许写成坐姿。
28. presenter_scene / closing_scene 的视频提示词必须更像真人现场拍摄：镜头始终带极轻微、真实的手持呼吸感，像有人拿着设备在现场拍；可以有更鲜明的拍摄节奏和人物互动感，但不能剧烈晃动、不能大幅摇镜，也不能突然失控。
28. 每条 presenter_scene / closing_scene 视频可以使用 1 到 2 个彼此协调的主导镜头动作，只能从以下安全运镜中选择并组合：轻微推近、轻微拉远、轻微平移、轻微跟拍、轻微后退跟拍、极轻微向主体聚焦。允许比以前更丰富的运镜表达，但仍然要保持同一条视频里的镜头逻辑清楚、连贯，不要变成混乱的多段切换。
29. 如果运镜包含“轻微平移”或“轻微跟拍”，人物应自然配合更明显一点的身体转向、肩部转向、手势变化或短暂视线配合，让镜头运动与人物状态协调起来；人物大多数时间仍然要保持对镜头的讲解感，但可以比之前更灵活，不必始终僵硬正对镜头。
30. 每条 presenter_scene / closing_scene 视频允许 2 到 4 个明确的人物动作或表演节拍，并且表情、眼神、肩部状态、手势状态和身体姿态应与口播同步，让下游模型能明显感受到人物当下的情绪。动作必须与口播同步、目的明确，优先使用：看镜头说、轻微点头、自然抬手示意、轻微侧身、小步移动、小幅退一步、短暂转向、手部强调动作、最后停住。不要设计莫名其妙转头、突然发笑、频繁乱指、复杂走位、走出画面。最后收尾时必须明确让人物停在当前可见区域内，保持在镜头里，不要继续往画外走。
31. video_positive_prompt 必须写成 LTX2.3 能直接执行的视觉语言，不要写给人看的抽象说明。像“有一点强调感”“像在等对方反应”“带一点算账的自信”“显得更有说服力”这类抽象心理语义，不能单独出现，必须改写成可直接看见的表情、口型、眼神、停顿、手势或身体变化。优先把抽象语义翻译成可见动作，例如轻微抬眉、短暂停顿、嘴角收紧后再开口、手掌摊开、抬手比数、身体微微前倾、肩部放松后回正、视线短暂停留后回到镜头，而不要停留在人类能理解但模型不一定能稳定执行的概括词。
32. 不要使用“说到『某个词』时”“提到『某句话』时”“在说『某个字眼』的时候”这类给人看的词语级时间锚点，也不要把手势绑定到单个词语上。LTX2.3 更适合理解按顺序展开的可见节拍：人物先做一个小动作，再说完整一句；或人物说完整一句后，再接一个明显但克制的表情/手势变化。动作时机要写成整句级、节拍级的连续视觉过程，而不是写成“听懂某个词后才触发”的说明。
33. 涉及数字、价格、费用、转让费、面积、时长、数量等信息时，不要只写“带一点强调感”这种抽象描述，更适合明确写出可见的数值表达动作，例如手部小幅摊开、用手指做简短计数动作、点头后短暂停顿、手掌向前轻示意等；只有当这种动作在当前构图里合理可见时才允许使用。不要发明过于复杂、夸张或脱离当前半身构图的动作。
34. 人物大多数时间应主要看向镜头，只有在确实需要带一下场景或主体时，才允许极短暂、有目的地看一眼旁边内容，然后自然回到镜头。不要让人物频繁左顾右盼。
35. 人物的表情、语气、说话节奏和动作状态，不要写死成统一模板。你应该根据输入内容先在内部判断这条更像开心推荐、惊喜分享、真诚沟通、理性说明、着急转让、带一点急切、专业介绍、轻松种草、郑重提醒或其他更合适的情绪状态，再把这种判断自然翻译成合适的表情、语气、口型、眼神和动作。不要把人物限制在一种固定情绪里，但一定要明确写出当前场景最适合的情绪表现，让模型真的能表现出来。可以积极、认真、真诚、带一点急切感或推荐感，但不要默认傻笑、乱笑、夸张表演或无缘无故过度兴奋。整体表达不要平、不要像机械念稿，允许出现更自然的递进、强调、轻微停顿和口语化技巧，让人物像真的在讲一个值得别人听下去的内容。
36. presenter_scene / closing_scene 的对话密度不应该平庸、干瘪或只有一两句太短的直给句。每条视频优先保留 1 到 3 个最重要的信息点，并把口播组织成 4 到 6 个更自然的表达节拍。除了台词本身，还要在提示词里自然写出与台词相匹配的表情变化、手势变化、肩部状态、身体姿态或细微动作，让人物在说话时有明显而自然的情绪和肢体表现。可以删减次要信息，但不要把整条视频写成过短、没层次、没技巧的平铺直叙。
37. 室外 presenter_scene 默认最后停在原地，只允许微动，不允许设计转身离场、背对镜头走开、迈步离开、继续往画外走或任何会让前景主体离开当前画面的动作。室内 presenter_scene 默认也不要离开当前可见区域，只允许很小幅度的位移、侧身或停顿收尾。closing_scene 更应该以收尾停住为主。无论室内还是室外，结尾都必须让前景中的讲解人留在当前构图里，停住并保持最后姿态，严禁离开当前画面，也不要在最后多迈一步。收尾句必须把“与项目设置性别一致的前景讲解人保持当前站位、严禁离开当前画面”这层意思写清楚，不要只写成模糊的“讲解人停住”或“人物留在画面内”。但更重要的是，这种边界句必须在 video_positive_prompt 第一行最先出现一次，收尾时再重复一次。
38. presenter_scene / closing_scene 中，前景中的讲解人必须是画面内唯一明确主体。除了前景中的讲解人之外，背景最多只允许 1 到 2 个更远处的路人短暂经过，他们只能作为更远处的背景环境元素存在，不要靠近前景中的讲解人，不要停下来看镜头，不要围观讲解人，不要与讲解人互动，也不要变成新的主体。
39. 室外 presenter_scene 在不违背输入事实的前提下，优先加入可见但克制的自然风感：例如头发边缘有轻微被风带动的真实变化，衣领边缘和肩部布料有一点轻微风感；如果手臂或袖口清楚可见，也可以写成袖口有一点轻微自然摆动。不要默认写衣服下摆，因为半身构图里下摆通常不可见。除非输入事实明显不适合，否则室外 presenter_scene 默认应包含这种轻微风感；但不要夸张，不要写成强风、飘动过大或戏剧化效果。
40. 如果某个场景是纯素材场景，就不要写主持人或讲解人动作，更适合围绕当前主体做稳定展示、轻微推近、轻微拉远、轻微平移或极轻微聚焦中的一种。不要让纯素材场景出现复杂镜头运动。
41. need_presenter 为 true 的场景，你可以默认最终视频更适合竖屏人物讲解；need_presenter 为 false 的场景，更适合横向展示主体。请在 upload_requirement 和 video_positive_prompt 里自然考虑这一点，但不要直接输出生硬的技术术语。
42. duration_seconds 必须是 2 到 20 的整数，并且选择自然、完整、但不过长的时长。不要只追求最短，要给人物表达、表情发挥、动作节拍和运镜节拍留出足够空间。
43. 如果项目总文案里已经明确给出了某个场景的目标时长，例如“场景2：……控制在3秒”“时长8秒”“2秒即可”，你必须优先服从这个用户指定时长来填写对应 scene 的 duration_seconds，不允许因为你自己觉得内容多少更合适就擅自改成别的时长。只有当用户明确给出的时长超出 2 到 20 秒硬边界时，才允许调整到最接近的合法整数，并把该场景的动作、口播、运镜和节奏一起压缩或补足到这个合法时长内。
44. 你必须先在内部评估这一条视频的“信息负载、动作负载、运镜负载、情绪表达负载”再决定时长，而不是按字数机械估时。但这条推理规则只在用户没有明确指定该场景时长时才生效；如果用户已经写明场景时长，你必须围绕用户给定时长去反推提示词密度，而不是反过来改掉用户时长。
45. 时长选择优先遵守以下区间：只有 1 个核心信息点且动作简单时，通常更适合 5 到 7 秒；有 2 个核心信息点、多个表演节拍、或需要更自然的说话层次时，通常更适合 7 到 10 秒；只有在确实还需要额外动作节拍、镜头缓冲或更完整表达时，才考虑 10 到 12 秒。超过 12 秒必须谨慎，但不是绝对禁止。
46. 不要为了覆盖输入中的所有信息而无限拉长时长，但也不要因为害怕变长而把视频压得太短。信息太多时，必须主动压缩、合并、删减，同时保留足够的说话空间，让人物说话有层次、有感情、有表达技巧，并允许提示词写得更长、更完整、更具表演感。
47. “结尾停住”只代表稳定收尾，不代表要留很长的空白停顿；但也不要把收尾压得过快，导致人物刚说完就立刻结束，应该给收尾动作和表情一个自然落点。
48. 如果你在两个时长之间犹豫，优先选择“更完整、更自然、更能承载表情与动作发挥”的那个，但仍然要避免无意义的拖长，因为 LTX2.3 的视频越长，越容易出现动作发散、人物漂移和结尾变形。
49. 整体风格必须统一，像同一个项目里连续拍出来的一组内容；但不要机械重复句式。
50. 你需要自己从输入文案里判断当前讲解的主体到底是什么，再决定场景拆解；主体可能是场地、门面、商品、设备、服务、活动、信息介绍或其他任何内容，不要拘泥于固定类别。
51. title、upload_headline、upload_requirement、intro_text 和 video_positive_prompt 里的主体措辞，必须优先遵循“项目总文案”里明确出现的称呼。不要被示例、历史场景或你自己的惯性理解带偏，也不要把一种主体擅自改写成另一种主体。如果总文案没有明确给出标准称呼，就优先使用更中性、更通用、但不容易带偏的说法，例如“这里”“这边”“当前主体”“当前位置”“这处内容”，不要自行发明具体行业名词。
52. 如果输入里讲的是价格、费用、转让条件、优惠、付款方式、位置优势、曝光、人流、转让原因等非空间结构信息，不要为了顺口把这些信息改写成“这个空间”“这片空间”“这个区域”之类的泛化说法。只有在场景重点真的在讲面积、布局、采光、房间结构或空间体验时，才适合使用“空间”这个词。
53. 不要把所有文案都塞进一个场景。要按“入口介绍 / 核心卖点 / 关键空间或关键素材 / 收尾推荐”这种逻辑拆开，但不要拘泥于固定行业模板。
54. 如果项目总文案里已经出现适合直接口播的原始短句、招租短句、地点短句或卖点短句，优先保留其核心表达，不要擅自改写成更空泛、更转述腔的说法，例如随意改成“这里在……”“这边是……”。除非原句明显不适合直接说出口，否则应优先贴近用户原话。
55. 如果用户明确写了某个场景“重点说什么”或“只说什么”，你必须保证这个重点信息在该 scene 的口播里被明确说出来；在不偏题、不冲突、且时长允许的前提下，可以做自然扩写，但不要把用户指定重点冲淡、替换或跳过。
56. 任何依赖上传图片的 scene，video_positive_prompt 都必须默认承接上传图片已经给出的原始视角、原始构图、原始朝向和原始机位关系。不要擅自假设图片一定是正面、居中、对称、镜头正对主体，也不要把镜头改写成会重新找角度的新机位，除非用户明确要求。
57. 如果用户明确写了“无对白”“微动”“静态画面”“仅轻微手持抖动”之类的要求，该 scene 只能保持当前构图做极轻微呼吸感或手持微晃，不允许推近、拉远、平移、重构图、转正主体、让镜头重新对准主体，也不要写会暗示重新取景的描述。
58. material_scene 更适合描述“在当前画面基础上的轻微动态”，不要写“主体完整居中呈现”“镜头正对左侧门”“镜头转到正面展示”这类会暗示画面重新构图、重新定机位或纠正视角的话。`

	userPromptBuilder := strings.Builder{}
	presenterLead := "前景中的一位女性讲解人"
	if project.PresenterGender == generalGuidePresenterGenderMale {
		userPromptBuilder.WriteString("讲解人性别：男性\n")
		presenterLead = "前景中的一位男性讲解人"
	} else {
		userPromptBuilder.WriteString("讲解人性别：女性\n")
	}
	userPromptBuilder.WriteString(fmt.Sprintf("讲解人人设：%s\n", generalGuidePresenterPersonaLabel(project.PresenterPersona)))
	userPromptBuilder.WriteString(fmt.Sprintf("项目总文案：%s\n", strings.TrimSpace(content)))
	if videoWidth > 0 && videoHeight > 0 {
		userPromptBuilder.WriteString(fmt.Sprintf("当前目标视频尺寸：%d × %d\n", videoWidth, videoHeight))
	}
	userPromptBuilder.WriteString(fmt.Sprintf("当前目标视频帧率：%dfps\n", generalGuideVideoFPS))
	userPromptBuilder.WriteString("\n额外要求：\n")
	userPromptBuilder.WriteString("- 需要讲解人出镜的场景里，讲解人的性别必须始终与项目设置一致，不要自行切换成另一种性别；但这个性别只用于约束 video_positive_prompt 里的出镜讲解人，不要因为这个设置就把场景标题或摘要强行写成“老板娘亲口说”“房东本人讲”这类身份表达，除非输入事实明确这样要求。\n")
	userPromptBuilder.WriteString(fmt.Sprintf("- 项目当前讲解人人设是：%s。%s\n", generalGuidePresenterPersonaLabel(project.PresenterPersona), generalGuidePresenterPersonaHint(project.PresenterPersona, project.PresenterGender)))
	userPromptBuilder.WriteString(fmt.Sprintf("- 只要 need_presenter=true，video_positive_prompt 第一行就必须先写边界句，并显式使用“%s”作为主语，然后先说明该主体始终留在当前画面内完成讲解、严禁离开画面。不要省略性别，也不要只写“人物”或“讲解人”。\n", presenterLead))
	userPromptBuilder.WriteString("- 视频里讲解人的说话语气、表情状态、肢体动作、行为方式、眼神和节奏，都必须持续符合当前项目设置的人设，不要只改性别而忘了人设气质。\n")
	userPromptBuilder.WriteString("- 主动补齐合理的场景规划，但不要凭空编造太多具体事实。\n")
	userPromptBuilder.WriteString("- 首个场景优先从室外开场来规划，这样更自然；只有当输入事实明显不适合室外开场时，才允许从室内开始。\n")
	userPromptBuilder.WriteString("- 如果首个场景已经从室外开场，第二个场景优先切入室内主体信息，不要默认连续两个室外人物讲解场景；只有当输入事实明确要求继续在室外补充关键信息时，第二个场景才允许继续室外。\n")
	userPromptBuilder.WriteString("- 优先让每个场景都明确对应一张可准备的上传图片。\n")
	userPromptBuilder.WriteString("- 对需要讲解人的场景，尽量让上传要求说明该图片应该保留完整空间、前景适合半身人物站位、光线均匀明亮。\n")
	userPromptBuilder.WriteString("- 对纯素材场景，尽量让上传要求说明主体完整、背景干净、构图稳定。\n")
	userPromptBuilder.WriteString("- 纯素材场景只能承担纯视觉展示，不承担解释任务。不要把“转让原因、老板娘怀孕、为什么低价、价格是否划算、付款方式、真实性判断、优惠逻辑”这类需要讲解人口播才能成立的内容塞给纯素材场景。\n")
	userPromptBuilder.WriteString("- 如果一条信息需要人物开口解释，或者用户不听声音就无法理解这条信息，那它就不应该是 material_scene，而应该规划成 presenter_scene 或 closing_scene。\n")
	userPromptBuilder.WriteString("- material_scene 的摘要和视频提示词里，不要出现“用素材说明”“配合解释”“通过画面交代原因”这类说法。纯素材只负责让观众看见客观存在的东西，不负责替代讲解人说话。\n")
	userPromptBuilder.WriteString("- 如果输入里包含价格、出租、转让、优惠、配套、位置优势、付款方式、功能特点等信息，请合理拆散到不同场景。\n")
	userPromptBuilder.WriteString("- 如果总文案本身没有把主体说成“空间”，那你在摘要和视频提示词里也不要把价格、转让、费用这类信息讲成“这个空间怎么样”。这类事实更适合用“这里”“这边”“当前主体”“这个位置”之类更贴近文案的说法。\n")
	userPromptBuilder.WriteString("- 如果项目总文案里已经出现适合直接说出口的原始短句、招租短句、地点短句或卖点短句，优先保留其核心表达，不要擅自改写成更空泛的“这里在……”“这边是……”这类转述腔说法。\n")
	userPromptBuilder.WriteString("- 如果用户明确写了某个场景“重点说什么”或“只说什么”，你必须保证这个重点一定被说出来；在不偏题、不冲突且时长允许的前提下，可以自然扩写，但不要把重点冲淡。\n")
	userPromptBuilder.WriteString("- 请把 video_positive_prompt 写得像真正能拿来驱动 LTX2.3 的拍摄指令，而不是普通文案。\n")
	userPromptBuilder.WriteString("- 每一个 video_positive_prompt 都必须以最后一行 Audio: 收尾，JSON 字符串中要包含换行符，例如“……稳定收尾。\\nAudio: 室内环境音、轻微空间回响。”。缺少 Audio: 会被系统直接判定为失败。\n")
	userPromptBuilder.WriteString("- 请根据输入内容自动内部判断更合适的说话语气、表情状态和动作感觉，再自然写进 video_positive_prompt；不要机械套用一种固定表情或一种固定说话方式。\n")
	userPromptBuilder.WriteString("- LTX2.3 对明确写出来的情绪、表情和肢体动作有更好的执行能力。请不要把人物写得过于平淡，要根据当前场景主动选择更合适的情绪，并明确写出表情、眼神、口型或身体状态；情绪可以是开心、真诚、急切、专业、惊喜、郑重、轻松等，但必须匹配场景，不要固定成一种。\n")
	userPromptBuilder.WriteString("- 口播不要写成平铺直叙、功能说明式、没有情绪的短句堆叠。请让人物说话更像真人在认真介绍，有一点起承转合、轻微强调和表达技巧，但仍然自然克制。\n")
	userPromptBuilder.WriteString("- 请特别注意“口播 + 手势 + 运镜”的协调：人物说什么，就配什么动作、表情和肢体反馈。不要把动作压得太少，可以更自然、更丰富，只要仍然留在当前画面内。\n")
	userPromptBuilder.WriteString("- 不要写“说到『某个词』时”“提到『某句话』时”这类给人看的词语级说明，也不要把动作绑在单个词上。更适合把动作写成完整节拍：人物先做一个动作再说一句，或说完一句再接一个明显但克制的动作/表情变化。\n")
	userPromptBuilder.WriteString("- 每条 presenter_scene / closing_scene 视频都要带极轻微、真实的手持呼吸感，像真人在拍；但绝不要写成明显乱晃。\n")
	userPromptBuilder.WriteString("- 如果这一条选择的是轻微平移，请让人物配合一个小幅自然转向或肩部转向，但仍然以看着镜头讲解为主，不要让人物一直看向别处。\n")
	userPromptBuilder.WriteString("- 静态首帧很容易让模型跑飞，所以不要给视频提示词设计大角度转换、大范围走位、复杂连续动作或离开当前画面的行为。\n")
	userPromptBuilder.WriteString("- 任何依赖上传图片的 scene，都必须承接上传图片已经给出的原始视角、原始构图、原始朝向和原始机位关系。不要擅自假设图片一定是正面、居中、对称、镜头正对主体，也不要把镜头改写成新的找角度动作。\n")
	userPromptBuilder.WriteString("- 如果用户明确写了“无对白”“微动”“静态画面”“仅轻微手持抖动”，该 scene 只能保持当前构图做极轻微呼吸感或手持微晃，不允许推近、拉远、平移、重构图、转正主体、让镜头重新对准主体。\n")
	userPromptBuilder.WriteString("- material_scene 更适合描述“基于当前画面的轻微动态”，不要写“主体完整居中呈现”“镜头正对某一侧门”“镜头转到正面展示”这类会暗示重新取景、重新定机位或纠正视角的话。\n")
	userPromptBuilder.WriteString(fmt.Sprintf("- 请把“严禁离开画面”和“结尾停住”都当成硬约束：%s始终留在当前可见区域内完成讲解，第一行先写清楚，结尾再重复一次，不要继续往画外走。因为 LTX2.3 一旦主体在结尾继续离开画面，很容易生成奇怪变形。\n", presenterLead))
	userPromptBuilder.WriteString("- 室外场景必须特别克制：不要设计人物转身离场、背对镜头走开、迈步离开或任何会让人物脱离当前站位的动作。室外收尾只能停在原地完成。\n")
	userPromptBuilder.WriteString(fmt.Sprintf("- 所有 presenter_scene / closing_scene 里，关键主体和收尾约束都请尽量直接写成“%s”，例如“%s保持当前站位，严禁离开当前画面”。不要在这些关键句里退回成模糊的“讲解人停在当前可见区域内”或“人物留在画面里”。\n", presenterLead, presenterLead))
	userPromptBuilder.WriteString("- 室外场景里如果有其他人，请只把他们写成更远处、最多 1 到 2 个的背景路人短暂经过，不要让他们靠近讲解人，也不要让他们抢主体。\n")
	userPromptBuilder.WriteString("- 室外场景默认请写出轻微自然风感，例如头发边缘有一点被风带动的真实变化，衣领边缘和肩部布料有一点轻微风感；如果袖口清楚可见，也可以写成袖口有一点轻微自然摆动。不要默认写衣服下摆，因为半身构图里下摆通常不可见；风感必须克制、真实、不能夸张。\n")
	userPromptBuilder.WriteString("- 时长不要压得过短。优先给出完整、自然、能容纳表达层次、表情发挥和动作节拍的时长；如果拿不准，不要机械选最短，而要选更像真人能从容说完、从容演完的那个。\n")
	userPromptBuilder.WriteString(fmt.Sprintf("- 当前视频固定按 %dfps 生成。推理 duration_seconds 时，请按 %dfps 的真实节奏估算说话速度、停顿、手势、运镜和收尾节拍，不要脱离这个固定帧率去估时长。\n", generalGuideVideoFPS, generalGuideVideoFPS))
	userPromptBuilder.WriteString("- 如果项目总文案里已经按“场景1 / 场景2 / 场景3”或“控制在X秒 / 时长X秒 / X秒即可”的方式明确写了某一行时长，你必须优先按用户指定时长填写对应 scene 的 duration_seconds，不要擅自改掉。\n")
	userPromptBuilder.WriteString("- 只有当用户明确写出的时长小于 2 秒或大于 20 秒时，你才允许改成最接近的合法整数；改完以后，还必须同步压缩或补足这一行的 video_positive_prompt，让内容真的能在这个合法时长内完成。\n")
	userPromptBuilder.WriteString("- 如果用户明确写了“无对白”“场景微动”“控制在2秒即可”这类约束，你必须同步降低该 scene 的信息负载、动作负载和口播负载；不能一边给很短时长，一边写成长对白和过重动作。\n")
	userPromptBuilder.WriteString("- 在输出最终 JSON 之前，你必须逐个 scene 做一遍时长合法化检查：scene1 到 sceneN 的 duration_seconds 必须全部真实存在，且每一个都必须是 2 到 20 的整数；任何一个 scene 只要写成 1、21、null、字符串，或漏掉 duration_seconds，整个答案都必须在内部重写后再输出。\n")
	userPromptBuilder.WriteString("- 如果用户已经明确写出了分场景清单和每场时长，优先沿用用户给出的场景顺序与对应关系，不要随意合并、拆分或打乱，避免把时长约束错配到别的 scene。\n")
	userPromptBuilder.WriteString("- 最终输出前，必须逐个 scene 重新检查 9 个必填字段是否全部真实输出，尤其是 duration_seconds。只要有任意一个 scene 漏了 duration_seconds，就必须在内部重写后再输出。\n")
	userPromptBuilder.WriteString("- duration_seconds 必须直接写在每个 scene 对象里，键名就叫 duration_seconds；不要省略，不要写成 null，不要写成 0，不要写成字符串，不要寄希望于系统替你补。\n")
	userPromptBuilder.WriteString("- 如果你已经写完了 video_positive_prompt，但还没写 duration_seconds，这个答案仍然是不合格的。你必须继续补齐所有 scene 的 duration_seconds，再输出最终 JSON。\n")
	if tagRules != "" {
		userPromptBuilder.WriteString("\n")
		userPromptBuilder.WriteString(tagRules)
		userPromptBuilder.WriteString("\n")
	}
	if len(existingScenes) > 0 {
		userPromptBuilder.WriteString("\n已有场景只作上下文参考；本次请按最新总文案重新输出完整 scenes 数组。\n")
	}
	userPromptBuilder.WriteString("\n只返回 JSON。")

	return systemPrompt, userPromptBuilder.String(), nil
}

func parseGeneralGuidePlanResponse(raw string) (*generalGuidePlanScenesResponse, error) {
	trimmed := cleanupGeneralGuidePlanJSON(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty llm response")
	}
	var payload generalGuidePlanScenesResponse
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, fmt.Errorf("invalid json: %v", err)
	}
	for i := range payload.Scenes {
		payload.Scenes[i].Title = strings.TrimSpace(payload.Scenes[i].Title)
		payload.Scenes[i].SceneType = normalizeGeneralGuideSceneType(payload.Scenes[i].SceneType)
		payload.Scenes[i].EnvironmentType = normalizeGeneralGuideEnvironmentType(payload.Scenes[i].EnvironmentType)
		payload.Scenes[i].UploadHeadline = strings.TrimSpace(payload.Scenes[i].UploadHeadline)
		payload.Scenes[i].UploadRequirement = strings.TrimSpace(payload.Scenes[i].UploadRequirement)
		payload.Scenes[i].IntroText = strings.TrimSpace(payload.Scenes[i].IntroText)
		payload.Scenes[i].VideoPositivePrompt = normalizeGeneralGuideVideoPositivePrompt(strings.TrimSpace(payload.Scenes[i].VideoPositivePrompt))
		if payload.Scenes[i].SceneType == generalGuideSceneTypeMaterial {
			payload.Scenes[i].NeedPresenter = false
		}
		payload.Scenes[i].ImagePreset = deriveGeneralGuideImagePreset(
			payload.Scenes[i].SceneType,
			payload.Scenes[i].EnvironmentType,
			payload.Scenes[i].NeedPresenter,
		)
		if payload.Scenes[i].ImagePreset == generalGuideImagePresetMaterialOnly {
			payload.Scenes[i].NeedPresenter = false
		}
	}
	return &payload, nil
}

func cleanupGeneralGuidePlanJSON(content string) string {
	jsonContent := strings.TrimSpace(content)
	re := regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")
	match := re.FindStringSubmatch(jsonContent)
	if len(match) > 1 {
		jsonContent = match[1]
	}
	arrayStart := strings.Index(jsonContent, "[")
	arrayEnd := strings.LastIndex(jsonContent, "]")
	objectStart := strings.Index(jsonContent, "{")
	objectEnd := strings.LastIndex(jsonContent, "}")
	if arrayStart >= 0 && arrayEnd >= arrayStart && (objectStart == -1 || arrayStart < objectStart) {
		jsonContent = jsonContent[arrayStart : arrayEnd+1]
	} else if objectStart >= 0 && objectEnd >= objectStart {
		jsonContent = jsonContent[objectStart : objectEnd+1]
	}
	jsonContent = normalizeLLMJSONTypography(jsonContent)
	return strings.TrimSpace(jsonContent)
}

func normalizeGeneralGuideVideoPositivePrompt(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || !strings.Contains(trimmed, `"`) {
		return trimmed
	}
	var builder strings.Builder
	builder.Grow(len(trimmed) + 8)
	opening := true
	for _, r := range trimmed {
		if r == '"' {
			if opening {
				builder.WriteRune('「')
			} else {
				builder.WriteRune('」')
			}
			opening = !opening
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func validateGeneralGuideVideoPromptInferResponse(payload *storeVisitVideoPromptInferResponse) error {
	if payload == nil {
		return fmt.Errorf("infer payload is nil")
	}
	if payload.VideoPositivePrompt == "" {
		return fmt.Errorf("video_positive_prompt is required")
	}
	if strings.Contains(payload.VideoPositivePrompt, `"`) {
		return fmt.Errorf("video_positive_prompt must not contain ASCII double quotes")
	}
	if payload.DurationSeconds < generalGuideMinVideoDurationSeconds || payload.DurationSeconds > generalGuideMaxVideoDurationSeconds {
		return fmt.Errorf("duration_seconds must be between %d and %d", generalGuideMinVideoDurationSeconds, generalGuideMaxVideoDurationSeconds)
	}
	flowing, err := parseFlowingVideoPrompt(payload.VideoPositivePrompt)
	if err != nil {
		return err
	}
	if err := validateStoreVisitAudioSection(flowing.Audio); err != nil {
		return err
	}
	return nil
}

func validateGeneralGuidePlanResponse(payload *generalGuidePlanScenesResponse) error {
	if payload == nil {
		return fmt.Errorf("scene planning payload is nil")
	}
	if len(payload.Scenes) == 0 {
		return fmt.Errorf("missing scenes")
	}
	if len(payload.Scenes) > 12 {
		return fmt.Errorf("too many scenes")
	}
	firstScene := payload.Scenes[0]
	if firstScene.SceneType == generalGuideSceneTypeMaterial || !firstScene.NeedPresenter {
		return fmt.Errorf("first scene must be a presenter scene with presenter")
	}
	materialSceneCount := 0
	for idx, scene := range payload.Scenes {
		if strings.TrimSpace(scene.Title) == "" {
			return fmt.Errorf("scene %d title is required", idx+1)
		}
		if _, ok := generalGuideAllowedSceneTypes[scene.SceneType]; !ok {
			return fmt.Errorf("scene %d scene_type is invalid", idx+1)
		}
		if _, ok := generalGuideAllowedEnvironmentTypes[scene.EnvironmentType]; !ok {
			return fmt.Errorf("scene %d environment_type is invalid", idx+1)
		}
		if scene.ImagePreset == generalGuideImagePresetMaterialOnly && scene.NeedPresenter {
			return fmt.Errorf("scene %d material_only cannot require presenter", idx+1)
		}
		if scene.SceneType == generalGuideSceneTypeMaterial || !scene.NeedPresenter {
			materialSceneCount++
		}
		if strings.TrimSpace(scene.UploadRequirement) == "" {
			return fmt.Errorf("scene %d upload_requirement is required", idx+1)
		}
		if strings.TrimSpace(scene.UploadHeadline) == "" {
			return fmt.Errorf("scene %d upload_headline is required", idx+1)
		}
		if strings.TrimSpace(scene.IntroText) == "" {
			return fmt.Errorf("scene %d intro_text is required", idx+1)
		}
		if err := validateGeneralGuideVideoPromptInferResponse(&storeVisitVideoPromptInferResponse{
			VideoPositivePrompt: scene.VideoPositivePrompt,
			DurationSeconds:     scene.DurationSeconds,
		}); err != nil {
			return fmt.Errorf("scene %d invalid: %w", idx+1, err)
		}
	}
	if materialSceneCount > 2 {
		return fmt.Errorf("too many material scenes: maximum 2")
	}
	return nil
}

func persistGeneralGuidePlan(projectID uint, content string, payload *generalGuidePlanScenesResponse, videoWidth int, videoHeight int) error {
	assetsToRemove := make([]string, 0, 32)
	transitionEngine := getConfiguredGeneralGuideTransitionEngine()
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(projectID)))
	if videoWidth <= 0 {
		videoWidth = 720
	}
	if videoHeight <= 0 {
		videoHeight = 1280
	}
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		var project models.GeneralGuideProject
		if err := tx.First(&project, projectID).Error; err != nil {
			return err
		}
		var existing []models.GeneralGuideScene
		if err := tx.Where("project_id = ?", projectID).Order("sort_order asc, id asc").Find(&existing).Error; err != nil {
			return err
		}
		var existingTransitions []models.GeneralGuideTransition
		if err := tx.Where("project_id = ?", projectID).Order("from_sort_order asc, to_sort_order asc, id asc").Find(&existingTransitions).Error; err != nil {
			return err
		}

		now := time.Now()
		for _, scene := range existing {
			assetsToRemove = append(assetsToRemove, scene.ReferenceImage, scene.GeneratedImage, scene.GeneratedVideo)
			if err := tx.Delete(&models.GeneralGuideScene{}, scene.ID).Error; err != nil {
				return err
			}
		}
		for _, transition := range existingTransitions {
			assetsToRemove = append(assetsToRemove, transition.TailFrameImage, transition.GeneratedVideo)
			if err := tx.Delete(&models.GeneralGuideTransition{}, transition.ID).Error; err != nil {
				return err
			}
		}

		sceneIDsBySort := make(map[int]uint, len(payload.Scenes))
		for idx, item := range payload.Scenes {
			needPresenter := item.NeedPresenter
			imagePreset := deriveGeneralGuideImagePreset(item.SceneType, item.EnvironmentType, needPresenter)
			if imagePreset == generalGuideImagePresetMaterialOnly {
				needPresenter = false
			}
			scene := models.GeneralGuideScene{
				ProjectID:            projectID,
				SortOrder:            idx + 1,
				Title:                item.Title,
				SceneType:            item.SceneType,
				EnvironmentType:      item.EnvironmentType,
				NeedPresenter:        needPresenter,
				ImagePreset:          imagePreset,
				UploadHeadline:       item.UploadHeadline,
				UploadRequirement:    item.UploadRequirement,
				IntroText:            item.IntroText,
				ImagePositivePrompt:  generalGuideDefaultImagePrompt(imagePreset, item.EnvironmentType, item.SceneType, idx+1),
				ImageNegativePrompt:  generalGuideDefaultImageNegativePrompt(imagePreset, item.EnvironmentType, item.SceneType, idx+1),
				VideoPositivePrompt:  item.VideoPositivePrompt,
				VideoNegativePrompt:  generalGuideDefaultVideoNegativePrompt,
				VideoDurationSeconds: item.DurationSeconds,
				VideoWidth:           videoWidth,
				VideoHeight:          videoHeight,
				ImageStatus:          "draft",
				VideoStatus:          "draft",
				CreatedAt:            now,
				UpdatedAt:            now,
			}
			if err := tx.Create(&scene).Error; err != nil {
				return err
			}
			sceneIDsBySort[idx+1] = scene.ID
		}

		for sortOrder := 1; sortOrder < len(payload.Scenes); sortOrder++ {
			preset := randomGeneralGuideTransitionPreset(rng, transitionEngine)
			durationSeconds := generalGuideTransitionDefaultSecond
			if preset.RecommendedDurationSeconds > 0 {
				durationSeconds = sanitizeGeneralGuideTransitionDurationSeconds(preset.RecommendedDurationSeconds)
			}
			transition := models.GeneralGuideTransition{
				ProjectID:        projectID,
				FromSceneID:      sceneIDsBySort[sortOrder],
				ToSceneID:        sceneIDsBySort[sortOrder+1],
				FromSortOrder:    sortOrder,
				ToSortOrder:      sortOrder + 1,
				TransitionPrompt: normalizeGeneralGuideTransitionPromptForEngine(preset.Prompt, transitionEngine),
				DurationSeconds:  durationSeconds,
				FramesFromEnd:    sanitizeGeneralGuideTransitionFramesFromEndForEngine(generalGuideTransitionDefaultFrames, transitionEngine),
				VideoStatus:      "draft",
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if err := tx.Create(&transition).Error; err != nil {
				return err
			}
		}

		return tx.Model(&models.GeneralGuideProject{}).Where("id = ?", projectID).Updates(map[string]interface{}{
			"auto_generate_content":    strings.TrimSpace(content),
			"current_planning_task_id": "",
			"last_planning_error":      "",
			"updated_at":               now,
		}).Error
	})
	if err != nil {
		return err
	}
	for _, asset := range assetsToRemove {
		if removeErr := removeGeneralGuideAsset(asset); removeErr != nil {
			Log(LogLevelWarn, "综合讲解规划后清理旧资源失败", fmt.Sprintf("project=%d asset=%s err=%v", projectID, asset, removeErr))
		}
	}
	return nil
}
