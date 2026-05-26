package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"

	"github.com/gin-gonic/gin"
	"github.com/sashabaranov/go-openai"
	"gorm.io/gorm"
)

type storeVisitProjectAutoGenerateRequest struct {
	Content          string `json:"content"`
	AllowSkipMissing bool   `json:"allow_skip_missing_refs"`
	PromptsOnly      bool   `json:"prompts_only"`
}

type storeVisitProjectAutoGenerateTaskPayload struct {
	ProjectID        uint   `json:"project_id"`
	Content          string `json:"content"`
	AllowSkipMissing bool   `json:"allow_skip_missing_refs"`
	PromptsOnly      bool   `json:"prompts_only"`
}

type storeVisitProjectMissingReference struct {
	SpotID   uint   `json:"spot_id"`
	SpotType string `json:"spot_type"`
	Name     string `json:"name"`
}

type storeVisitProjectAutoGenerateSpotResponse struct {
	SpotType            string `json:"spot_type"`
	IntroText           string `json:"intro_text"`
	VideoPositivePrompt string `json:"video_positive_prompt"`
	DurationSeconds     int    `json:"duration_seconds"`
}

type storeVisitProjectAutoGenerateResponse struct {
	Spots []storeVisitProjectAutoGenerateSpotResponse `json:"spots"`
}

type queuedStoreVisitSpotRender struct {
	Spot          models.StoreVisitSpot
	PromptID      string
	WorkflowLabel string
}

type queuedStoreVisitDishRender struct {
	Item          models.StoreVisitDishGenerationItem
	PromptID      string
	WorkflowLabel string
}

var storeVisitProjectAutoGenerateSpotTypes = []string{
	storeVisitSpotTypeEntrance,
	storeVisitSpotTypeLobby,
	storeVisitSpotTypePrivateRoom,
	storeVisitSpotTypeKitchen,
	storeVisitSpotTypeFeaturedArea,
	storeVisitSpotTypeTableDishes,
	storeVisitSpotTypePromotion,
}

func StartStoreVisitProjectAutoGenerate(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "补齐探店区域失败"})
		return
	}

	var req storeVisitProjectAutoGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式不正确"})
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先填写项目总文案"})
		return
	}

	if !req.PromptsOnly {
		if hasRunning, err := storeVisitProjectHasRunningGeneration(project.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "检查项目生成状态失败"})
			return
		} else if hasRunning {
			c.JSON(http.StatusConflict, gin.H{"error": "项目仍在生成中，请等待完成后再操作"})
			return
		}
	}

	spots, err := listStoreVisitSpotsForProject(project.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取探店区域失败"})
		return
	}

	missingRefs := collectStoreVisitProjectMissingReferences(spots)
	missingDishGeneration := !storeVisitProjectHasRunnableDishGeneration(spots)
	if !req.PromptsOnly && ((!req.AllowSkipMissing && len(missingRefs) > 0) || (!req.AllowSkipMissing && missingDishGeneration)) {
		c.JSON(http.StatusConflict, gin.H{
			"error":                   "部分区域缺少参考图或菜品生成未配置 key frame，确认后才能继续",
			"requires_confirmation":   true,
			"missing_reference_spots": missingRefs,
			"missing_dish_generation": missingDishGeneration,
		})
		return
	}

	if err := db.DB.Model(&models.StoreVisitProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"auto_generate_content": req.Content,
		"updated_at":            time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存项目总文案失败"})
		return
	}

	payload := storeVisitProjectAutoGenerateTaskPayload{
		ProjectID:        project.ID,
		Content:          req.Content,
		AllowSkipMissing: req.AllowSkipMissing,
		PromptsOnly:      req.PromptsOnly,
	}
	t, err := task.GlobalTaskManager.AddTask("auto_generate_store_visit_project", payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交一键生成任务失败"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":                 ternaryStoreVisitMessage(req.PromptsOnly, "一键生成提示词任务已提交", "一键生成任务已提交"),
		"task_id":                 t.ID,
		"missing_reference_spots": missingRefs,
		"missing_dish_generation": missingDishGeneration,
		"prompts_only":            req.PromptsOnly,
	})
}

