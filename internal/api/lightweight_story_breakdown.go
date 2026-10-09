package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"kt-ai-studio/internal/models"
)

type lightweightStoryNarrativeNode struct {
	ID           int      `json:"id"`
	Type         string   `json:"type"`
	Summary      string   `json:"summary"`
	Characters   []string `json:"characters"`
	LocationHint string   `json:"location_hint"`
}

type lightweightStoryBreakdownResponse struct {
	TotalNodes     int                             `json:"total_nodes"`
	NarrativeNodes []lightweightStoryNarrativeNode `json:"narrative_nodes"`
}

func buildLightweightStoryBreakdownPrompts(project models.Project, req models.AutoGenerateRequest) (string, string, error) {
	if strings.TrimSpace(req.Plot) == "" {
		return "", "", fmt.Errorf("plot is required")
	}
	systemPrompt := `你是一位专业影视剧本拆解师。你的唯一任务：把用户输入的剧本原文拆解成「叙事节点」清单。
叙事节点是驱动故事前进的导演化单元，每个节点都独立成立，是后续分镜的强制数量锚点。

节点分类（type 只允许以下五种，必须原样使用）：
- 地点切换：故事移动到新的物理空间
- 时间跳跃：故事发生明显时间推移
- 动作链断点：一个完整的行动序列，例如进场、搜寻、交手、逃脱、组装、追逐
- 结果落点：一个事件的结论、揭示、发现或后果
- 情绪转折：人物关系或内心状态发生明确转变

拆解要求：
1. 通读全文后按事件真实发生顺序列出节点，节点之间必须是线性改编顺序，禁止重组。
2. 每个节点只容纳一个主导事件；同一空间同一连续动作不要重复切分。
3. 节点总数必须足以完整覆盖主线、冲突推进、关键对白和结局；但不要为了凑数把单个动作切碎。
4. 每个节点输出 5 个字段：
   - id：从 1 开始连续递增
   - type：上面五种之一
   - summary：1 到 2 句简体中文概括该节点的剧情推进或信息落点
   - characters：该节点实际参与推进的关键角色名数组；若没有明确参与者返回空数组
   - location_hint：该节点所在空间的可见轮廓描述（环境、建筑、器物、光线层次）；若空间在画面中不重要可返回空字符串
5. 只返回一个 JSON 对象，禁止输出 JSON 之外的任何解释、标题、注释或代码块标记。

返回格式（仅此结构）：
{
  "total_nodes": 3,
  "narrative_nodes": [
    {
      "id": 1,
      "type": "地点切换",
      "summary": "",
      "characters": [],
      "location_hint": ""
    }
  ]
}`

	userPrompt := fmt.Sprintf(`请根据以下输入，拆解出完整的叙事节点清单。

项目信息：
- project_name: %s
- project_description: %s
- episode: %d

剧本全文：
%s

请输出叙事节点清单 JSON。`,
		strings.TrimSpace(project.Name),
		strings.TrimSpace(project.Description),
		req.Episode,
		strings.TrimSpace(req.Plot),
	)

	return systemPrompt, userPrompt, nil
}

func parseLightweightStoryBreakdown(raw string) (*lightweightStoryBreakdownResponse, error) {
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

	var payload lightweightStoryBreakdownResponse
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, fmt.Errorf("invalid json: %v", err)
	}
	if payload.TotalNodes == 0 {
		payload.TotalNodes = len(payload.NarrativeNodes)
	}
	if payload.TotalNodes < 1 || len(payload.NarrativeNodes) == 0 {
		return nil, fmt.Errorf("narrative breakdown must contain at least one node")
	}
	seenIDs := map[int]bool{}
	for i := range payload.NarrativeNodes {
		node := &payload.NarrativeNodes[i]
		node.Summary = strings.TrimSpace(node.Summary)
		node.Type = strings.TrimSpace(node.Type)
		node.LocationHint = strings.TrimSpace(node.LocationHint)
		if node.ID <= 0 {
			return nil, fmt.Errorf("narrative node %d id must be greater than 0", i+1)
		}
		if seenIDs[node.ID] {
			return nil, fmt.Errorf("narrative node id %d is duplicated", node.ID)
		}
		seenIDs[node.ID] = true
		if node.Type == "" {
			return nil, fmt.Errorf("narrative node %d type is required", node.ID)
		}
		if node.Summary == "" {
			return nil, fmt.Errorf("narrative node %d summary is required", node.ID)
		}
		if node.Characters == nil {
			node.Characters = []string{}
		}
	}
	for _, node := range payload.NarrativeNodes {
		nodeType := strings.TrimSpace(node.Type)
		switch nodeType {
		case "地点切换", "时间跳跃", "动作链断点", "结果落点", "情绪转折":
		default:
			return nil, fmt.Errorf("narrative node %d type %q is not in the allowed set", node.ID, nodeType)
		}
	}

	return &payload, nil
}

func runLightweightStoryBreakdown(project models.Project, req models.AutoGenerateRequest, provider models.LLMProvider, taskID string) (*lightweightStoryBreakdownResponse, error) {
	systemPrompt, userPrompt, err := buildLightweightStoryBreakdownPrompts(project, req)
	if err != nil {
		return nil, err
	}

	Log(
		LogLevelInfo,
		llmLogMessage("LLM Request", provider),
		fmt.Sprintf("Starting H3 short narrative breakdown for project=%d episode=%d", project.ID, req.Episode),
	)

	raw, err := requestLightweightStoryOnce(provider, systemPrompt, userPrompt, taskID)
	if err != nil {
		Log(
			LogLevelError,
			llmLogMessage("LLM Error", provider),
			fmt.Sprintf("H3 short narrative breakdown failed: %v", err),
		)
		return nil, err
	}

	Log(
		LogLevelInfo,
		llmLogMessage("LLM 完整返回(H3 短剧前置分镜节点清单)", provider),
		raw,
	)

	payload, err := parseLightweightStoryBreakdown(raw)
	if err != nil {
		Log(
			LogLevelError,
			llmLogMessage("LLM 返回解析失败(H3 短剧前置分镜节点清单)", provider),
			err.Error(),
		)
		return nil, err
	}

	Log(
		LogLevelInfo,
		llmLogMessage("H3 短剧前置分镜节点清单确认", provider),
		fmt.Sprintf("total_nodes=%d", payload.TotalNodes),
	)

	return payload, nil
}