func HandleAutoGenerateStoreVisitProjectTask(t *models.Task) (interface{}, error) {
	var payload storeVisitProjectAutoGenerateTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	var project models.StoreVisitProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("博主探店项目不存在")
	}
	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		return nil, err
	}
	spots, err := listStoreVisitSpotsForProject(project.ID)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 10, "检查参考图和区域配置")

	systemPrompt, userPrompt := buildStoreVisitProjectAutoGeneratePrompts(project, payload.Content, spots)
	var provider models.LLMProvider
	if err := db.DB.Where("is_active = ?", true).First(&provider).Error; err != nil {
		return nil, fmt.Errorf("请先配置并启用 LLM 引擎")
	}
	model, err := requireProviderModelName(provider)
	if err != nil {
		return nil, err
	}

	Log(LogLevelInfo, llmLogMessage("LLM Request", provider), fmt.Sprintf("Starting store-visit project auto generation for project=%d", project.ID))
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

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 22, ternaryStoreVisitMessage(payload.PromptsOnly, "正在调用 LLM 生成全部区域提示词", "正在调用 LLM 生成全部区域提示词"))
	raw, err := requestLLMContentStreaming(provider, req, 12*time.Minute, t.ID, ternaryStoreVisitMessage(payload.PromptsOnly, "探店项目一键生成提示词", "探店项目一键生成"))
	if err != nil {
		return nil, err
	}
	Log(LogLevelInfo, llmLogMessage("LLM 完整返回(探店项目一键生成)", provider), raw)

	parsed, err := parseStoreVisitProjectAutoGenerateResponse(raw)
	if err != nil {
		return nil, err
	}
	if err := validateStoreVisitProjectAutoGenerateResponse(parsed); err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 42, "写回各区域介绍内容和视频提示词")
	if err := persistStoreVisitProjectAutoGenerateResult(project.ID, parsed); err != nil {
		return nil, err
	}

	if payload.PromptsOnly {
		task.GlobalTaskManager.UpdateTaskProgress(t.ID, 100, "项目提示词一键生成完成")
		return gin.H{
			"project_id":    project.ID,
			"llm_spots":     len(parsed.Spots),
			"prompts_only":  true,
			"image_summary": nil,
			"video_summary": nil,
			"dish_summary":  nil,
		}, nil
	}

	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		return nil, err
	}
	spots, err = listStoreVisitSpotsForProject(project.ID)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 52, "批量提交图片生成到 ComfyUI 队列")
	imageSummary, err := queueAndRenderStoreVisitProjectImages(t.ID, project, spots)
	if err != nil {
		return nil, err
	}

	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		return nil, err
	}
	spots, err = listStoreVisitSpotsForProject(project.ID)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 74, "批量提交视频生成到 ComfyUI 队列")
	videoSummary, err := queueAndRenderStoreVisitProjectVideos(t.ID, project, spots)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 88, "菜品生成最后批量执行")
	dishSummary, err := queueAndRenderStoreVisitProjectDishVideos(t.ID, project, spots)
	if err != nil {
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 100, "项目一键生成完成")
	return gin.H{
		"project_id":    project.ID,
		"llm_spots":     len(parsed.Spots),
		"prompts_only":  false,
		"image_summary": imageSummary,
		"video_summary": videoSummary,
		"dish_summary":  dishSummary,
	}, nil
}

func buildStoreVisitProjectAutoGeneratePrompts(project models.StoreVisitProject, content string, spots []models.StoreVisitSpot) (string, string) {
	systemPrompt := `你是一个专门把“探店项目总文案”拆解成多个区域口播提示词的提示词工程助手。

你的任务是根据用户输入的一段总文案，一次性为以下 7 个区域返回结果：
1. entrance
2. lobby
3. private_room
4. kitchen
5. featured_area
6. table_dishes
7. promotion

你必须严格遵守以下规则：
1. 只能返回 JSON。禁止输出解释、标题、注释、代码块。
2. 顶层 JSON 只能包含：
   - spots
3. spots 必须是数组，且必须完整返回 7 个对象，不能缺少也不能多出其他类型。
4. 每个对象必须包含：
   - spot_type
   - intro_text
   - video_positive_prompt
   - duration_seconds
5. intro_text 只用于回填编辑框，必须是简短摘要，1 到 3 句中文即可，不要写成长台词稿。
6. video_positive_prompt 必须全文使用中文自然表达，必须是连续叙事式正文，最后单独一行必须是 Audio: ...
7. 台词如果出现在正文中，必须使用中文直角引号「」。
8. 不要在正文中使用 ASCII 双引号 "。
9. 下游模型是 LTX2.3，只能理解当前提示词里直接可见或可听的事实。
10. 不要写角色名字；人物只能用自然、通俗、可执行的方式来指代。
11. 最终正文里禁止出现“同一位”“出镜人物”“当前人物”“当前区域”“首帧”“当前图片”这类元词；如果不需要额外区分，直接写“人物”即可。
12. 所有区域的视频都要带有极轻微、真实的手持呼吸感，像真人拿着手机在现场拍摄；不能剧烈晃动，也不要写构图口号、画幅口号或硬站位描述。
12.5 每个区域的视频都只能有一个主导镜头动作。你可以选择轻微推近、轻微拉远、轻微平移、轻微后退跟拍、或轻微向桌面聚焦中的一种，但不要在同一条视频里叠加多种明显运镜。
13. Audio: 只允许写背景音、环境音、物体声和空间回响；绝对不要把台词、旁白、解说词或复述文本写进 Audio: 里。
14. duration_seconds 必须是 3 到 15 的整数，并且必须取最短但足够自然的值。
14.5 每个区域优先只保留 1 到 2 个核心信息点，不要把用户总文案里的所有卖点都塞进同一条视频。宁可省掉次要信息，也不要把一条视频写得过满。
14.6 台词密度要明显偏低、偏自然，优先用短句和口语化表达。单条视频更适合 2 到 3 句短台词，不要连续堆很多并列卖点，也不要每句都很长。
14.7 如果某个区域需要表达的信息很多，你必须主动压缩、合并、删减，只保留最能支撑该区域主题的内容；不要为了覆盖所有信息而机械拉长时长或让人物连续说太久。
14.8 同一个项目里的全部区域都必须保持同一种说话人设和语气风格，像同一个真人博主连续拍摄出来的一组视频。整体语气要统一、自然、口语化，不要某一段很正式、某一段很夸张、某一段又突然像广告配音。
14.9 人物大多数时间都应主要看向镜头。只有在确实需要提到身旁、身后或当前区域里的内容时，才允许短暂、有目的地看一眼，然后迅速回到镜头；不要无意义转头、频繁左右看或一直东张西望。
14.10 表情和语气默认要明显积极、高兴、愿意推荐，有自然的笑意、期待感和兴奋感，让人感觉这位博主是真的喜欢、真的愿意推荐这个地方；但不要无缘由干笑、傻笑、突然大笑，情绪必须始终服务于正在说的内容。
14.11 手势可以比之前更丰富，但必须有目的、幅度克制，并且服务于当前一句内容。不要反复点头、反复抬手、重复同一种动作，也不要让头部动作比说话内容更抢戏。
15. entrance 负责第一印象、为什么值得进去和自然开场；其他区域都默认已经进店，不要重复介绍店名或整家店定位。
16. entrance 仍然要以人物为主，只允许极轻微真实手持感配合一种简单运镜，例如极轻微推近、极轻微拉远或极轻微平移。前半段人物只能做微小自然动作，不要离开人物去单独拍门头或招牌，也不要把镜头切成大片门头空镜。说完后允许人物自然转身、背对镜头朝身后的入口走过去，并停留在最后那个收尾姿态；不要在结尾继续说太多话，也不要走出很远。
16.5 private_room 默认人物已经在包间内部，禁止写推门、开门、从门外进入、站在包间门口往里带视线，或任何“先在门外再进入包间”的动作。
17. 除了 table_dishes 以外，lobby、private_room、kitchen、featured_area、promotion 都允许人物在当前区域里做少量自然移动，例如边走边说、缓慢走两步、轻微倒退或转身再继续讲解；镜头可以自然跟随，但动作和运镜都必须简单、克制，不能突然跑动、冲出画面、跨区域移动或切成大片空镜。
18. table_dishes 默认是人物坐在椅子上正对镜头讲整桌内容，镜头以稳定为主，只允许极轻微地向桌面聚焦、极轻微推近或极轻微拉远中的一种，不要设计复杂运镜，也不要安排逐个指向、逐个点名示意桌上内容。更适合围绕整桌的特色、亮点、招牌组合、整体食欲感和推荐理由来讲，不要把结尾写成“接下来去优惠信息看看”。
19. promotion 只讲优惠、套餐、团购、最后结论，不要重新回头讲门头或整家店的基础定位。promotion 结尾必须停留在原地完成收尾，不要说完后离场、走开、转身离开或继续往别处移动。
20. 表情和动作不要太少。你可以让人物根据内容自然地惊喜、推荐、期待、点头、转身、边走边说或做更丰富的小幅手势，只要整体仍然真实自然、像真人现场拍摄。
21. 人物动作和镜头动作都应保持“简单、清楚、单线推进”。优先让人物只完成一个主导动作，让镜头只完成一个主导运镜，这样更像真人现场拍摄，也更适合 LTX2.3 稳定表演。
22. 如果总文案没有直接写出某个区域的具体细节，你可以做适度、克制、不过度具体的合理拆解，但不要编造精确的菜名、价格、活动规则、空间陈设或其他用户没有提供的具体事实。

返回格式示例：
{
  "spots": [
    {
      "spot_type": "entrance",
      "intro_text": "",
      "video_positive_prompt": "...\nAudio: ...",
      "duration_seconds": 6
    }
  ]
}`

	continuityHint := buildStoreVisitProjectContinuityHint(spots)

	userPrompt := fmt.Sprintf(`请根据以下探店项目总文案，一次性生成全部区域的介绍摘要和视频提示词。

项目信息：
- project_name: %s

当前视频基础规格：
- width: %d
- height: %d
- fps: %d

项目总文案：
%s

区域拆解要求：
- entrance：基础场景图里，人物半身特写前景站在画面最前面，背后是门头和入口。负责第一印象、为什么值得进去、自然开场；始终以人物为主，只允许镜头做极轻微推近、极轻微拉远或极轻微平移中的一种，不要单独去拍门头或招牌。人物前半段只做微小自然动作，结尾允许自然转身背对镜头朝身后入口走过去，并停在最后的收尾姿态。
- lobby：基础场景图里，人物半身特写前景站在画面最前面，后方是大厅。负责大厅环境、整洁度、空间感、氛围；不要再重复店名和门头。
- private_room：基础场景图里，人物半身特写前景站在画面最前面，后方是包间。人物默认已经在包间内部。负责包间私密性、舒适度、适合什么聚会场景；不要再重复整家店定位，也不要写推门、开门或从门外进入。
- kitchen：基础场景图里，人物半身特写前景站在画面最前面，后方是厨房或明档。负责厨房或明档的干净程度、现做感、烟火气；不要编太具体的后厨流程。
- featured_area：基础场景图里，人物半身特写前景站在画面最前面，后方是特色区域。负责这家店最有记忆点、最适合打卡或最能区分别家的区域。
- table_dishes：基础场景图里，人物坐着正对镜头，位于画面的后方，人物面前是菜品，桌面内容更靠近镜头。负责人物坐在椅子上面对镜头介绍整桌内容，持续说话，让镜头轻微拉远一点把前方桌面带进来，或轻微向桌面聚焦，要有食欲感；重点更适合讲整桌的特色、亮点、招牌搭配、整体卖相和为什么值得点，不要设计逐个指菜、逐个点名示意或频繁低头找菜，也不要把结尾接到优惠信息。
- promotion：基础场景图里，人物半身特写前景站在画面最前面，后方是当前店内环境。负责优惠、团购、套餐、价格优势、最后推荐结论；不要回头重新介绍门头和环境。结尾必须停留在原地完成收尾，不要离场。

补充要求：
- 每个区域都要返回 intro_text 和 video_positive_prompt。
- intro_text 用于回填编辑框，保持简短。
- video_positive_prompt 要像真实探店短视频里的自然口播镜头，动作、表情、节拍各不相同，但整体都带极轻微真实手持感。
- 每个区域的视频内容都要更克制，不要把信息说得太密。优先只保留 1 到 2 个核心卖点，用 2 到 3 句短台词自然说完。
- 如果一个区域能用更少的话说明白，就主动减少内容，不要为了显得丰富而塞太多描述。
- 人物大多数时间应主要看着镜头说话。只有在确实需要提到当前区域里的某个方向或内容时，才允许短暂、有目的地看一眼后迅速回到镜头；不要无意义转头、频繁左右看或一直东张西望。
- 表情和语气整体要更积极、更高兴、更有推荐感，最好让人感觉这位博主是真心喜欢、说得起劲、愿意分享；可以有自然笑意、期待感和轻快情绪，但不要无缘由干笑、傻笑或突然夸张大笑。
- 手势可以更丰富，但必须有目的、幅度克制，并且服务于当前一句内容。不要让头部动作或重复点头比说话内容更抢戏。
- 每个区域都只保留一个主导镜头动作，不要在一条视频里同时安排推近、拉远、平移、跟拍、仰拍等多套明显运镜。
- 除了 table_dishes 外，其他区域不要总是原地站桩。允许人物一边讲一边慢慢走、转身、轻微倒退或朝当前区域里更有代表性的方向移动一点点，镜头也可以自然跟随，但必须自然、克制、像真人现场拍摄。
- 对普通区域，优先从这些简单镜头里选择一种：轻微推近、轻微拉远、轻微平移、轻微后退跟拍。
- 对 entrance，优先让镜头始终跟着人物，只允许极轻微推近、极轻微拉远或极轻微平移中的一种，不要离开人物去单独拍门头或招牌。更适合前半段面对镜头说，结尾再自然转身背对镜头朝身后入口走过去，并停在最后那一帧。
- 对 promotion，人物可以做微小自然动作，但最后必须原地停住完成收尾，不要离开当前位置。
- 对 table_dishes，只能在“极轻微向桌面聚焦 / 极轻微推近 / 极轻微拉远”里选一种，不要复杂运镜。人物应始终主要看着镜头说，不要写逐个指向桌上具体内容、逐个点名示意、频繁低头找菜或精确比划某一道菜。
- 不要写“竖屏构图、9:16、左侧三分之一、人物居中”这类元描述。
- 如果总文案里已经写了明确优惠或价格，就优先放到 promotion。
- 如果总文案里写了具体品类，例如烧烤、火锅、甜品、奶茶等，你可以围绕这个品类拆解各区域，但不要额外编造用户没写出的过多细节。
- 所有区域都要保持同一种博主口吻和语气风格，像同一个人一镜接一镜连续拍出来的，不要忽然换成另一种说话方式。
- 如果某个区域后面还有已具备参考图或现成画面的下一个区域，那么该区域的最后一句口播必须明确承接到下一个区域，例如「我们去大厅看看。」；不要只做模糊收尾，也不要省略这句承接。但 kitchen 如果后面可拍的是 featured_area，更适合承接成「我们去下个区域看看。」这类更通用的转场。featured_area 如果后面可拍的是 table_dishes，更适合承接成「老板已经准备好了，我们去看看菜品吧。」这类自然转场。table_dishes 不要承接到 promotion，promotion 作为整个项目的独立收尾区域单独完成。
- 如果某个区域后面已经没有可拍区域，才允许自然收尾，不要硬说去下一个地方。

当前项目区域衔接参考：
%s

只返回 JSON。`,
		project.Name,
		storeVisitVideoWidth,
		storeVisitVideoHeight,
		storeVisitDefaultVideoFPS,
		strings.TrimSpace(content),
		continuityHint,
	)

	return systemPrompt, userPrompt
}

func buildStoreVisitProjectContinuityHint(spots []models.StoreVisitSpot) string {
	ordered := storeVisitProjectAutoGenerateSpotTypes
	canRender := map[string]bool{}
	for _, spot := range spots {
		spotType := normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
		if isDeprecatedStoreVisitSpotType(spotType) || spotType == storeVisitSpotTypeDishGeneration {
			continue
		}
		if strings.TrimSpace(spot.ReferenceImage) != "" || strings.TrimSpace(spot.GeneratedImage) != "" {
			canRender[spotType] = true
		}
	}

	lines := make([]string, 0, len(ordered)+2)
	availableNames := make([]string, 0, len(ordered))
	for _, spotType := range ordered {
		if !canRender[spotType] {
			continue
		}
		def := getStoreVisitSpotDefinition(spotType)
		availableNames = append(availableNames, def.Name)
	}

	if len(availableNames) == 0 {
		lines = append(lines, "- 当前项目还没有任何可拍区域参考图，所以所有区域都不要硬写“接下来去哪里看看”的承接，只做自然收尾。")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, fmt.Sprintf("- 当前项目里已具备参考图或现成画面的区域有：%s。", strings.Join(availableNames, "、")))
	lines = append(lines, "- 如果某个区域后面还有可拍区域，那么该区域的最后一句口播必须明确说出“我们去XX看看”这种承接句，而且只能承接到当前项目里已具备参考图或现成画面的下一个区域；如果后面没有合适的可拍区域，才允许自然收尾。")
	for idx, spotType := range ordered {
		def := getStoreVisitSpotDefinition(spotType)
		nextName := "自然收尾"
		for j := idx + 1; j < len(ordered); j++ {
			nextType := ordered[j]
			if canRender[nextType] {
				nextName = fmt.Sprintf("最后一句口播必须明确说：我们去%s看看。", getStoreVisitSpotDefinition(nextType).Name)
				break
			}
		}
		if spotType == storeVisitSpotTypeEntrance {
			for j := idx + 1; j < len(ordered); j++ {
				nextType := ordered[j]
				if canRender[nextType] {
					nextName = "最后一句口播更适合自然承接成：走，我们进去一探究竟。"
					break
				}
			}
		}
		if spotType == storeVisitSpotTypeKitchen {
			for j := idx + 1; j < len(ordered); j++ {
				nextType := ordered[j]
				if nextType == storeVisitSpotTypeFeaturedArea && canRender[nextType] {
					nextName = "最后一句口播更适合自然承接成：我们去下个区域看看。"
					break
				}
			}
		}
		if spotType == storeVisitSpotTypeFeaturedArea {
			for j := idx + 1; j < len(ordered); j++ {
				nextType := ordered[j]
				if nextType == storeVisitSpotTypeTableDishes && canRender[nextType] {
					nextName = "最后一句口播更适合自然承接成：老板已经准备好了，我们去看看菜品吧。"
					break
				}
			}
		}
		if spotType == storeVisitSpotTypeTableDishes {
			nextName = "不要承接到优惠信息，更适合围绕整桌的特色、亮点、食欲感和推荐理由自然收束。"
		}
		lines = append(lines, fmt.Sprintf("- %s：%s", def.Name, nextName))
	}
	return strings.Join(lines, "\n")
}

func parseStoreVisitProjectAutoGenerateResponse(raw string) (*storeVisitProjectAutoGenerateResponse, error) {
	trimmed := cleanupLLMJSON(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty llm response")
	}
	var payload storeVisitProjectAutoGenerateResponse
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, fmt.Errorf("invalid json: %v", err)
	}
	for i := range payload.Spots {
		payload.Spots[i].SpotType = normalizeStoreVisitSpotType(payload.Spots[i].SpotType, "")
		payload.Spots[i].IntroText = strings.TrimSpace(payload.Spots[i].IntroText)
		payload.Spots[i].VideoPositivePrompt = strings.TrimSpace(payload.Spots[i].VideoPositivePrompt)
	}
	return &payload, nil
}

func validateStoreVisitProjectAutoGenerateResponse(payload *storeVisitProjectAutoGenerateResponse) error {
	if payload == nil {
		return fmt.Errorf("project auto-generate payload is nil")
	}
	required := make(map[string]struct{}, len(storeVisitProjectAutoGenerateSpotTypes))
	for _, spotType := range storeVisitProjectAutoGenerateSpotTypes {
		required[spotType] = struct{}{}
	}
	seen := map[string]struct{}{}
	for _, item := range payload.Spots {
		if _, ok := required[item.SpotType]; !ok {
			return fmt.Errorf("unexpected spot_type: %s", item.SpotType)
		}
		if _, exists := seen[item.SpotType]; exists {
			return fmt.Errorf("duplicated spot_type: %s", item.SpotType)
		}
		seen[item.SpotType] = struct{}{}
		if strings.TrimSpace(item.IntroText) == "" {
			return fmt.Errorf("%s intro_text is required", item.SpotType)
		}
		if err := validateStoreVisitVideoPromptInferResponse(&storeVisitVideoPromptInferResponse{
			VideoPositivePrompt: item.VideoPositivePrompt,
			DurationSeconds:     item.DurationSeconds,
		}); err != nil {
			return fmt.Errorf("%s invalid: %w", item.SpotType, err)
		}
	}
	for _, spotType := range storeVisitProjectAutoGenerateSpotTypes {
		if _, ok := seen[spotType]; !ok {
			return fmt.Errorf("missing spot_type: %s", spotType)
		}
	}
	return nil
}

func persistStoreVisitProjectAutoGenerateResult(projectID uint, payload *storeVisitProjectAutoGenerateResponse) error {
	return db.RetryOnBusy(func() error {
		return db.DB.Transaction(func(tx *gorm.DB) error {
			var spots []models.StoreVisitSpot
			if err := tx.Where("project_id = ?", projectID).Find(&spots).Error; err != nil {
				return err
			}
			spotMap := make(map[string]models.StoreVisitSpot, len(spots))
			for _, spot := range spots {
				spotMap[normalizeStoreVisitSpotType(spot.SpotType, spot.Name)] = spot
			}
			now := time.Now()
			for _, item := range payload.Spots {
				spot, ok := spotMap[item.SpotType]
				if !ok {
					continue
				}
				if err := tx.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
					"intro_text":             strings.TrimSpace(item.IntroText),
					"video_positive_prompt":  strings.TrimSpace(item.VideoPositivePrompt),
					"video_duration_seconds": expandStoreVisitAutoGeneratedDuration(item.SpotType, item.DurationSeconds),
					"updated_at":             now,
				}).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
}

func expandStoreVisitAutoGeneratedDuration(spotType string, seconds int) int {
	if seconds <= 0 {
		return 4
	}
	switch normalizeStoreVisitSpotType(spotType, "") {
	case storeVisitSpotTypeEntrance, storeVisitSpotTypePromotion:
		return seconds + 1
	}
	return seconds + 2
}

func ternaryStoreVisitMessage(condition bool, ifTrue string, ifFalse string) string {
	if condition {
		return ifTrue
	}
	return ifFalse
}

func listStoreVisitSpotsForProject(projectID uint) ([]models.StoreVisitSpot, error) {
	var spots []models.StoreVisitSpot
	if err := db.DB.Where("project_id = ?", projectID).Order("sort_order asc, id asc").Find(&spots).Error; err != nil {
		return nil, err
	}
	return spots, nil
}

func storeVisitProjectHasRunningGeneration(projectID uint) (bool, error) {
	var runningCount int64
	if err := db.DB.Model(&models.StoreVisitSpot{}).
		Where("project_id = ? AND (image_status = ? OR video_status = ?)", projectID, "generating", "generating").
		Count(&runningCount).Error; err != nil {
		return false, err
	}
	if runningCount > 0 {
		return true, nil
	}
	if err := db.DB.Model(&models.StoreVisitDishGenerationItem{}).
		Where("project_id = ? AND video_status = ?", projectID, "generating").
		Count(&runningCount).Error; err != nil {
		return false, err
	}
	return runningCount > 0, nil
}

func collectStoreVisitProjectMissingReferences(spots []models.StoreVisitSpot) []storeVisitProjectMissingReference {
	missing := make([]storeVisitProjectMissingReference, 0)
	for _, spot := range spots {
		spotType := normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
		if spotType == storeVisitSpotTypeDishGeneration || isDeprecatedStoreVisitSpotType(spotType) {
			continue
		}
		if strings.TrimSpace(spot.ReferenceImage) != "" {
			continue
		}
		if strings.TrimSpace(spot.GeneratedImage) != "" {
			continue
		}
		missing = append(missing, storeVisitProjectMissingReference{
			SpotID:   spot.ID,
			SpotType: spotType,
			Name:     getStoreVisitSpotDisplayName(spot),
		})
	}
	return missing
}

func storeVisitProjectHasRunnableDishGeneration(spots []models.StoreVisitSpot) bool {
	var dishSpot *models.StoreVisitSpot
	for i := range spots {
		if normalizeStoreVisitSpotType(spots[i].SpotType, spots[i].Name) == storeVisitSpotTypeDishGeneration {
			dishSpot = &spots[i]
			break
		}
	}
	if dishSpot == nil {
		return false
	}
	items, err := listStoreVisitDishGenerationItemsBySpot(dishSpot.ID)
	if err != nil {
		return false
	}
	for _, item := range items {
		if len(decodeStoreVisitDishGenerationFrames(item)) >= 2 {
			return true
		}
	}
	return false
}

func queueAndRenderStoreVisitProjectImages(taskID string, project models.StoreVisitProject, spots []models.StoreVisitSpot) (gin.H, error) {
	skippedMissing := []string{}
	skippedGenerated := []string{}
	failed := []string{}
	queued := make([]queuedStoreVisitSpotRender, 0)

	if strings.TrimSpace(project.BloggerReferenceImage) == "" {
		for _, spot := range spots {
			spotType := normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
			if spotType == storeVisitSpotTypeDishGeneration || isDeprecatedStoreVisitSpotType(spotType) {
				continue
			}
			skippedMissing = append(skippedMissing, getStoreVisitSpotDisplayName(spot))
		}
		return gin.H{
			"queued":            0,
			"skipped_generated": skippedGenerated,
			"skipped_missing":   skippedMissing,
			"failed":            failed,
		}, nil
	}

	template, err := loadStoreVisitWorkflowTemplate(storeVisitImageWorkflowPath)
	if err != nil {
		return nil, err
	}
	bloggerRefAbs, err := assetWebPathToAbs(project.BloggerReferenceImage)
	if err != nil {
		return nil, err
	}
	bloggerImageName, err := UploadToComfyUIInput(bloggerRefAbs)
	if err != nil {
		return nil, err
	}

	for idx, spot := range spots {
		spotType := normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
		if spotType == storeVisitSpotTypeDishGeneration || isDeprecatedStoreVisitSpotType(spotType) {
			continue
		}
		label := getStoreVisitSpotDisplayName(spot)
		if strings.TrimSpace(spot.ReferenceImage) == "" {
			skippedMissing = append(skippedMissing, label)
			continue
		}
		if strings.TrimSpace(spot.GeneratedImage) != "" && spot.ImageStatus == "generated" {
			skippedGenerated = append(skippedGenerated, label)
			continue
		}

		spotRefAbs, err := assetWebPathToAbs(spot.ReferenceImage)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		spotImageName, err := UploadToComfyUIInput(spotRefAbs)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", label, err))
			continue
		}

		seed := getConfiguredGlobalSeed() + int64(idx*17+1)
		workflowJSON, err := buildStoreVisitImageWorkflow(template, bloggerImageName, spotImageName, spot, project, seed)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		logComfyWorkflowPayload("Store Visit Image ComfyUI Payload", workflowDisplayNameFromPath(storeVisitImageWorkflowPath), workflowJSON)
		promptID, err := QueueComfyPrompt(workflowJSON)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		if err := db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
			"image_status":             "generating",
			"image_current_task_id":    taskID,
			"image_last_error":         "",
			"generated_image":          "",
			"image_generated_workflow": "",
			"video_status":             "draft",
			"video_current_task_id":    "",
			"video_last_error":         "",
			"generated_video":          "",
			"video_generated_workflow": "",
			"updated_at":               time.Now(),
		}).Error; err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		BroadcastUpdate("store_visit_spot", spot.ID)
		queued = append(queued, queuedStoreVisitSpotRender{
			Spot:          spot,
			PromptID:      promptID,
			WorkflowLabel: workflowDisplayNameFromPath(storeVisitImageWorkflowPath),
		})
	}

	for idx, item := range queued {
		task.GlobalTaskManager.UpdateTaskProgress(taskID, 52+int(float64(idx+1)/float64(maxInt(1, len(queued)))*12), fmt.Sprintf("等待图片生成：%s", getStoreVisitSpotDisplayName(item.Spot)))
		webPath, err := waitForStoreVisitImageOutput(item.PromptID, project.Code, getStoreVisitSpotFileKey(item.Spot), item.Spot.ID, func() bool {
			return shouldApplyStoreVisitImageTaskResult(item.Spot.ID, taskID)
		})
		if err != nil || strings.TrimSpace(webPath) == "" {
			msg := "未获取到图片输出"
			if err != nil {
				msg = err.Error()
			}
			failed = append(failed, fmt.Sprintf("%s: %s", getStoreVisitSpotDisplayName(item.Spot), msg))
			_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", item.Spot.ID).Updates(map[string]interface{}{
				"image_status":          "failed",
				"image_current_task_id": "",
				"image_last_error":      msg,
				"updated_at":            time.Now(),
			}).Error
			BroadcastUpdate("store_visit_spot", item.Spot.ID)
			continue
		}
		_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", item.Spot.ID).Updates(map[string]interface{}{
			"generated_image":          webPath,
			"image_status":             "generated",
			"image_current_task_id":    "",
			"image_last_error":         "",
			"image_generated_workflow": item.WorkflowLabel,
			"updated_at":               time.Now(),
		}).Error
		BroadcastUpdate("store_visit_spot", item.Spot.ID)
	}

	return gin.H{
		"queued":            len(queued),
		"skipped_generated": skippedGenerated,
		"skipped_missing":   skippedMissing,
		"failed":            failed,
	}, nil
}

func queueAndRenderStoreVisitProjectVideos(taskID string, project models.StoreVisitProject, spots []models.StoreVisitSpot) (gin.H, error) {
	skippedMissing := []string{}
	skippedGenerated := []string{}
	failed := []string{}
	queued := make([]queuedStoreVisitSpotRender, 0)

	for idx, spot := range spots {
		spotType := normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
		if spotType == storeVisitSpotTypeDishGeneration || isDeprecatedStoreVisitSpotType(spotType) {
			continue
		}
		label := getStoreVisitSpotDisplayName(spot)
		if strings.TrimSpace(spot.GeneratedImage) == "" {
			skippedMissing = append(skippedMissing, label)
			continue
		}
		if strings.TrimSpace(spot.VideoPositivePrompt) == "" {
			skippedMissing = append(skippedMissing, label)
			continue
		}
		if strings.TrimSpace(spot.GeneratedVideo) != "" && spot.VideoStatus == "generated" {
			skippedGenerated = append(skippedGenerated, label)
			continue
		}
		seed := getConfiguredGlobalSeed() + int64(idx*37+101)
		workflowJSON, workflowLabel, err := buildStoreVisitVideoWorkflow(spot, project, seed)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		logComfyWorkflowPayload("Store Visit Video ComfyUI Payload", workflowLabel, workflowJSON)
		promptID, err := QueueComfyPrompt(workflowJSON)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		if err := db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
			"video_status":             "generating",
			"video_current_task_id":    taskID,
			"video_last_error":         "",
			"generated_video":          "",
			"video_generated_workflow": "",
			"updated_at":               time.Now(),
		}).Error; err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		BroadcastUpdate("store_visit_spot", spot.ID)
		queued = append(queued, queuedStoreVisitSpotRender{
			Spot:          spot,
			PromptID:      promptID,
			WorkflowLabel: workflowLabel,
		})
	}

	for idx, item := range queued {
		task.GlobalTaskManager.UpdateTaskProgress(taskID, 74+int(float64(idx+1)/float64(maxInt(1, len(queued)))*12), fmt.Sprintf("等待视频生成：%s", getStoreVisitSpotDisplayName(item.Spot)))
		webPath, err := waitForStoreVisitVideoOutput(item.PromptID, project.Code, getStoreVisitSpotFileKey(item.Spot), item.Spot.ID, func() bool {
			return shouldApplyStoreVisitVideoTaskResult(item.Spot.ID, taskID)
		})
		if err != nil || strings.TrimSpace(webPath) == "" {
			msg := "未获取到视频输出"
			if err != nil {
				msg = err.Error()
			}
			failed = append(failed, fmt.Sprintf("%s: %s", getStoreVisitSpotDisplayName(item.Spot), msg))
			_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", item.Spot.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      msg,
				"updated_at":            time.Now(),
			}).Error
			BroadcastUpdate("store_visit_spot", item.Spot.ID)
			continue
		}
		_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", item.Spot.ID).Updates(map[string]interface{}{
			"generated_video":          webPath,
			"video_status":             "generated",
			"video_current_task_id":    "",
			"video_last_error":         "",
			"video_generated_workflow": item.WorkflowLabel,
			"updated_at":               time.Now(),
		}).Error
		BroadcastUpdate("store_visit_spot", item.Spot.ID)
	}

	return gin.H{
		"queued":            len(queued),
		"skipped_generated": skippedGenerated,
		"skipped_missing":   skippedMissing,
		"failed":            failed,
	}, nil
}

func queueAndRenderStoreVisitProjectDishVideos(taskID string, project models.StoreVisitProject, spots []models.StoreVisitSpot) (gin.H, error) {
	var dishSpot *models.StoreVisitSpot
	for i := range spots {
		if normalizeStoreVisitSpotType(spots[i].SpotType, spots[i].Name) == storeVisitSpotTypeDishGeneration {
			dishSpot = &spots[i]
			break
		}
	}
	if dishSpot == nil {
		return gin.H{"queued": 0, "skipped": []string{"菜品生成区域不存在"}, "failed": []string{}}, nil
	}
	items, err := listStoreVisitDishGenerationItemsBySpot(dishSpot.ID)
	if err != nil {
		return nil, err
	}
	skipped := []string{}
	failed := []string{}
	queued := make([]queuedStoreVisitDishRender, 0)

	for idx, item := range items {
		label := fmt.Sprintf("菜品生成 %d", item.SortOrder)
		if len(decodeStoreVisitDishGenerationFrames(item)) < 2 {
			skipped = append(skipped, label)
			continue
		}
		if strings.TrimSpace(item.GeneratedVideo) != "" && item.VideoStatus == "generated" {
			skipped = append(skipped, label)
			continue
		}
		seed := getConfiguredGlobalSeed() + int64(idx*53+201)
		workflowJSON, workflowLabel, err := buildStoreVisitDishGenerationWorkflow(item, *dishSpot, project, seed)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		logComfyWorkflowPayload("Store Visit Dish Generation ComfyUI Payload", workflowLabel, workflowJSON)
		promptID, err := QueueComfyPrompt(workflowJSON)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		if err := db.DB.Model(&models.StoreVisitDishGenerationItem{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
			"video_status":             "generating",
			"video_current_task_id":    taskID,
			"video_last_error":         "",
			"generated_video":          "",
			"video_generated_workflow": "",
			"updated_at":               time.Now(),
		}).Error; err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", label, err))
			continue
		}
		queued = append(queued, queuedStoreVisitDishRender{
			Item:          item,
			PromptID:      promptID,
			WorkflowLabel: workflowLabel,
		})
	}

	for idx, item := range queued {
		task.GlobalTaskManager.UpdateTaskProgress(taskID, 88+int(float64(idx+1)/float64(maxInt(1, len(queued)))*10), fmt.Sprintf("等待菜品视频生成：%d", item.Item.SortOrder))
		webPath, err := waitForStoreVisitDishGenerationVideoOutput(item.PromptID, project.Code, getStoreVisitSpotFileKey(*dishSpot), item.Item.ID, func() bool {
			return shouldApplyStoreVisitDishGenerationTaskResult(item.Item.ID, taskID)
		})
		if err != nil || strings.TrimSpace(webPath) == "" {
			msg := "未获取到菜品视频输出"
			if err != nil {
				msg = err.Error()
			}
			failed = append(failed, fmt.Sprintf("菜品生成 %d: %s", item.Item.SortOrder, msg))
			_ = db.DB.Model(&models.StoreVisitDishGenerationItem{}).Where("id = ?", item.Item.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      msg,
				"updated_at":            time.Now(),
			}).Error
			continue
		}
		_ = db.DB.Model(&models.StoreVisitDishGenerationItem{}).Where("id = ?", item.Item.ID).Updates(map[string]interface{}{
			"generated_video":          webPath,
			"video_status":             "generated",
			"video_current_task_id":    "",
			"video_last_error":         "",
			"video_generated_workflow": item.WorkflowLabel,
			"updated_at":               time.Now(),
		}).Error
	}

	return gin.H{
		"queued":  len(queued),
		"skipped": skipped,
		"failed":  failed,
	}, nil
}
