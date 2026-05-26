package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"

	"github.com/gin-gonic/gin"
	"github.com/sashabaranov/go-openai"
	"gorm.io/gorm"
)

const (
	qwenTTSOutputRoot      = "output/qwen_tts"
	qwenTTSWorkflowPath    = "workflows/Qwen3-TTS Voice Clone (Reference).json"
	qwenTTSASRWorkflowPath = "workflows/asr.json"
	qwenTTSDefaultTemp     = 0.5
)

type qwenTTSAutoParseScriptTaskPayload struct {
	ProjectID        uint   `json:"project_id"`
	SourceText       string `json:"source_text"`
	ResumeFromTaskID string `json:"resume_from_task_id"`
	Mode             string `json:"mode"`
}

type qwenTTSAutoParseItem struct {
	CharacterName string `json:"character_name"`
	Text          string `json:"text"`
	Instruct      string `json:"instruct"`
}

type qwenTTSAutoParseResponse struct {
	Total int                    `json:"total"`
	Items []qwenTTSAutoParseItem `json:"items"`
}

type qwenTTSAutoParseContinuationResponse struct {
	Total int                    `json:"total"`
	Items []qwenTTSAutoParseItem `json:"items"`
}

type qwenTTSAutoParseResumeInfo struct {
	Task              *models.Task           `json:"task,omitempty"`
	Stream            *models.LLMStreamState `json:"stream,omitempty"`
	Resumable         bool                   `json:"resumable"`
	BaseReturnedCount int                    `json:"base_returned_count"`
	ReturnedCount     int                    `json:"returned_count"`
	Total             int                    `json:"total"`
	SourceText        string                 `json:"source_text"`
	PartialJSON       string                 `json:"partial_json"`
}

type qwenTTSAutoParseStoryState struct {
	ProjectID     uint                   `json:"project_id"`
	SourceText    string                 `json:"source_text"`
	PartialJSON   string                 `json:"partial_json"`
	ReturnedCount int                    `json:"returned_count"`
	Total         int                    `json:"total"`
	LastError     string                 `json:"last_error"`
	LastTaskID    string                 `json:"last_task_id"`
	Status        string                 `json:"status"`
	CanContinue   bool                   `json:"can_continue"`
	CanRestart    bool                   `json:"can_restart"`
	Task          *models.Task           `json:"task,omitempty"`
	Stream        *models.LLMStreamState `json:"stream,omitempty"`
}

type qwenTTSAutoParsePartialScan struct {
	Response         *qwenTTSAutoParseResponse
	Broken           bool
	FirstBrokenIndex int
}

type qwenTTSAutoParseStreamValidationError struct {
	BrokenIndex int
}

func (e *qwenTTSAutoParseStreamValidationError) Error() string {
	if e == nil {
		return "Qwen3 TTS 自动解析流式校验失败"
	}
	return fmt.Sprintf("Qwen3 TTS 自动解析在第 %d 条起检测到结构损坏，已中断当前输出", e.BrokenIndex)
}

func parseQwenTTSAutoParsePartialJSON(content string) *qwenTTSAutoParseResponse {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}
	var resp qwenTTSAutoParseResponse
	if err := json.Unmarshal([]byte(trimmed), &resp); err != nil {
		return nil
	}
	if resp.Total <= 0 || len(resp.Items) == 0 {
		return nil
	}
	return &resp
}

var qwenTTSTotalFieldPattern = regexp.MustCompile(`"total"\s*:\s*(\d+)`)

func buildQwenTTSAutoParseResumeInfo(taskRecord *models.Task, stream *models.LLMStreamState, sourceText string, lastError string) *qwenTTSAutoParseResumeInfo {
	info := &qwenTTSAutoParseResumeInfo{
		Task:       taskRecord,
		Stream:     stream,
		SourceText: strings.TrimSpace(sourceText),
	}

	totalFromBase := 0
	if taskRecord != nil {
		var payload qwenTTSAutoParseScriptTaskPayload
		if err := json.Unmarshal([]byte(taskRecord.Payload), &payload); err == nil {
			if info.SourceText == "" {
				info.SourceText = strings.TrimSpace(payload.SourceText)
			}
			if resumeTaskID := strings.TrimSpace(payload.ResumeFromTaskID); resumeTaskID != "" {
				if resumePartial, err := extractQwenTTSAutoParsePartial(getTaskLatestLLMStreamContent(resumeTaskID)); err == nil {
					info.BaseReturnedCount = len(resumePartial.Items)
					totalFromBase = resumePartial.Total
				}
			}
		}
	}

	if stream != nil {
		if partial, err := extractQwenTTSAutoParsePartial(stream.Content); err == nil {
			info.ReturnedCount = info.BaseReturnedCount + len(partial.Items)
			info.PartialJSON = marshalQwenTTSAutoParsePartialJSON(partial.Total, partial.Items)
			if partial.Total > 0 {
				info.Total = partial.Total
			} else {
				info.Total = totalFromBase
			}
		} else {
			info.ReturnedCount = info.BaseReturnedCount
			info.Total = extractQwenTTSTotalFromPartial(stream.Content)
			if info.Total == 0 {
				info.Total = totalFromBase
			}
		}
	} else {
		info.ReturnedCount = info.BaseReturnedCount
		info.Total = totalFromBase
	}

	if taskRecord != nil {
		info.Resumable = taskRecord.Status == "failed" && info.ReturnedCount > 0
		if !info.Resumable && strings.TrimSpace(taskRecord.Error) == "" && strings.TrimSpace(lastError) != "" {
			info.Task.Error = strings.TrimSpace(lastError)
		}
		return info
	}

	if info.ReturnedCount > 0 {
		synthesizedTask := &models.Task{
			ID: strings.TrimSpace(func() string {
				if stream != nil {
					return stream.TaskID
				}
				return ""
			}()),
			Type:      "auto_parse_qwen_tts_script",
			Status:    "failed",
			Progress:  0,
			Error:     strings.TrimSpace(lastError),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if stream != nil {
			synthesizedTask.CreatedAt = stream.CreatedAt
			synthesizedTask.UpdatedAt = stream.UpdatedAt
		}
		if synthesizedTask.Error == "" {
			synthesizedTask.Error = "上一次自动解析任务在后端重启后丢失，但已保留部分返回内容，可继续解析剩余内容"
		}
		info.Task = synthesizedTask
		info.Resumable = true
	}
	return info
}

func applyProjectPartialToQwenTTSAutoParseResumeInfo(info *qwenTTSAutoParseResumeInfo, projectPartial *qwenTTSAutoParseResponse) *qwenTTSAutoParseResumeInfo {
	if info == nil || projectPartial == nil || len(projectPartial.Items) == 0 {
		return info
	}
	info.BaseReturnedCount = len(projectPartial.Items)
	info.ReturnedCount = len(projectPartial.Items)
	info.Total = projectPartial.Total
	info.PartialJSON = marshalQwenTTSAutoParsePartialJSON(projectPartial.Total, projectPartial.Items)

	if info.Stream == nil || strings.TrimSpace(info.Stream.Content) == "" {
		return info
	}

	streamPartial, err := extractQwenTTSAutoParsePartial(info.Stream.Content)
	if err != nil || streamPartial == nil || len(streamPartial.Items) == 0 {
		return info
	}

	knownTotal := projectPartial.Total
	if knownTotal <= 0 {
		knownTotal = streamPartial.Total
	}
	mergedItems := mergeQwenTTSAutoParseItems(projectPartial.Items, streamPartial.Items)
	if len(mergedItems) > len(projectPartial.Items) && (knownTotal <= 0 || len(mergedItems) <= knownTotal) {
		info.ReturnedCount = len(mergedItems)
		info.Total = knownTotal
		info.PartialJSON = marshalQwenTTSAutoParsePartialJSON(knownTotal, mergedItems)
	}
	return info
}

func mergeQwenTTSAutoParsePersistedAndLivePartial(persisted *qwenTTSAutoParseResponse, liveContent string) *qwenTTSAutoParseResponse {
	live, liveErr := extractQwenTTSAutoParsePartial(liveContent)
	if persisted == nil {
		if liveErr != nil {
			return nil
		}
		return live
	}
	if liveErr != nil || live == nil || len(live.Items) == 0 {
		return persisted
	}
	knownTotal := persisted.Total
	if knownTotal <= 0 {
		knownTotal = live.Total
	}
	mergedItems := mergeQwenTTSAutoParseItems(persisted.Items, live.Items)
	if knownTotal > 0 && len(mergedItems) > knownTotal {
		trimmed := trimQwenTTSAutoParseItemsToTotal(mergedItems, knownTotal)
		if len(trimmed) != knownTotal {
			return persisted
		}
		mergedItems = trimmed
	}
	return &qwenTTSAutoParseResponse{
		Total: knownTotal,
		Items: mergedItems,
	}
}

func buildQwenTTSAutoParseStoryState(project models.QwenTTSProject) (*qwenTTSAutoParseStoryState, error) {
	state := &qwenTTSAutoParseStoryState{
		ProjectID:   project.ID,
		SourceText:  strings.TrimSpace(project.AutoParseSourceText),
		PartialJSON: strings.TrimSpace(project.AutoParsePartialJSON),
		ReturnedCount: func() int {
			if project.AutoParseReturnedCount > 0 {
				return project.AutoParseReturnedCount
			}
			return 0
		}(),
		Total:      project.AutoParseTotal,
		LastError:  strings.TrimSpace(project.LastAutoParseError),
		LastTaskID: strings.TrimSpace(project.LastAutoParseTaskID),
		Status:     "idle",
	}

	projectPartial := parseQwenTTSAutoParsePartialJSON(project.AutoParsePartialJSON)
	if projectPartial != nil {
		state.PartialJSON = marshalQwenTTSAutoParsePartialJSON(projectPartial.Total, projectPartial.Items)
		state.ReturnedCount = len(projectPartial.Items)
		if projectPartial.Total > 0 {
			state.Total = projectPartial.Total
		}
	}

	if state.LastTaskID != "" {
		var taskRecord models.Task
		if err := db.DB.Where("id = ? AND type = ?", state.LastTaskID, "auto_parse_qwen_tts_script").First(&taskRecord).Error; err == nil {
			maybeMarkQwenTTSAutoParseTaskStale(&taskRecord)
			state.Task = &taskRecord
			state.Stream = getTaskLatestLLMStream(state.LastTaskID)
		} else {
			state.Stream = getTaskLatestLLMStream(state.LastTaskID)
		}
	}

	if state.Stream != nil {
		if merged := mergeQwenTTSAutoParsePersistedAndLivePartial(projectPartial, state.Stream.Content); merged != nil {
			state.PartialJSON = marshalQwenTTSAutoParsePartialJSON(merged.Total, merged.Items)
			state.ReturnedCount = len(merged.Items)
			if merged.Total > 0 {
				state.Total = merged.Total
			}
		} else if state.Total <= 0 {
			state.Total = extractQwenTTSTotalFromPartial(state.Stream.Content)
		}
	}

	switch {
	case state.Task != nil && (state.Task.Status == "pending" || state.Task.Status == "running"):
		state.Status = state.Task.Status
	case state.Total > 0 && state.ReturnedCount >= state.Total:
		state.Status = "completed"
	case state.LastError != "":
		state.Status = "failed"
	case state.SourceText != "":
		state.Status = "draft"
	}
	state.CanContinue = state.Status != "running" && state.Status != "pending" && state.ReturnedCount > 0 && state.Total > state.ReturnedCount
	state.CanRestart = state.SourceText != ""
	return state, nil
}

func loadQwenTTSAutoParseStoryState(projectID uint) (*qwenTTSAutoParseStoryState, error) {
	if projectID == 0 {
		return nil, nil
	}
	var project models.QwenTTSProject
	if err := db.DB.Select("id", "auto_parse_source_text", "auto_parse_partial_json", "auto_parse_returned_count", "auto_parse_total", "last_auto_parse_task_id", "last_auto_parse_error").First(&project, projectID).Error; err != nil {
		return nil, err
	}
	return buildQwenTTSAutoParseStoryState(project)
}

func marshalQwenTTSAutoParsePartialJSON(total int, items []qwenTTSAutoParseItem) string {
	resp := qwenTTSAutoParseResponse{
		Total: total,
		Items: items,
	}
	bytes, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return ""
	}
	return string(bytes)
}

func persistQwenTTSAutoParseState(projectID uint, sourceText string, taskID string, lastError string, partial *qwenTTSAutoParseResponse, preserveExistingPartial bool) {
	updates := map[string]interface{}{
		"auto_parse_source_text":  strings.TrimSpace(sourceText),
		"last_auto_parse_task_id": strings.TrimSpace(taskID),
		"last_auto_parse_error":   strings.TrimSpace(lastError),
		"updated_at":              time.Now(),
	}
	if partial != nil && len(partial.Items) > 0 {
		updates["auto_parse_partial_json"] = marshalQwenTTSAutoParsePartialJSON(partial.Total, partial.Items)
		updates["auto_parse_returned_count"] = len(partial.Items)
		updates["auto_parse_total"] = partial.Total
	} else if !preserveExistingPartial {
		updates["auto_parse_partial_json"] = ""
		updates["auto_parse_returned_count"] = 0
		updates["auto_parse_total"] = 0
	}
	_ = db.DB.Model(&models.QwenTTSProject{}).Where("id = ?", projectID).Updates(updates).Error
}

func maybeMarkQwenTTSAutoParseTaskStale(taskRecord *models.Task) bool {
	if taskRecord == nil {
		return false
	}
	if taskRecord.Type != "auto_parse_qwen_tts_script" {
		return false
	}
	if taskRecord.Status != "running" && taskRecord.Status != "pending" {
		return false
	}
	if time.Since(taskRecord.UpdatedAt) < 90*time.Second {
		return false
	}
	if stream := getTaskLatestLLMStream(taskRecord.ID); stream != nil {
		if stream.Status == "running" && time.Since(stream.UpdatedAt) < 90*time.Second {
			return false
		}
		if stream.Status == "completed" && time.Since(stream.UpdatedAt) < 2*time.Minute {
			return false
		}
		if stream.Status == "failed" && time.Since(stream.UpdatedAt) < 2*time.Minute {
			return false
		}
	}
	message := "自动解析任务长时间未更新，可能因后端重启或 LLM 断流中断，已自动标记为失败，可继续解析剩余内容"
	task.GlobalTaskManager.UpdateTaskStatus(taskRecord.ID, "failed", taskRecord.Progress, message)
	_ = db.DB.Model(&models.QwenTTSProject{}).Where("last_auto_parse_task_id = ?", taskRecord.ID).Updates(map[string]interface{}{
		"last_auto_parse_error": message,
		"updated_at":            time.Now(),
	}).Error
	taskRecord.Status = "failed"
	taskRecord.Error = message
	taskRecord.UpdatedAt = time.Now()
	return true
}

func qwenTTSProjectDir(code string) string {
	return filepath.Join(qwenTTSOutputRoot, code)
}

func qwenTTSCharacterDir(code string) string {
	return filepath.Join(qwenTTSProjectDir(code), "characters")
}

func qwenTTSGeneratedDir(code string) string {
	return filepath.Join(qwenTTSProjectDir(code), "generated")
}

func qwenTTSCharacterAudioPath(code string, id uint, ext string) string {
	return filepath.Join(qwenTTSCharacterDir(code), fmt.Sprintf("character_%d%s", id, ext))
}

func normalizeQwenTTSTemperature(value float64) float64 {
	if value <= 0 {
		return qwenTTSDefaultTemp
	}
	if value < 0.1 {
		return 0.1
	}
	if value > 2 {
		return 2
	}
	return value
}

func cleanupQwenTTSInferJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
			lines = lines[1:]
		}
		if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
		trimmed = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	arrayStart := strings.Index(trimmed, "[")
	arrayEnd := strings.LastIndex(trimmed, "]")
	objectStart := strings.Index(trimmed, "{")
	objectEnd := strings.LastIndex(trimmed, "}")
	if arrayStart >= 0 && arrayEnd >= arrayStart && (objectStart == -1 || arrayStart < objectStart) {
		trimmed = trimmed[arrayStart : arrayEnd+1]
	} else if objectStart >= 0 && objectEnd >= objectStart {
		trimmed = trimmed[objectStart : objectEnd+1]
	}
	trimmed = normalizeLLMJSONTypography(trimmed)
	trimmed = normalizeQwenTTSInferJSONStringQuotes(trimmed)
	return strings.TrimSpace(trimmed)
}

func cleanupQwenTTSInferPartial(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	if marker := strings.Index(trimmed, "[解析失败]"); marker >= 0 {
		trimmed = strings.TrimSpace(trimmed[:marker])
	}
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
			lines = lines[1:]
		}
		if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
		trimmed = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	trimmed = normalizeLLMJSONTypography(trimmed)
	trimmed = normalizeQwenTTSInferJSONStringQuotes(trimmed)
	return strings.TrimSpace(trimmed)
}

func stripQwenTTSMarkdownFence(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func extractQwenTTSJSONObjectCandidate(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	objectStart := strings.Index(trimmed, "{")
	objectEnd := strings.LastIndex(trimmed, "}")
	if objectStart >= 0 && objectEnd >= objectStart {
		return strings.TrimSpace(trimmed[objectStart : objectEnd+1])
	}
	return trimmed
}

type qwenTTSJSONStringKind int

const (
	qwenTTSJSONStringNone qwenTTSJSONStringKind = iota
	qwenTTSJSONStringKey
	qwenTTSJSONStringValue
)

func normalizeQwenTTSInferJSONStringQuotes(input string) string {
	if strings.TrimSpace(input) == "" {
		return input
	}
	runes := []rune(input)
	var builder strings.Builder
	builder.Grow(len(input))

	inString := false
	escapeNext := false
	expectValue := false
	stringKind := qwenTTSJSONStringNone
	embeddedQuoteNextOpen := true

	nextNonSpace := func(start int) rune {
		for idx := start; idx < len(runes); idx++ {
			if !unicode.IsSpace(runes[idx]) {
				return runes[idx]
			}
		}
		return rune(0)
	}

	for idx, r := range runes {
		if inString {
			if escapeNext {
				builder.WriteRune(r)
				escapeNext = false
				continue
			}
			if r == '\\' {
				builder.WriteRune(r)
				escapeNext = true
				continue
			}
			if r == '"' {
				next := nextNonSpace(idx + 1)
				isClosing := false
				switch stringKind {
				case qwenTTSJSONStringKey:
					isClosing = next == ':'
				case qwenTTSJSONStringValue:
					isClosing = next == ',' || next == '}' || next == ']'
				default:
					isClosing = true
				}
				if isClosing {
					builder.WriteRune(r)
					inString = false
					stringKind = qwenTTSJSONStringNone
					if expectValue {
						expectValue = false
					}
					continue
				}
				if stringKind == qwenTTSJSONStringValue {
					if embeddedQuoteNextOpen {
						builder.WriteRune('“')
					} else {
						builder.WriteRune('”')
					}
					embeddedQuoteNextOpen = !embeddedQuoteNextOpen
					continue
				}
			}
			builder.WriteRune(r)
			continue
		}

		switch r {
		case '"':
			inString = true
			if expectValue {
				stringKind = qwenTTSJSONStringValue
				embeddedQuoteNextOpen = true
			} else {
				stringKind = qwenTTSJSONStringKey
			}
			builder.WriteRune(r)
		case ':':
			expectValue = true
			builder.WriteRune(r)
		case ',':
			expectValue = false
			builder.WriteRune(r)
		default:
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

func buildQwenTTSAutoParsePrompts(sourceText string) (string, string) {
	systemPrompt := strings.TrimSpace(`
你是一个专门把小说原文、剧本文本、对白片段，严格提取为语音生产格式的助手。

你的任务是：
把用户输入的原文，按出现顺序提取成一组条目，每个条目都必须是：
- character_name
- text
- instruct

严格规则：
1. 只能返回 JSON，对象顶层只能包含：
   - total
   - items
2. total 必须是最终会返回的 items 精确总条数，是一个整数；你必须先在内部完成完整拆分，再输出 total，并保证 total 与最终 items 实际条数完全一致。
3. items 必须是数组，每个元素必须包含：
   - character_name
   - text
   - instruct
4. 如果原文里是明确角色说的话，就填写对应角色名。
5. 如果原文里是叙述、动作描写、心理描写、环境描写、旁白说明，没有明确说话人，就统一使用：旁白
6. 严禁改写、润色、缩短、扩写、总结、合并、删减原文。
7. 允许修正极明显、无需上下文争议的错别字、同音误字，以及明显错误、缺失或混乱的标点符号，但只允许做最小修正。
8. 标点修正的目标是让 Qwen3 TTS 更容易正确理解停顿、语气和断句；优先修正中文逗号、句号、顿号、问号、叹号、冒号、分号、引号，以及明显错误的重复标点、缺失句末标点、半角/全角混乱等问题。
9. 严禁因为修正错字或标点而改变句意、删字、漏字、截断句子、合并句子或改写风格。
10. 必须保留原文里的关键措辞、语气词、停顿和完整语义；标点可以做最小必要修正，但文本内容本身不能被缩短或重写。
11. 只允许做“角色归类”“逐条拆分”和“极明显错字/标点的最小修正”；除此之外不允许改动文本本身。
12. 如果一段文本本身就是旁白或叙述，必须原样放进 text，不要改写成更口语化。
13. 如果一段文本本身就是角色台词，必须原样放进 text，不要解释，不要加前后缀。
14. 严禁漏掉任何一句台词、任何一句旁白、任何一句描写；严禁把多句合并成一句；严禁把一句截断成半句。
15. instruct 必须是适合 Qwen3 TTS 的单行语气提示，描述这句该怎么说，包含语气、情绪、节奏、力度、状态，但不要复述台词内容。
16. instruct 必须更像 Qwen3 TTS 可直接使用的自然语言控制，而不是给人看的分析说明。优先使用“语气+情绪+节奏+力度+声音状态”这种结构，例如：
   - 语气真诚克制，带一点压抑的悲伤，语速偏慢。
   - 情绪激动，明显愤怒，声音发紧，节奏加快。
   - 温柔、耐心、放松，像在认真安慰晚辈。
17. instruct 必须结合上下文判断当下说话状态，充分利用前后文，不要机械套同一模板。
18. instruct 必须保持单行，不要换行，不要加引号说明，不要写“这句应该……”之类元话术。
19. text 和 instruct 的 JSON 字符串内部，严禁出现未转义的半角双引号 "。如果原文里有拟声词、引用、强调词等需要引号包裹，必须改用中文引号「」或“”，绝不能把半角双引号直接写进 JSON 字符串里。
20. 特别注意像 “哗啦”“咔嚓”“砰” 这类词，如果要保留引号，也只能用中文引号，绝不能输出成 "哗啦"、"咔嚓" 这种会破坏 JSON 的形式。
21. 输出前先自行检查 JSON 合法、字符串闭合正确、没有多余字符、没有 Markdown、没有代码块，并再次确认 total 与 items 实际条数完全一致。
22. 只返回 JSON，不要输出解释。
`)

	userPrompt := strings.TrimSpace(fmt.Sprintf(`
请把下面这段原文严格提取成语音脚本条目。

重要要求：
- 最终会被拼成这种格式：{角色名}台词内容
- 你现在只需要返回 total 和 items 的 JSON
- 必须完整保留所有应有的台词和旁白
- 严禁漏字、严禁缩短、严禁改写、严禁总结
- 只允许修正极明显的错别字，以及明显错误、缺失或混乱的标点符号，但绝不允许因为修正错字或标点而删掉任何内容
- 标点修正要优先服务于 Qwen3 TTS 的断句和停顿表达，例如中文逗号、句号、问号、叹号、冒号、引号等可以做最小必要修正
- 如果角色不明确但内容显然是叙述或描写，统一标成“旁白”
- 每条 items 还要同时返回一条适合 Qwen3 TTS 的 instruct
- instruct 要根据整段剧情上下文推断这句真正的语气、情绪和说话状态
- instruct 要写成 Qwen3 TTS 可直接使用的单行提示词，重点写语气、情绪、节奏、力度、声音状态
- instruct 不要复述原句，不要解释剧情，不要写给人看的说明
- text 和 instruct 里的引号必须保证 JSON 合法；严禁在 JSON 字符串里直接出现未转义的半角双引号 "，如果需要保留引号，只能改用中文引号「」或“”

原文如下：
%s
`, strings.TrimSpace(sourceText)))

	return systemPrompt, userPrompt
}

func buildQwenTTSAutoParseContinuationPrompts(sourceText string, partialItems []qwenTTSAutoParseItem, knownTotal int) (string, string, error) {
	partialJSON, err := json.Marshal(partialItems)
	if err != nil {
		return "", "", fmt.Errorf("序列化已返回条目失败: %w", err)
	}
	remainingCount := 0
	if knownTotal > len(partialItems) {
		remainingCount = knownTotal - len(partialItems)
	}
	lastItemHint := "无"
	if len(partialItems) > 0 {
		lastItem := partialItems[len(partialItems)-1]
		lastItemHint = fmt.Sprintf("%s：%s", strings.TrimSpace(lastItem.CharacterName), strings.TrimSpace(lastItem.Text))
	}
	systemPrompt := strings.TrimSpace(`
你是一个专门把小说原文、剧本文本、对白片段，严格提取为语音生产格式的助手。

你正在续跑一个因为 LLM 断流而中途中断的返回。系统已经把前半段完整 items 成功保存到了数据库，现在你只能继续返回“剩余 items”，绝对不要重复前面已经返回过的内容。

硬规则：
1. 只能返回 JSON，对象顶层只能包含：
   - total
   - items
2. total 在这次续跑里，必须是“剩余还没返回的条目数”，不是整段原文最终总条数。
3. items 只能包含“剩余还没返回的条目”，并且必须紧接着已返回条目的后续顺序继续返回。
4. 严禁重复已返回 items，严禁回滚重写前面已经返回过的内容。
5. 严禁漏字、严禁缩短、严禁改写、严禁总结、严禁截断、严禁合并。
6. 允许修正极明显、无需上下文争议的错别字、同音误字，以及明显错误、缺失或混乱的标点符号，但只允许做最小修正。
7. 标点修正的目标是让 Qwen3 TTS 更容易正确理解停顿、语气和断句；优先修正中文逗号、句号、顿号、问号、叹号、冒号、分号、引号，以及明显错误的重复标点、缺失句末标点、半角/全角混乱等问题。
8. 严禁因为修正错字或标点而改变句意、删字、漏字、截断句子、合并句子或改写风格。
9. 如果原文里是明确角色说的话，就填写对应角色名。
10. 如果原文里是叙述、动作描写、心理描写、环境描写、旁白说明，没有明确说话人，就统一使用：旁白。
11. instruct 必须是适合 Qwen3 TTS 的单行语气提示，描述这句该怎么说，包含语气、情绪、节奏、力度、状态，但不要复述台词内容。
12. instruct 必须结合上下文判断当下说话状态，充分利用前后文，不要机械套同一模板。
13. text 和 instruct 的 JSON 字符串内部，严禁出现未转义的半角双引号 "。如果原文里有拟声词、引用、强调词等需要引号包裹，必须改用中文引号「」或“”，绝不能把半角双引号直接写进 JSON 字符串里。
14. 特别注意像 “哗啦”“咔嚓”“砰” 这类词，如果要保留引号，也只能用中文引号，绝不能输出成 "哗啦"、"咔嚓" 这种会破坏 JSON 的形式。
15. 输出前先自行检查 JSON 合法、字符串闭合正确、没有多余字符、没有 Markdown、没有代码块，并再次确认 total 等于这次剩余应返条数，且 total 与 items 实际条数完全一致。
16. 只返回 JSON，不要输出解释。
`)

	totalHint := "未知"
	if knownTotal > 0 {
		totalHint = fmt.Sprintf("%d", knownTotal)
	}

	userPrompt := strings.TrimSpace(fmt.Sprintf(`
请继续返回下面原文里“还没返回的剩余条目”。

重要要求：
- 原文必须按顺序完整拆分
- 下面给你的 JSON 数组，是“已经成功返回过的条目”
- 你这次只能返回剩余 items，不能重复它们
- 你本次应该只返回剩余的 %d 条；如果多一条、少一条、或从更早位置重写，都算失败
- 这次因为上一轮 LLM 断流，只剩下 %d 条没有返回；你这次必须只返回这 %d 条
- total 必须写这次剩余应返条数，也就是 %d，不是整段原文最终总条数
- 如果你判断已返回条目已经覆盖全部内容，也必须返回合法 JSON，此时 items 可以为空数组，但 total 必须是 0
- 严禁漏掉任何后续内容
- 严禁删字、缩短、改写、总结、截断
- 只允许修正极明显错别字，以及明显错误、缺失或混乱的标点符号；标点修正要优先服务于 Qwen3 TTS 的断句和停顿表达
- 绝不允许因修正错字或标点而删掉内容、改变句意或改写风格
- text 和 instruct 的 JSON 字符串内部，严禁出现未转义的半角双引号 "；如果需要保留引号，只能改用中文引号「」或“”
- 你必须从“最后一条已成功条目”的后面继续，不允许重复最后一条，也不允许回滚到它前面
- 如果你返回超过 %d 条、或者返回任何已经成功过的条目，系统会直接判失败

已知最终 total（如果前一轮已经给出）：
%s

最后一条已成功条目（你必须从它之后继续）：
%s

已经成功返回过的 items：
%s

原文如下：
%s
	`, remainingCount, remainingCount, remainingCount, remainingCount, remainingCount, totalHint, lastItemHint, string(partialJSON), strings.TrimSpace(sourceText)))

	return systemPrompt, userPrompt, nil
}

func requestQwenTTSAutoParseContent(provider models.LLMProvider, projectID uint, sourceText string, systemPrompt string, userPrompt string, taskID string, timeout time.Duration, progress int, progressMessage string, streamLogLabel string) (string, error) {
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
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
	if taskID != "" {
		task.GlobalTaskManager.UpdateTaskProgress(taskID, progress, progressMessage)
	}
	if strings.EqualFold(strings.TrimSpace(provider.Provider), "Direct") {
		return requestQwenTTSAutoParseContentDirect(provider, projectID, sourceText, req, timeout, taskID, streamLogLabel)
	}
	return requestQwenTTSAutoParseContentOpenAI(provider, projectID, sourceText, req, timeout, taskID, streamLogLabel)
}

func updateQwenTTSAutoParsePartialProgress(projectID uint, sourceText string, taskID string, currentContent string, lastSavedCount *int, lastSavedTotal *int) error {
	scan, err := scanQwenTTSAutoParsePartial(currentContent)
	if err != nil {
		return nil
	}
	partial := scan.Response
	if partial != nil {
		currentCount := len(partial.Items)
		currentTotal := partial.Total
		if currentCount > *lastSavedCount || (*lastSavedTotal == 0 && currentTotal > 0) || currentTotal > *lastSavedTotal {
			persistQwenTTSAutoParseState(projectID, sourceText, taskID, "", partial, false)
			*lastSavedCount = currentCount
			if currentTotal > 0 {
				*lastSavedTotal = currentTotal
			}
			progress := 18
			if currentTotal > 0 {
				progress = 18 + int(float64(currentCount)/float64(currentTotal)*42)
			}
			if progress > 60 {
				progress = 60
			}
			task.GlobalTaskManager.UpdateTaskProgress(taskID, progress, fmt.Sprintf("已实时解析 %d / %d 条", currentCount, currentTotal))
		}
	}
	if scan.Broken {
		if partial != nil {
			Log(LogLevelWarn, "Qwen3 TTS 自动解析流式截断", fmt.Sprintf("检测到第 %d 条起结构损坏，已实时保留连续正确前缀 %d 条", scan.FirstBrokenIndex, len(partial.Items)))
		}
		return &qwenTTSAutoParseStreamValidationError{BrokenIndex: scan.FirstBrokenIndex}
	}
	return nil
}

func requestQwenTTSAutoParseContentOpenAI(provider models.LLMProvider, projectID uint, sourceText string, req openai.ChatCompletionRequest, timeout time.Duration, taskID string, streamLogLabel string) (string, error) {
	messageParts := make([]string, 0, len(req.Messages))
	for _, message := range req.Messages {
		messageParts = append(messageParts, message.Content)
	}
	RecordLLMUsageInput(provider, messageParts...)
	idleController := newLLMStreamIdleController(timeout)
	defer idleController.Stop()
	stream, err := buildLLMOpenAIClient(provider, timeout, true).CreateChatCompletionStream(idleController.Context(), req)
	if err != nil {
		return "", idleController.WrapError(err)
	}
	defer stream.Close()

	var builder strings.Builder
	var streamStateID uint
	if streamLogLabel != "" {
		streamStateID = upsertLLMStreamState(0, taskID, provider, streamLogLabel, "", "running")
	}
	lastSavedCount := 0
	lastSavedTotal := 0
	for {
		response, recvErr := stream.Recv()
		if recvErr != nil {
			if recvErr.Error() == "EOF" || strings.Contains(strings.ToLower(recvErr.Error()), "eof") {
				break
			}
			content := strings.TrimSpace(builder.String())
			if streamLogLabel != "" {
				finalizeLLMStreamState(streamStateID, taskID, provider, streamLogLabel, content, "failed")
			}
			return content, idleController.WrapError(recvErr)
		}
		if len(response.Choices) == 0 {
			continue
		}
		idleController.Touch()
		builder.WriteString(response.Choices[0].Delta.Content)
		currentContent := strings.TrimSpace(builder.String())
		if streamLogLabel != "" {
			streamStateID = upsertLLMStreamState(streamStateID, taskID, provider, streamLogLabel, currentContent, "running")
		}
		if validationErr := updateQwenTTSAutoParsePartialProgress(projectID, sourceText, taskID, currentContent, &lastSavedCount, &lastSavedTotal); validationErr != nil {
			if streamLogLabel != "" {
				finalizeLLMStreamState(streamStateID, taskID, provider, streamLogLabel, currentContent, "failed")
			}
			return currentContent, validationErr
		}
	}
	content := strings.TrimSpace(builder.String())
	if streamLogLabel != "" && content != "" {
		finalizeLLMStreamState(streamStateID, taskID, provider, streamLogLabel, content, "completed")
	}
	RecordLLMUsageOutput(provider, content)
	return content, nil
}

func requestQwenTTSAutoParseContentDirect(provider models.LLMProvider, projectID uint, sourceText string, req openai.ChatCompletionRequest, timeout time.Duration, taskID string, streamLogLabel string) (string, error) {
	streamReq := req
	streamReq.Stream = true
	idleController := newLLMStreamIdleController(timeout)
	defer idleController.Stop()

	messageParts := make([]string, 0, len(streamReq.Messages))
	for _, message := range streamReq.Messages {
		messageParts = append(messageParts, message.Content)
	}
	RecordLLMUsageInput(provider, messageParts...)

	httpReq, err := newDirectLLMRequest(idleController.Context(), provider, streamReq)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := buildLLMStreamingHTTPClient().Do(httpReq)
	if err != nil {
		return "", idleController.WrapError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return "", readErr
		}
		return "", fmt.Errorf("API 请求失败: http %d: %s", resp.StatusCode, trimHTTPErrorBody(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	var builder strings.Builder
	var rawBody strings.Builder
	var streamStateID uint
	sawStreamChunk := false
	lastSavedCount := 0
	lastSavedTotal := 0
	if streamLogLabel != "" {
		streamStateID = upsertLLMStreamState(0, taskID, provider, streamLogLabel, "", "running")
	}

	for scanner.Scan() {
		idleController.Touch()
		line := scanner.Text()
		rawBody.WriteString(line)
		rawBody.WriteString("\n")
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk directLLMChatCompletionResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			content := strings.TrimSpace(builder.String())
			if streamLogLabel != "" {
				finalizeLLMStreamState(streamStateID, taskID, provider, streamLogLabel, content, "failed")
			}
			return content, fmt.Errorf("failed to parse direct stream chunk: %w", err)
		}
		delta := pickDirectLLMContent(chunk)
		if delta == "" {
			continue
		}
		sawStreamChunk = true
		builder.WriteString(delta)
		currentContent := strings.TrimSpace(builder.String())
		if streamLogLabel != "" {
			streamStateID = upsertLLMStreamState(streamStateID, taskID, provider, streamLogLabel, currentContent, "running")
		}
		if validationErr := updateQwenTTSAutoParsePartialProgress(projectID, sourceText, taskID, currentContent, &lastSavedCount, &lastSavedTotal); validationErr != nil {
			if streamLogLabel != "" {
				finalizeLLMStreamState(streamStateID, taskID, provider, streamLogLabel, currentContent, "failed")
			}
			return currentContent, validationErr
		}
	}

	if err := scanner.Err(); err != nil {
		content := strings.TrimSpace(builder.String())
		if streamLogLabel != "" {
			finalizeLLMStreamState(streamStateID, taskID, provider, streamLogLabel, content, "failed")
		}
		return content, idleController.WrapError(err)
	}

	content := strings.TrimSpace(builder.String())
	if !sawStreamChunk {
		content, err = parseDirectLLMResponseBody([]byte(rawBody.String()))
		if err != nil {
			return content, err
		}
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return requestLLMContentNonStreamingDirect(provider, req, timeout, taskID, streamLogLabel)
	}
	if streamLogLabel != "" {
		finalizeLLMStreamState(streamStateID, taskID, provider, streamLogLabel, content, "completed")
	}
	RecordLLMUsageOutput(provider, content)
	return content, nil
}

func parseQwenTTSAutoParseResponse(raw string) (*qwenTTSAutoParseResponse, error) {
	candidates := []string{
		strings.TrimSpace(raw),
		stripQwenTTSMarkdownFence(raw),
		extractQwenTTSJSONObjectCandidate(stripQwenTTSMarkdownFence(raw)),
		cleanupQwenTTSInferJSON(raw),
	}
	var (
		resp    qwenTTSAutoParseResponse
		lastErr error
	)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		resp = qwenTTSAutoParseResponse{}
		if err := json.Unmarshal([]byte(candidate), &resp); err == nil {
			lastErr = nil
			break
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("LLM 返回 JSON 解析失败: %v", lastErr)
	}
	if len(resp.Items) == 0 {
		return nil, fmt.Errorf("LLM 没有返回任何可解析条目")
	}
	if resp.Total <= 0 {
		return nil, fmt.Errorf("LLM 没有返回合法 total")
	}
	if resp.Total != len(resp.Items) {
		return nil, fmt.Errorf("LLM 返回的 total=%d 与 items 数量=%d 不一致", resp.Total, len(resp.Items))
	}
	return &resp, nil
}

func parseQwenTTSImportedResponse(raw string) (*qwenTTSAutoParseResponse, error) {
	candidates := []string{
		strings.TrimSpace(raw),
		stripQwenTTSMarkdownFence(raw),
		extractQwenTTSJSONObjectCandidate(stripQwenTTSMarkdownFence(raw)),
		cleanupQwenTTSInferJSON(raw),
	}
	var (
		resp    qwenTTSAutoParseResponse
		lastErr error
	)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		resp = qwenTTSAutoParseResponse{}
		if err := json.Unmarshal([]byte(candidate), &resp); err == nil {
			lastErr = nil
			break
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("导入 JSON 解析失败: %v", lastErr)
	}
	if len(resp.Items) == 0 {
		return nil, fmt.Errorf("导入结果里没有任何 items")
	}
	normalized := make([]qwenTTSAutoParseItem, 0, len(resp.Items))
	for idx, item := range resp.Items {
		item.CharacterName = strings.TrimSpace(item.CharacterName)
		item.Text = strings.TrimSpace(item.Text)
		item.Instruct = strings.TrimSpace(item.Instruct)
		if item.CharacterName == "" {
			item.CharacterName = "旁白"
		}
		if item.Text == "" {
			return nil, fmt.Errorf("第 %d 条缺少 text", idx+1)
		}
		normalized = append(normalized, item)
	}
	if resp.Total > 0 && resp.Total != len(normalized) {
		Log(LogLevelWarn, "Qwen3 TTS 导入 total 不一致", fmt.Sprintf("import total=%d, items=%d; 已按 items 实际数量导入", resp.Total, len(normalized)))
	}
	resp.Items = normalized
	resp.Total = len(normalized)
	return &resp, nil
}

func parseQwenTTSAutoParseContinuationResponse(raw string, remainingExpected int) (*qwenTTSAutoParseContinuationResponse, error) {
	candidates := []string{
		strings.TrimSpace(raw),
		stripQwenTTSMarkdownFence(raw),
		extractQwenTTSJSONObjectCandidate(stripQwenTTSMarkdownFence(raw)),
		cleanupQwenTTSInferJSON(raw),
	}
	var (
		resp    qwenTTSAutoParseContinuationResponse
		lastErr error
	)
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		resp = qwenTTSAutoParseContinuationResponse{}
		if err := json.Unmarshal([]byte(candidate), &resp); err == nil {
			lastErr = nil
			break
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("LLM 续跑返回 JSON 解析失败: %v", lastErr)
	}
	if resp.Total < 0 {
		return nil, fmt.Errorf("LLM 续跑没有返回合法 total")
	}
	if remainingExpected < 0 {
		remainingExpected = 0
	}
	if resp.Total != remainingExpected {
		return nil, fmt.Errorf("LLM 续跑返回的 total=%d 与剩余应返条数=%d 不一致", resp.Total, remainingExpected)
	}
	if len(resp.Items) > resp.Total {
		return nil, fmt.Errorf("LLM 续跑返回的 items 数量=%d 超过 total=%d", len(resp.Items), resp.Total)
	}
	return &resp, nil
}

func getTaskLatestLLMStreamContent(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	var stream models.LLMStreamState
	if err := db.DB.Where("task_id = ?", taskID).Order("updated_at desc").First(&stream).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(stream.Content)
}

func getTaskLatestLLMStream(taskID string) *models.LLMStreamState {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	var stream models.LLMStreamState
	if err := db.DB.Where("task_id = ?", taskID).Order("updated_at desc").First(&stream).Error; err != nil {
		return nil
	}
	return &stream
}

func extractQwenTTSTotalFromPartial(content string) int {
	match := qwenTTSTotalFieldPattern.FindStringSubmatch(content)
	if len(match) != 2 {
		return 0
	}
	var total int
	fmt.Sscanf(match[1], "%d", &total)
	return total
}

func extractCompleteJSONObjectArrayItems(content string, fieldName string) []string {
	content = cleanupQwenTTSInferPartial(content)
	if content == "" {
		return nil
	}
	fieldIndex := strings.Index(content, fmt.Sprintf(`"%s"`, fieldName))
	if fieldIndex < 0 {
		return nil
	}
	arrayRelative := strings.Index(content[fieldIndex:], "[")
	if arrayRelative < 0 {
		return nil
	}
	arrayStart := fieldIndex + arrayRelative
	objects := make([]string, 0)
	inString := false
	escapeNext := false
	depth := 0
	objStart := -1
	for i := arrayStart + 1; i < len(content); i++ {
		ch := content[i]
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
			if depth == 0 {
				objStart = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && objStart >= 0 {
				objects = append(objects, content[objStart:i+1])
				objStart = -1
			}
		case ']':
			if depth == 0 {
				return objects
			}
		}
	}
	return objects
}

func scanQwenTTSAutoParsePartial(raw string) (*qwenTTSAutoParsePartialScan, error) {
	content := cleanupQwenTTSInferPartial(raw)
	if content == "" {
		return nil, fmt.Errorf("没有可用于续跑的 partial 内容")
	}
	total := extractQwenTTSTotalFromPartial(content)
	objectStrings := extractCompleteJSONObjectArrayItems(content, "items")
	items := make([]qwenTTSAutoParseItem, 0, len(objectStrings))
	firstBrokenIndex := -1
	for _, objectString := range objectStrings {
		var item qwenTTSAutoParseItem
		if err := json.Unmarshal([]byte(objectString), &item); err != nil {
			firstBrokenIndex = len(items)
			break
		}
		item.CharacterName = strings.TrimSpace(item.CharacterName)
		item.Text = strings.TrimSpace(item.Text)
		item.Instruct = strings.TrimSpace(item.Instruct)
		if item.CharacterName == "" || item.Text == "" {
			firstBrokenIndex = len(items)
			break
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("没有从 partial 内容里提取到完整条目")
	}
	return &qwenTTSAutoParsePartialScan{
		Response: &qwenTTSAutoParseResponse{
			Total: total,
			Items: items,
		},
		Broken:           firstBrokenIndex >= 0,
		FirstBrokenIndex: firstBrokenIndex + 1,
	}, nil
}

func extractQwenTTSAutoParsePartial(raw string) (*qwenTTSAutoParseResponse, error) {
	scan, err := scanQwenTTSAutoParsePartial(raw)
	if err != nil {
		return nil, err
	}
	if scan.Broken {
		Log(LogLevelWarn, "Qwen3 TTS 自动解析 partial 截断", fmt.Sprintf("检测到第 %d 条起结构损坏，只保留连续正确前缀 %d 条", scan.FirstBrokenIndex, len(scan.Response.Items)))
	}
	return scan.Response, nil
}

func findLatestQwenTTSAutoParseResumeInfo(projectID uint) (*qwenTTSAutoParseResumeInfo, error) {
	if projectID == 0 {
		return nil, nil
	}
	var fallback *qwenTTSAutoParseResumeInfo
	var project models.QwenTTSProject
	if err := db.DB.Select("id", "auto_parse_source_text", "auto_parse_partial_json", "auto_parse_returned_count", "auto_parse_total", "last_auto_parse_task_id", "last_auto_parse_error").First(&project, projectID).Error; err == nil {
		projectPartial := parseQwenTTSAutoParsePartialJSON(project.AutoParsePartialJSON)
		lastTaskID := strings.TrimSpace(project.LastAutoParseTaskID)
		if lastTaskID != "" {
			var taskRecord models.Task
			if err := db.DB.Where("id = ? AND type = ?", lastTaskID, "auto_parse_qwen_tts_script").First(&taskRecord).Error; err == nil {
				maybeMarkQwenTTSAutoParseTaskStale(&taskRecord)
				stream := getTaskLatestLLMStream(lastTaskID)
				info := buildQwenTTSAutoParseResumeInfo(&taskRecord, stream, project.AutoParseSourceText, project.LastAutoParseError)
				info = applyProjectPartialToQwenTTSAutoParseResumeInfo(info, projectPartial)
				if info.Resumable {
					return info, nil
				}
				if info.Stream != nil || info.SourceText != "" {
					fallback = info
				}
			} else if stream := getTaskLatestLLMStream(lastTaskID); stream != nil || strings.TrimSpace(project.AutoParseSourceText) != "" {
				info := buildQwenTTSAutoParseResumeInfo(nil, stream, project.AutoParseSourceText, project.LastAutoParseError)
				info = applyProjectPartialToQwenTTSAutoParseResumeInfo(info, projectPartial)
				if info != nil && info.Resumable {
					return info, nil
				}
				if info != nil && fallback == nil && (info.Stream != nil || info.SourceText != "") {
					fallback = info
				}
			}
		}
		if projectPartial != nil {
			info := &qwenTTSAutoParseResumeInfo{
				Task: &models.Task{
					ID:        strings.TrimSpace(project.LastAutoParseTaskID),
					Type:      "auto_parse_qwen_tts_script",
					Status:    "failed",
					Error:     strings.TrimSpace(project.LastAutoParseError),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
				Resumable:         true,
				BaseReturnedCount: 0,
				ReturnedCount:     len(projectPartial.Items),
				Total:             projectPartial.Total,
				SourceText:        strings.TrimSpace(project.AutoParseSourceText),
				PartialJSON:       strings.TrimSpace(project.AutoParsePartialJSON),
			}
			return info, nil
		}
		if strings.TrimSpace(project.AutoParseSourceText) != "" && fallback == nil {
			fallback = &qwenTTSAutoParseResumeInfo{
				SourceText: strings.TrimSpace(project.AutoParseSourceText),
				Task: &models.Task{
					ID:        strings.TrimSpace(project.LastAutoParseTaskID),
					Type:      "auto_parse_qwen_tts_script",
					Status:    "failed",
					Error:     strings.TrimSpace(project.LastAutoParseError),
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
			}
		}
	}
	var tasks []models.Task
	if err := db.DB.Where("type = ?", "auto_parse_qwen_tts_script").Order("created_at desc").Limit(100).Find(&tasks).Error; err != nil {
		return nil, err
	}
	for _, taskRecord := range tasks {
		var payload qwenTTSAutoParseScriptTaskPayload
		if err := json.Unmarshal([]byte(taskRecord.Payload), &payload); err != nil {
			continue
		}
		if payload.ProjectID != projectID {
			continue
		}
		maybeMarkQwenTTSAutoParseTaskStale(&taskRecord)
		stream := getTaskLatestLLMStream(taskRecord.ID)
		info := buildQwenTTSAutoParseResumeInfo(&taskRecord, stream, payload.SourceText, "")
		info = applyProjectPartialToQwenTTSAutoParseResumeInfo(info, parseQwenTTSAutoParsePartialJSON(project.AutoParsePartialJSON))
		if info.Resumable {
			return info, nil
		}
		if fallback == nil && info != nil {
			fallback = info
		}
	}
	return fallback, nil
}

func qwenTTSAutoParseItemEqual(a qwenTTSAutoParseItem, b qwenTTSAutoParseItem) bool {
	return strings.TrimSpace(a.CharacterName) == strings.TrimSpace(b.CharacterName) &&
		strings.TrimSpace(a.Text) == strings.TrimSpace(b.Text) &&
		strings.TrimSpace(a.Instruct) == strings.TrimSpace(b.Instruct)
}

func qwenTTSAutoParseItemContentEqual(a qwenTTSAutoParseItem, b qwenTTSAutoParseItem) bool {
	return strings.TrimSpace(a.CharacterName) == strings.TrimSpace(b.CharacterName) &&
		strings.TrimSpace(a.Text) == strings.TrimSpace(b.Text)
}

func qwenTTSAutoParseHasPrefix(items []qwenTTSAutoParseItem, prefix []qwenTTSAutoParseItem) bool {
	if len(prefix) > len(items) {
		return false
	}
	for idx := range prefix {
		if !qwenTTSAutoParseItemContentEqual(items[idx], prefix[idx]) {
			return false
		}
	}
	return true
}

func mergeQwenTTSAutoParseItems(existing []qwenTTSAutoParseItem, next []qwenTTSAutoParseItem) []qwenTTSAutoParseItem {
	if len(existing) == 0 {
		return append([]qwenTTSAutoParseItem(nil), next...)
	}
	if len(next) == 0 {
		return append([]qwenTTSAutoParseItem(nil), existing...)
	}
	if qwenTTSAutoParseHasPrefix(next, existing) {
		return append([]qwenTTSAutoParseItem(nil), next...)
	}
	maxOverlap := 0
	maxCheck := len(existing)
	if len(next) < maxCheck {
		maxCheck = len(next)
	}
	for overlap := maxCheck; overlap > 0; overlap-- {
		match := true
		for idx := 0; idx < overlap; idx++ {
			if !qwenTTSAutoParseItemContentEqual(existing[len(existing)-overlap+idx], next[idx]) {
				match = false
				break
			}
		}
		if match {
			maxOverlap = overlap
			break
		}
	}
	merged := append([]qwenTTSAutoParseItem(nil), existing...)
	merged = append(merged, next[maxOverlap:]...)
	return merged
}

func trimQwenTTSAutoParseItemsToTotal(items []qwenTTSAutoParseItem, total int) []qwenTTSAutoParseItem {
	if total <= 0 || len(items) <= total {
		return items
	}
	trimmed := append([]qwenTTSAutoParseItem(nil), items...)
	for len(trimmed) > total {
		removed := false
		for idx := 1; idx < len(trimmed); idx++ {
			if qwenTTSAutoParseItemContentEqual(trimmed[idx-1], trimmed[idx]) {
				trimmed = append(trimmed[:idx], trimmed[idx+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			return items
		}
	}
	return trimmed
}

func buildQwenTTSAutoParseFailurePartial(project models.QwenTTSProject, taskID string, preserveExisting bool) *qwenTTSAutoParseResponse {
	latestProject := project
	if project.ID != 0 {
		var refreshed models.QwenTTSProject
		if err := db.DB.Select(
			"id",
			"auto_parse_partial_json",
			"auto_parse_returned_count",
			"auto_parse_total",
			"last_auto_parse_task_id",
			"last_auto_parse_error",
		).First(&refreshed, project.ID).Error; err == nil {
			latestProject = refreshed
		}
	}

	var base *qwenTTSAutoParseResponse
	// 失败救场时，优先信任数据库里已经实时落下来的前缀，不再依赖任务开始时的旧 project 快照。
	if latestProject.ID != 0 {
		base = parseQwenTTSAutoParsePartialJSON(latestProject.AutoParsePartialJSON)
	}
	if base == nil && preserveExisting {
		base = parseQwenTTSAutoParsePartialJSON(project.AutoParsePartialJSON)
	}
	current, currentErr := extractQwenTTSAutoParsePartial(getTaskLatestLLMStreamContent(taskID))
	if base == nil {
		if currentErr != nil {
			return nil
		}
		return current
	}
	if currentErr != nil || current == nil || len(current.Items) == 0 {
		return base
	}
	knownTotal := base.Total
	if knownTotal <= 0 {
		knownTotal = current.Total
	}
	mergedItems := mergeQwenTTSAutoParseItems(base.Items, current.Items)
	if knownTotal > 0 && len(mergedItems) > knownTotal {
		trimmed := trimQwenTTSAutoParseItemsToTotal(mergedItems, knownTotal)
		if len(trimmed) == knownTotal {
			mergedItems = trimmed
		} else {
			return base
		}
	}
	return &qwenTTSAutoParseResponse{
		Total: knownTotal,
		Items: mergedItems,
	}
}

func continueQwenTTSAutoParseWithKnownItems(provider models.LLMProvider, projectID uint, sourceText string, partial *qwenTTSAutoParseResponse, taskID string, manual bool) (*qwenTTSAutoParseResponse, error) {
	if partial == nil || len(partial.Items) == 0 {
		return nil, fmt.Errorf("没有可续跑的已返回条目")
	}
	combined := &qwenTTSAutoParseResponse{
		Total: partial.Total,
		Items: append([]qwenTTSAutoParseItem(nil), partial.Items...),
	}
	if combined.Total > 0 && len(combined.Items) >= combined.Total {
		if len(combined.Items) > combined.Total {
			return nil, fmt.Errorf("已返回条目数=%d 超过 total=%d", len(combined.Items), combined.Total)
		}
		return combined, nil
	}

	attempts := 1
	labelPrefix := "Qwen3 TTS 自动解析脚本（手动续跑）"
	progressBase := 24
	if !manual {
		attempts = 2
		labelPrefix = "Qwen3 TTS 自动解析脚本（救场续跑）"
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		remainingCount := 0
		if combined.Total > len(combined.Items) {
			remainingCount = combined.Total - len(combined.Items)
		}
		task.GlobalTaskManager.UpdateTaskProgress(taskID, progressBase+(attempt*6), fmt.Sprintf("LLM 中断，正在续跑剩余内容（第 %d 次）", attempt))
		rescueSystem, rescueUser, rescuePromptErr := buildQwenTTSAutoParseContinuationPrompts(sourceText, combined.Items, combined.Total)
		if rescuePromptErr != nil {
			return nil, rescuePromptErr
		}
		rescueLabel := fmt.Sprintf("%s %d", labelPrefix, attempt)
		Log(LogLevelInfo, llmLogMessage("LLM Request", provider), fmt.Sprintf("Starting qwen-tts script auto parse continuation for project=%d attempt=%d partial_count=%d total=%d manual=%v", projectID, attempt, len(combined.Items), combined.Total, manual))
		Log(LogLevelInfo, llmLogMessage("LLM Request Prompt", provider), fmt.Sprintf("System:\n%s\n\nUser:\n%s", rescueSystem, rescueUser))
		rescueRaw, rescueErr := requestQwenTTSAutoParseContent(provider, projectID, sourceText, rescueSystem, rescueUser, taskID, 4*time.Minute, progressBase+(attempt*6), fmt.Sprintf("LLM 中断，正在续跑剩余内容（第 %d 次）", attempt), rescueLabel)
		if rescueErr != nil {
			Log(LogLevelWarn, llmLogMessage("LLM 续跑中断(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("第 %d 次续跑中断: %v", attempt, rescueErr))
		} else {
			Log(LogLevelInfo, llmLogMessage(fmt.Sprintf("LLM 完整返回(%s %d)", labelPrefix, attempt), provider), rescueRaw)
		}
		continuationResp, continuationParseErr := parseQwenTTSAutoParseContinuationResponse(rescueRaw, remainingCount)
		if continuationParseErr != nil {
			partialContinuation, partialContinuationErr := extractQwenTTSAutoParsePartial(rescueRaw)
			if partialContinuationErr != nil {
				Log(LogLevelWarn, llmLogMessage("LLM 续跑解析失败(Qwen3 TTS 自动解析脚本)", provider), continuationParseErr.Error())
				continue
			}
			continuationResp = &qwenTTSAutoParseContinuationResponse{
				Total: partialContinuation.Total,
				Items: partialContinuation.Items,
			}
		}
		if remainingCount >= 0 && len(continuationResp.Items) > remainingCount {
			return nil, fmt.Errorf("LLM 续跑返回条数=%d 超过剩余应返条数=%d", len(continuationResp.Items), remainingCount)
		}
		combined.Items = mergeQwenTTSAutoParseItems(combined.Items, continuationResp.Items)
		if combined.Total > 0 && len(combined.Items) >= combined.Total {
			if len(combined.Items) > combined.Total {
				trimmed := trimQwenTTSAutoParseItemsToTotal(combined.Items, combined.Total)
				if len(trimmed) == combined.Total {
					combined.Items = trimmed
					return combined, nil
				}
				return nil, fmt.Errorf("LLM 续跑后条目数=%d 超过 total=%d", len(combined.Items), combined.Total)
			}
			return combined, nil
		}
	}

	if manual {
		return nil, fmt.Errorf("续跑后仍未补齐剩余条目，当前已有 %d / %d 条", len(combined.Items), combined.Total)
	}
	return nil, fmt.Errorf("救场续跑后仍未补齐剩余条目，当前已有 %d / %d 条", len(combined.Items), combined.Total)
}

func continueAutoParseQwenTTSScriptFromProjectState(project models.QwenTTSProject, sourceText string, taskID string) (*qwenTTSAutoParseResponse, error) {
	var provider models.LLMProvider
	if err := db.DB.Where("is_active = ?", true).First(&provider).Error; err != nil {
		return nil, fmt.Errorf("未找到启用中的 LLM 提供商")
	}
	partialResp := parseQwenTTSAutoParsePartialJSON(project.AutoParsePartialJSON)
	if partialResp == nil || len(partialResp.Items) == 0 {
		return nil, fmt.Errorf("无法从数据库中取出已完成条目")
	}
	if partialResp.Total > 0 && len(partialResp.Items) >= partialResp.Total {
		return nil, fmt.Errorf("当前没有可继续解析的剩余条目")
	}
	Log(LogLevelInfo, llmLogMessage("LLM 手动续跑(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("project_id=%d partial_count=%d total=%d", project.ID, len(partialResp.Items), partialResp.Total))
	combined, err := continueQwenTTSAutoParseWithKnownItems(provider, project.ID, sourceText, partialResp, taskID, true)
	if err != nil {
		markTaskLLMStreamFailed(taskID, err.Error())
		Log(LogLevelError, llmLogMessage("LLM 手动续跑失败(Qwen3 TTS 自动解析脚本)", provider), err.Error())
		return nil, err
	}
	Log(LogLevelInfo, llmLogMessage("LLM 手动续跑成功(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("续跑完成，total=%d", combined.Total))
	return combined, nil
}

func autoParseQwenTTSScriptOnce(project models.QwenTTSProject, sourceText string, taskID string) (*qwenTTSAutoParseResponse, error) {
	var provider models.LLMProvider
	if err := db.DB.Where("is_active = ?", true).First(&provider).Error; err != nil {
		return nil, fmt.Errorf("未找到启用中的 LLM 提供商")
	}
	systemPrompt, userPrompt := buildQwenTTSAutoParsePrompts(sourceText)
	Log(LogLevelInfo, llmLogMessage("LLM Request", provider), fmt.Sprintf("Starting qwen-tts script auto parse for project=%d", project.ID))
	Log(LogLevelInfo, llmLogMessage("LLM Request Prompt", provider), fmt.Sprintf("System:\n%s\n\nUser:\n%s", systemPrompt, userPrompt))
	raw, err := requestQwenTTSAutoParseContent(provider, project.ID, sourceText, systemPrompt, userPrompt, taskID, 6*time.Minute, 18, "正在调用 LLM 自动解析原文", "Qwen3 TTS 自动解析脚本")
	if err != nil {
		Log(LogLevelWarn, llmLogMessage("LLM 中断(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("主请求中断，尝试续跑剩余条目: %v", err))
		partialSource := strings.TrimSpace(raw)
		if partialSource == "" {
			partialSource = getTaskLatestLLMStreamContent(taskID)
		}
		partialResp, partialErr := extractQwenTTSAutoParsePartial(partialSource)
		if partialErr != nil {
			Log(LogLevelError, llmLogMessage("LLM Error", provider), fmt.Sprintf("Qwen3 TTS script auto parse failed and partial parse failed: %v / %v", err, partialErr))
			return nil, err
		}
		combined, rescueErr := continueQwenTTSAutoParseWithKnownItems(provider, project.ID, sourceText, partialResp, taskID, false)
		if rescueErr == nil {
			Log(LogLevelInfo, llmLogMessage("LLM 救场成功(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("续跑完成，total=%d", combined.Total))
			return combined, nil
		}
		Log(LogLevelError, llmLogMessage("LLM Error", provider), fmt.Sprintf("Qwen3 TTS script auto parse failed after rescue attempts: %v", err))
		return nil, err
	}
	Log(LogLevelInfo, llmLogMessage("LLM 完整返回(Qwen3 TTS 自动解析脚本)", provider), raw)
	resp, parseErr := parseQwenTTSAutoParseResponse(raw)
	if parseErr != nil {
		partialSource := strings.TrimSpace(raw)
		if partialSource == "" {
			partialSource = getTaskLatestLLMStreamContent(taskID)
		}
		partialResp, partialErr := extractQwenTTSAutoParsePartial(partialSource)
		if partialErr == nil {
			Log(LogLevelWarn, llmLogMessage("LLM 返回解析失败(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("%s；已提取 partial_count=%d total=%d，尝试续跑剩余条目", parseErr.Error(), len(partialResp.Items), partialResp.Total))
			combined, rescueErr := continueQwenTTSAutoParseWithKnownItems(provider, project.ID, sourceText, partialResp, taskID, false)
			if rescueErr == nil {
				Log(LogLevelInfo, llmLogMessage("LLM 解析失败后救场成功(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("主请求解析失败后已续跑完成，total=%d", combined.Total))
				return combined, nil
			}
			markTaskLLMStreamFailed(taskID, rescueErr.Error())
			Log(LogLevelError, llmLogMessage("LLM 解析失败后救场失败(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("parseErr=%v / rescueErr=%v", parseErr, rescueErr))
			return nil, rescueErr
		}
		markTaskLLMStreamFailed(taskID, parseErr.Error())
		Log(LogLevelError, llmLogMessage("LLM 返回解析失败(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("%s；且无法提取 partial: %v", parseErr.Error(), partialErr))
		return nil, parseErr
	}
	return resp, nil
}

func continueAutoParseQwenTTSScriptFromTask(project models.QwenTTSProject, sourceText string, resumeFromTaskID string, taskID string) (*qwenTTSAutoParseResponse, error) {
	var provider models.LLMProvider
	if err := db.DB.Where("is_active = ?", true).First(&provider).Error; err != nil {
		return nil, fmt.Errorf("未找到启用中的 LLM 提供商")
	}
	projectPartial := parseQwenTTSAutoParsePartialJSON(project.AutoParsePartialJSON)
	partialSource := getTaskLatestLLMStreamContent(resumeFromTaskID)
	var (
		streamPartial *qwenTTSAutoParseResponse
		partialResp   *qwenTTSAutoParseResponse
		partialErr    error
	)
	if strings.TrimSpace(partialSource) != "" {
		streamPartial, partialErr = extractQwenTTSAutoParsePartial(partialSource)
	}
	partialResp = projectPartial
	if partialResp == nil && streamPartial != nil {
		partialResp = streamPartial
		partialErr = nil
	} else if partialResp != nil && streamPartial != nil {
		knownTotal := partialResp.Total
		if knownTotal <= 0 {
			knownTotal = streamPartial.Total
		}
		mergedItems := mergeQwenTTSAutoParseItems(partialResp.Items, streamPartial.Items)
		if len(mergedItems) > len(partialResp.Items) && (knownTotal <= 0 || len(mergedItems) <= knownTotal) {
			partialResp = &qwenTTSAutoParseResponse{
				Total: knownTotal,
				Items: mergedItems,
			}
			partialErr = nil
		}
	}
	if partialResp == nil && projectPartial != nil {
		partialResp = projectPartial
		partialErr = nil
	}
	if partialResp != nil && len(partialResp.Items) > 0 {
		persistQwenTTSAutoParseState(project.ID, sourceText, resumeFromTaskID, project.LastAutoParseError, partialResp, false)
		if partialResp.Total > 0 {
			Log(LogLevelInfo, llmLogMessage("LLM 手动续跑前缀恢复(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("resume_from_task_id=%s recovered_prefix=%d/%d", resumeFromTaskID, len(partialResp.Items), partialResp.Total))
		}
	}
	if partialErr != nil {
		return nil, fmt.Errorf("无法从上一次返回内容中提取已完成条目: %w", partialErr)
	}
	if partialResp == nil {
		return nil, fmt.Errorf("没有找到可用于继续解析的已完成条目")
	}
	Log(LogLevelInfo, llmLogMessage("LLM 手动续跑(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("resume_from_task_id=%s partial_count=%d total=%d", resumeFromTaskID, len(partialResp.Items), partialResp.Total))
	combined, err := continueQwenTTSAutoParseWithKnownItems(provider, project.ID, sourceText, partialResp, taskID, true)
	if err != nil {
		markTaskLLMStreamFailed(taskID, err.Error())
		Log(LogLevelError, llmLogMessage("LLM 手动续跑失败(Qwen3 TTS 自动解析脚本)", provider), err.Error())
		return nil, err
	}
	Log(LogLevelInfo, llmLogMessage("LLM 手动续跑成功(Qwen3 TTS 自动解析脚本)", provider), fmt.Sprintf("续跑完成，total=%d", combined.Total))
	return combined, nil
}

func buildQwenTTSScriptTextFromItems(items []qwenTTSAutoParseItem) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		characterName := strings.TrimSpace(item.CharacterName)
		text := strings.TrimSpace(item.Text)
		if characterName == "" || text == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("{%s}%s", characterName, text))
	}
	return strings.Join(lines, "\n")
}

func buildQwenTTSInferredInstructMap(items []qwenTTSAutoParseItem) map[string]string {
	result := make(map[string]string, len(items))
	for idx, item := range items {
		key := qwenTTSLinePreserveKey(idx+1, item.CharacterName, item.Text)
		result[key] = strings.TrimSpace(item.Instruct)
	}
	return result
}

func loadQwenTTSProjectOr404(c *gin.Context) (*models.QwenTTSProject, error) {
	projectID := strings.TrimSpace(c.Param("id"))
	var project models.QwenTTSProject
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Qwen3 TTS 项目不存在"})
		return nil, err
	}
	return &project, nil
}

func loadQwenTTSCharacterOr404(c *gin.Context) (*models.QwenTTSCharacter, error) {
	characterID := strings.TrimSpace(c.Param("characterId"))
	var character models.QwenTTSCharacter
	if err := db.DB.First(&character, characterID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Qwen3 TTS 角色声音资产不存在"})
		return nil, err
	}
	return &character, nil
}

func loadQwenTTSLineOr404(c *gin.Context) (*models.QwenTTSLine, error) {
	lineID := strings.TrimSpace(c.Param("lineId"))
	var line models.QwenTTSLine
	if err := db.DB.First(&line, lineID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Qwen3 TTS 台词行不存在"})
		return nil, err
	}
	return &line, nil
}

func qwenTTSLinePreserveKey(sortOrder int, characterName string, text string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", sortOrder, strings.TrimSpace(characterName), strings.TrimSpace(text))
}

func replaceQwenTTSLines(projectID uint, script string, defaultInstruct string, defaultTemperature float64, defaultSeed int64, inferredInstructByKey map[string]string) ([]models.QwenTTSLine, error) {
	parsed, err := parseAudioCloneScript(script)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	created := make([]models.QwenTTSLine, 0, len(parsed))
	oldGeneratedAudio := make([]string, 0)
	preservedGeneratedAudio := make(map[string]bool)
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		var oldLines []models.QwenTTSLine
		if err := tx.Where("project_id = ?", projectID).Find(&oldLines).Error; err != nil {
			return err
		}
		oldLineByKey := make(map[string]models.QwenTTSLine, len(oldLines))
		oldInstructByKey := make(map[string]string, len(oldLines))
		oldTemperatureByKey := make(map[string]float64, len(oldLines))
		oldSeedByKey := make(map[string]int64, len(oldLines))
		for _, line := range oldLines {
			if strings.TrimSpace(line.GeneratedAudio) != "" {
				oldGeneratedAudio = append(oldGeneratedAudio, line.GeneratedAudio)
			}
			key := qwenTTSLinePreserveKey(line.SortOrder, line.CharacterName, line.Text)
			oldLineByKey[key] = line
			if strings.TrimSpace(line.Instruct) != "" {
				oldInstructByKey[key] = line.Instruct
			}
			if line.Temperature > 0 {
				oldTemperatureByKey[key] = line.Temperature
			}
			if line.Seed > 0 {
				oldSeedByKey[key] = line.Seed
			}
		}
		if err := tx.Where("project_id = ?", projectID).Delete(&models.QwenTTSLine{}).Error; err != nil {
			return err
		}
		for _, item := range parsed {
			key := qwenTTSLinePreserveKey(item.SortOrder, item.CharacterName, item.Text)
			instruct := strings.TrimSpace(defaultInstruct)
			if existing := strings.TrimSpace(oldInstructByKey[key]); existing != "" {
				instruct = existing
			} else if inferred := strings.TrimSpace(inferredInstructByKey[key]); inferred != "" {
				instruct = inferred
			}
			temperature := normalizeQwenTTSTemperature(defaultTemperature)
			if existing, ok := oldTemperatureByKey[key]; ok && existing > 0 {
				temperature = normalizeQwenTTSTemperature(existing)
			}
			seed := normalizeQwenTTSSeed(defaultSeed)
			if existing, ok := oldSeedByKey[key]; ok && existing > 0 {
				seed = normalizeQwenTTSSeed(existing)
			}
			line := models.QwenTTSLine{
				ProjectID:     projectID,
				SortOrder:     item.SortOrder,
				CharacterName: item.CharacterName,
				Text:          item.Text,
				Instruct:      instruct,
				Temperature:   temperature,
				Seed:          seed,
				Status:        audioCloneLineStatusDraft,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if oldLine, ok := oldLineByKey[key]; ok &&
				oldLine.Status == audioCloneLineStatusGenerated &&
				strings.TrimSpace(oldLine.GeneratedAudio) != "" {
				line.Status = audioCloneLineStatusGenerated
				line.CurrentTaskID = ""
				line.LastError = ""
				line.GeneratedAudio = oldLine.GeneratedAudio
				line.GeneratedWorkflow = oldLine.GeneratedWorkflow
				preservedGeneratedAudio[oldLine.GeneratedAudio] = true
			}
			if err := tx.Create(&line).Error; err != nil {
				return err
			}
			created = append(created, line)
		}
		return nil
	})
	if err == nil {
		for _, path := range oldGeneratedAudio {
			if preservedGeneratedAudio[path] {
				continue
			}
			_ = removeAudioCloneAsset(path)
		}
	}
	return created, err
}

func validateQwenTTSLineAssets(projectID uint, parsed []audioCloneParsedLine) []audioCloneMissingCharacter {
	var characters []models.QwenTTSCharacter
	_ = db.DB.Where("project_id = ?", projectID).Find(&characters).Error
	charMap := make(map[string]models.QwenTTSCharacter, len(characters))
	for _, character := range characters {
		charMap[strings.TrimSpace(character.Name)] = character
	}
	missingMap := make(map[string]string)
	for _, line := range parsed {
		character, ok := charMap[line.CharacterName]
		switch {
		case !ok:
			missingMap[line.CharacterName] = "没有创建这个角色"
		case strings.TrimSpace(character.ReferenceAudio) == "":
			missingMap[line.CharacterName] = "缺少参考音频"
		case character.ReferenceTextStatus == audioCloneLineStatusGenerating && strings.TrimSpace(character.ReferenceText) == "":
			missingMap[line.CharacterName] = "参考音频内容正在识别，请稍后再生成"
		case strings.TrimSpace(character.ReferenceText) == "":
			missingMap[line.CharacterName] = "缺少参考音频内容，请先识别或手动填写"
		}
	}
	names := make([]string, 0, len(missingMap))
	for name := range missingMap {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]audioCloneMissingCharacter, 0, len(names))
	for _, name := range names {
		result = append(result, audioCloneMissingCharacter{Name: name, MissingReason: missingMap[name]})
	}
	return result
}

func ListQwenTTSProjects(c *gin.Context) {
	var projects []models.QwenTTSProject
	if err := db.DB.Order("created_at desc").Find(&projects).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取 Qwen3 TTS 项目失败"})
		return
	}
	c.JSON(http.StatusOK, projects)
}

func GetQwenTTSProject(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	c.JSON(http.StatusOK, project)
}

func CreateQwenTTSProject(c *gin.Context) {
	var req struct {
		Name        string  `json:"name"`
		Code        string  `json:"code"`
		Description string  `json:"description"`
		ScriptText  string  `json:"script_text"`
		Instruct    string  `json:"instruct"`
		Temperature float64 `json:"temperature"`
		XVectorOnly *bool   `json:"x_vector_only"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	name := strings.TrimSpace(req.Name)
	code := normalizeAudioCloneCode(req.Code)
	if name == "" || code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写项目名称和项目文件名"})
		return
	}
	if !validateAudioCloneProjectCode(code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名只允许英文、数字、下划线或连字符"})
		return
	}
	var count int64
	db.DB.Model(&models.QwenTTSProject{}).Where("code = ?", code).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
		return
	}
	if _, err := os.Stat(qwenTTSProjectDir(code)); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
		return
	}
	now := time.Now()
	project := models.QwenTTSProject{
		Name:        name,
		Code:        code,
		Description: strings.TrimSpace(req.Description),
		ScriptText:  strings.TrimSpace(req.ScriptText),
		Instruct:    strings.TrimSpace(req.Instruct),
		Temperature: normalizeQwenTTSTemperature(req.Temperature),
		XVectorOnly: false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if req.XVectorOnly != nil {
		project.XVectorOnly = *req.XVectorOnly
	}
	if err := os.MkdirAll(qwenTTSProjectDir(code), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建项目目录失败"})
		return
	}
	if err := db.DB.Create(&project).Error; err != nil {
		_ = os.RemoveAll(qwenTTSProjectDir(code))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建 Qwen3 TTS 项目失败"})
		return
	}
	c.JSON(http.StatusCreated, project)
}

func UpdateQwenTTSProject(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	var req struct {
		Name        string  `json:"name"`
		Code        string  `json:"code"`
		Description string  `json:"description"`
		ScriptText  string  `json:"script_text"`
		Instruct    string  `json:"instruct"`
		Temperature float64 `json:"temperature"`
		XVectorOnly *bool   `json:"x_vector_only"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	name := strings.TrimSpace(req.Name)
	code := normalizeAudioCloneCode(req.Code)
	if name == "" || code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写项目名称和项目文件名"})
		return
	}
	if !validateAudioCloneProjectCode(code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名只允许英文、数字、下划线或连字符"})
		return
	}
	if code != project.Code {
		var count int64
		db.DB.Model(&models.QwenTTSProject{}).Where("code = ? AND id <> ?", code, project.ID).Count(&count)
		if count > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
			return
		}
		if _, err := os.Stat(qwenTTSProjectDir(code)); err == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
			return
		}
		if err := os.Rename(qwenTTSProjectDir(project.Code), qwenTTSProjectDir(code)); err != nil && !os.IsNotExist(err) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "重命名项目目录失败"})
			return
		}
		oldPrefix := "/" + filepath.ToSlash(qwenTTSProjectDir(project.Code))
		newPrefix := "/" + filepath.ToSlash(qwenTTSProjectDir(code))
		replacePrefix := func(path string) string {
			if strings.HasPrefix(path, oldPrefix) {
				return newPrefix + strings.TrimPrefix(path, oldPrefix)
			}
			return path
		}
		var characters []models.QwenTTSCharacter
		_ = db.DB.Where("project_id = ?", project.ID).Find(&characters).Error
		for _, character := range characters {
			_ = db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
				"reference_audio":      replacePrefix(character.ReferenceAudio),
				"reference_test_audio": replacePrefix(character.ReferenceTestAudio),
			}).Error
		}
		var lines []models.QwenTTSLine
		_ = db.DB.Where("project_id = ?", project.ID).Find(&lines).Error
		for _, line := range lines {
			_ = db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Update("generated_audio", replacePrefix(line.GeneratedAudio)).Error
		}
	}
	updates := map[string]interface{}{
		"name":        name,
		"code":        code,
		"description": strings.TrimSpace(req.Description),
		"script_text": strings.TrimSpace(req.ScriptText),
		"instruct":    strings.TrimSpace(req.Instruct),
		"temperature": normalizeQwenTTSTemperature(req.Temperature),
		"updated_at":  time.Now(),
	}
	if req.XVectorOnly != nil {
		updates["x_vector_only"] = *req.XVectorOnly
	}
	if err := db.DB.Model(&models.QwenTTSProject{}).Where("id = ?", project.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新 Qwen3 TTS 项目失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "项目已更新"})
}

func DeleteQwenTTSProject(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.QwenTTSLine{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.QwenTTSCharacter{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.QwenTTSProject{}, project.ID).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除 Qwen3 TTS 项目失败"})
		return
	}
	_ = os.RemoveAll(qwenTTSProjectDir(project.Code))
	c.JSON(http.StatusOK, gin.H{"message": "项目已删除"})
}

func ListQwenTTSCharacters(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	var characters []models.QwenTTSCharacter
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&characters).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取角色声音资产失败"})
		return
	}
	c.JSON(http.StatusOK, characters)
}

func saveQwenTTSCharacterAudio(c *gin.Context, project models.QwenTTSProject, characterID uint, file *multipart.FileHeader) (string, error) {
	if file == nil {
		return "", nil
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext == "" {
		ext = ".mp3"
	}
	if err := os.MkdirAll(qwenTTSCharacterDir(project.Code), 0755); err != nil {
		return "", err
	}
	absPath := qwenTTSCharacterAudioPath(project.Code, characterID, ext)
	if err := c.SaveUploadedFile(file, absPath); err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(absPath), nil
}

func CreateQwenTTSCharacter(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	referenceText := strings.TrimSpace(c.PostForm("reference_text"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写角色名"})
		return
	}
	var count int64
	db.DB.Model(&models.QwenTTSCharacter{}).Where("project_id = ? AND name = ?", project.ID, name).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "角色名已存在"})
		return
	}
	var existingCount int64
	db.DB.Model(&models.QwenTTSCharacter{}).Where("project_id = ?", project.ID).Count(&existingCount)
	now := time.Now()
	character := models.QwenTTSCharacter{
		ProjectID:           project.ID,
		SortOrder:           int(existingCount) + 1,
		Name:                name,
		ReferenceText:       referenceText,
		ReferenceTextStatus: audioCloneLineStatusDraft,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if referenceText != "" {
		character.ReferenceTextStatus = audioCloneLineStatusGenerated
	}
	file, _ := c.FormFile("reference_audio")
	if err := db.DB.Create(&character).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建角色声音资产失败"})
		return
	}
	if file != nil {
		webPath, err := saveQwenTTSCharacterAudio(c, *project, character.ID, file)
		if err != nil {
			_ = db.DB.Delete(&models.QwenTTSCharacter{}, character.ID).Error
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存参考音频失败"})
			return
		}
		character.ReferenceAudio = webPath
		_ = db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
			"reference_audio": webPath,
			"reference_text_status": func() string {
				if referenceText != "" {
					return audioCloneLineStatusGenerated
				}
				return audioCloneLineStatusDraft
			}(),
			"reference_text_error": "",
			"updated_at":           time.Now(),
		}).Error
		if referenceText == "" {
			if taskID, err := startQwenTTSReferenceRecognitionTask(&character, project, getConfiguredGlobalSeed()); err == nil {
				character.ReferenceTextStatus = audioCloneLineStatusGenerating
				character.ReferenceTextCurrentTaskID = taskID
				_ = db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
					"reference_text_status":          audioCloneLineStatusGenerating,
					"reference_text_current_task_id": taskID,
					"reference_text_error":           "",
					"updated_at":                     time.Now(),
				}).Error
			} else {
				character.ReferenceTextStatus = audioCloneLineStatusFailed
				character.ReferenceTextError = err.Error()
				_ = db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
					"reference_text_status": audioCloneLineStatusFailed,
					"reference_text_error":  err.Error(),
					"updated_at":            time.Now(),
				}).Error
			}
		}
	}
	_ = db.DB.First(&character, character.ID).Error
	c.JSON(http.StatusCreated, character)
}

func UpdateQwenTTSCharacter(c *gin.Context) {
	character, err := loadQwenTTSCharacterOr404(c)
	if err != nil {
		return
	}
	var project models.QwenTTSProject
	if err := db.DB.First(&project, character.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	referenceText := strings.TrimSpace(c.PostForm("reference_text"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写角色名"})
		return
	}
	var count int64
	db.DB.Model(&models.QwenTTSCharacter{}).Where("project_id = ? AND name = ? AND id <> ?", character.ProjectID, name, character.ID).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "角色名已存在"})
		return
	}
	updates := map[string]interface{}{
		"name":                           name,
		"reference_text":                 referenceText,
		"reference_text_current_task_id": "",
		"reference_text_error":           "",
		"reference_text_status":          audioCloneLineStatusDraft,
		"updated_at":                     time.Now(),
	}
	if referenceText != "" {
		updates["reference_text_status"] = audioCloneLineStatusGenerated
	}
	audioChanged := false
	if file, err := c.FormFile("reference_audio"); err == nil && file != nil {
		webPath, err := saveQwenTTSCharacterAudio(c, project, character.ID, file)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存参考音频失败"})
			return
		}
		_ = removeAudioCloneAsset(character.ReferenceAudio)
		_ = removeAudioCloneAsset(character.ReferenceTestAudio)
		updates["reference_audio"] = webPath
		updates["reference_test_audio"] = ""
		audioChanged = true
	}
	if err := db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", character.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新角色声音资产失败"})
		return
	}
	if audioChanged && referenceText == "" {
		var updated models.QwenTTSCharacter
		if err := db.DB.First(&updated, character.ID).Error; err == nil {
			if taskID, err := startQwenTTSReferenceRecognitionTask(&updated, &project, getConfiguredGlobalSeed()); err == nil {
				_ = db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", updated.ID).Updates(map[string]interface{}{
					"reference_text_status":          audioCloneLineStatusGenerating,
					"reference_text_current_task_id": taskID,
					"reference_text_error":           "",
					"updated_at":                     time.Now(),
				}).Error
			} else {
				_ = db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", updated.ID).Updates(map[string]interface{}{
					"reference_text_status": audioCloneLineStatusFailed,
					"reference_text_error":  err.Error(),
					"updated_at":            time.Now(),
				}).Error
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "角色声音资产已更新"})
}

func DeleteQwenTTSCharacter(c *gin.Context) {
	character, err := loadQwenTTSCharacterOr404(c)
	if err != nil {
		return
	}
	if err := db.DB.Delete(&models.QwenTTSCharacter{}, character.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除角色声音资产失败"})
		return
	}
	_ = removeAudioCloneAsset(character.ReferenceAudio)
	_ = removeAudioCloneAsset(character.ReferenceTestAudio)
	c.JSON(http.StatusOK, gin.H{"message": "角色声音资产已删除"})
}

func RecognizeQwenTTSCharacterReference(c *gin.Context) {
	character, err := loadQwenTTSCharacterOr404(c)
	if err != nil {
		return
	}
	if strings.TrimSpace(character.ReferenceAudio) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先上传参考音频"})
		return
	}
	var project models.QwenTTSProject
	if err := db.DB.First(&project, character.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	taskID, err := startQwenTTSReferenceRecognitionTask(character, &project, getConfiguredGlobalSeed())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交参考音频识别任务失败"})
		return
	}
	if err := db.DB.Model(&models.QwenTTSCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
		"reference_text_status":          audioCloneLineStatusGenerating,
		"reference_text_current_task_id": taskID,
		"reference_text_error":           "",
		"updated_at":                     time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新参考音频识别状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "参考音频识别任务已提交", "task_id": taskID})
}

func ListQwenTTSLines(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	var lines []models.QwenTTSLine
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&lines).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取生成行失败"})
		return
	}
	defaultTemperature := normalizeQwenTTSTemperature(project.Temperature)
	defaultSeed := normalizeQwenTTSSeed(getConfiguredGlobalSeed())
	for idx := range lines {
		updates := map[string]interface{}{}
		if lines[idx].Temperature <= 0 {
			lines[idx].Temperature = defaultTemperature
			updates["temperature"] = defaultTemperature
		}
		if lines[idx].Seed <= 0 {
			lines[idx].Seed = defaultSeed
			updates["seed"] = defaultSeed
		}
		if len(updates) > 0 {
			updates["updated_at"] = time.Now()
			_ = db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", lines[idx].ID).Updates(updates).Error
		}
	}
	c.JSON(http.StatusOK, lines)
}

func SaveQwenTTSScriptLines(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	var req struct {
		ScriptText    string                 `json:"script_text"`
		ImportedItems []qwenTTSAutoParseItem `json:"imported_items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	script := strings.TrimSpace(req.ScriptText)
	parsed, err := parseAudioCloneScript(script)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	missing := validateQwenTTSLineAssets(project.ID, parsed)
	lines, err := replaceQwenTTSLines(project.ID, script, project.Instruct, project.Temperature, getConfiguredGlobalSeed(), buildQwenTTSInferredInstructMap(req.ImportedItems))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解析脚本失败"})
		return
	}
	if err := db.DB.Model(&models.QwenTTSProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"script_text": script,
		"updated_at":  time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存脚本失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lines": lines, "missing_characters": missing})
}

func BuildQwenTTSImportPrompt(c *gin.Context) {
	if _, err := loadQwenTTSProjectOr404(c); err != nil {
		return
	}
	var req struct {
		SourceText string `json:"source_text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	sourceText := strings.TrimSpace(req.SourceText)
	if sourceText == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先输入小说原文"})
		return
	}
	systemPrompt, userPrompt := buildQwenTTSAutoParsePrompts(sourceText)
	content := strings.TrimSpace(fmt.Sprintf(`
[系统提示词]
%s

[用户消息]
%s
`, systemPrompt, userPrompt))
	c.JSON(http.StatusOK, gin.H{
		"content":       content,
		"system_prompt": systemPrompt,
		"user_prompt":   userPrompt,
	})
}

func ImportQwenTTSResult(c *gin.Context) {
	if _, err := loadQwenTTSProjectOr404(c); err != nil {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	resp, err := parseQwenTTSImportedResponse(req.Content)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	scriptText := buildQwenTTSScriptTextFromItems(resp.Items)
	if strings.TrimSpace(scriptText) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "导入结果里没有可用脚本内容"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"script_text": scriptText,
		"items":       resp.Items,
		"count":       len(resp.Items),
		"total":       resp.Total,
	})
}

func SaveQwenTTSScriptText(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	var req struct {
		ScriptText string `json:"script_text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	script := strings.TrimSpace(req.ScriptText)
	if err := db.DB.Model(&models.QwenTTSProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"script_text": script,
		"updated_at":  time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存脚本失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "脚本已保存"})
}

func GenerateQwenTTSProjectLines(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	var req struct {
		ScriptText    string                 `json:"script_text"`
		RandomSeed    bool                   `json:"random_seed"`
		ImportedItems []qwenTTSAutoParseItem `json:"imported_items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	script := strings.TrimSpace(req.ScriptText)
	parsed, err := parseAudioCloneScript(script)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	missing := validateQwenTTSLineAssets(project.ID, parsed)
	if len(missing) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":              "存在缺少声音资产的角色，请先补齐",
			"missing_characters": missing,
		})
		return
	}
	lines, err := replaceQwenTTSLines(project.ID, script, project.Instruct, project.Temperature, getConfiguredGlobalSeed(), buildQwenTTSInferredInstructMap(req.ImportedItems))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存生成行失败"})
		return
	}
	if err := db.DB.Model(&models.QwenTTSProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"script_text": script,
		"updated_at":  time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存脚本失败"})
		return
	}
	started := 0
	seedForBatch := int64(0)
	if req.RandomSeed {
		seedForBatch = randomQwenTTSSeed()
	}
	for _, line := range lines {
		if line.Status == audioCloneLineStatusGenerated && strings.TrimSpace(line.GeneratedAudio) != "" {
			continue
		}
		lineCopy := line
		seed := normalizeQwenTTSSeed(line.Seed)
		if line.Seed <= 0 {
			seed = normalizeQwenTTSSeed(getConfiguredGlobalSeed())
		}
		if req.RandomSeed {
			seed = seedForBatch
		}
		lineCopy.Seed = seed
		promptID, workflowLabel, err := queueQwenTTSLinePrompt(*project, lineCopy, seed)
		if err != nil {
			_ = db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":     audioCloneLineStatusFailed,
				"last_error": err.Error(),
				"seed":       seed,
				"updated_at": time.Now(),
			}).Error
			continue
		}
		taskID, err := startQwenTTSLineTask(&lineCopy, project, seed, promptID, workflowLabel)
		if err != nil {
			_ = db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":     audioCloneLineStatusFailed,
				"last_error": err.Error(),
				"updated_at": time.Now(),
			}).Error
			continue
		}
		_ = db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
			"status":          audioCloneLineStatusGenerating,
			"current_task_id": taskID,
			"seed":            seed,
			"last_error":      "",
			"updated_at":      time.Now(),
		}).Error
		started++
	}
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("已提交 %d 条 Qwen3 TTS 任务", started), "lines": lines})
}

func ResetQwenTTSProjectLineStates(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	if err := db.DB.Model(&models.QwenTTSLine{}).Where("project_id = ?", project.ID).Updates(map[string]interface{}{
		"status":          audioCloneLineStatusDraft,
		"current_task_id": "",
		"last_error":      "",
		"updated_at":      time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置 Qwen3 TTS 项目状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Qwen3 TTS 项目状态已重置"})
}

func ResetQwenTTSProjectLines(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	var lines []models.QwenTTSLine
	if err := db.DB.Where("project_id = ?", project.ID).Find(&lines).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取 Qwen3 TTS 台词行失败"})
		return
	}
	generatedAudio := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line.GeneratedAudio) != "" {
			generatedAudio = append(generatedAudio, line.GeneratedAudio)
		}
	}
	if err := db.DB.Where("project_id = ?", project.ID).Delete(&models.QwenTTSLine{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "全部重置 Qwen3 TTS 行失败"})
		return
	}
	for _, path := range generatedAudio {
		_ = removeAudioCloneAsset(path)
	}
	c.JSON(http.StatusOK, gin.H{"message": "Qwen3 TTS 下方台词行已全部清空，脚本已保留"})
}

func InterruptQwenTTSProjectGeneration(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}
	if err := StopComfyUI(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "停止 ComfyUI 失败: " + err.Error()})
		return
	}
	if err := db.DB.Model(&models.QwenTTSLine{}).Where("project_id = ? AND status = ?", project.ID, audioCloneLineStatusGenerating).Updates(map[string]interface{}{
		"status":          audioCloneLineStatusDraft,
		"current_task_id": "",
		"last_error":      "已手动停止",
		"updated_at":      time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置 Qwen3 TTS 生成中状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已停止当前 Qwen3 TTS 生成任务"})
}

func GenerateQwenTTSLine(c *gin.Context) {
	line, err := loadQwenTTSLineOr404(c)
	if err != nil {
		return
	}
	var project models.QwenTTSProject
	if err := db.DB.First(&project, line.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	var req struct {
		RandomSeed bool `json:"random_seed"`
	}
	_ = c.ShouldBindJSON(&req)
	parsed := []audioCloneParsedLine{{CharacterName: line.CharacterName, Text: line.Text, SortOrder: line.SortOrder}}
	missing := validateQwenTTSLineAssets(project.ID, parsed)
	if len(missing) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":              "存在缺少声音资产的角色，请先补齐",
			"missing_characters": missing,
		})
		return
	}
	seed := normalizeQwenTTSSeed(line.Seed)
	if line.Seed <= 0 {
		seed = normalizeQwenTTSSeed(getConfiguredGlobalSeed())
	}
	if req.RandomSeed {
		seed = randomQwenTTSSeed()
	}
	line.Seed = seed
	taskID, err := startQwenTTSLineTask(line, &project, seed, "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交 Qwen3 TTS 任务失败"})
		return
	}
	if err := db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
		"status":          audioCloneLineStatusGenerating,
		"current_task_id": taskID,
		"seed":            seed,
		"last_error":      "",
		"updated_at":      time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新 Qwen3 TTS 行状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Qwen3 TTS 任务已提交", "task_id": taskID})
}

func UpdateQwenTTSLine(c *gin.Context) {
	line, err := loadQwenTTSLineOr404(c)
	if err != nil {
		return
	}
	var req struct {
		Text        string  `json:"text"`
		Instruct    string  `json:"instruct"`
		Temperature float64 `json:"temperature"`
		Seed        int64   `json:"seed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		text = strings.TrimSpace(line.Text)
	}
	instruct := strings.TrimSpace(req.Instruct)
	temperature := normalizeQwenTTSTemperature(req.Temperature)
	seed := normalizeQwenTTSSeed(req.Seed)
	if req.Seed <= 0 {
		seed = normalizeQwenTTSSeed(getConfiguredGlobalSeed())
	}
	changed := strings.TrimSpace(line.Text) != text ||
		strings.TrimSpace(line.Instruct) != instruct ||
		normalizeQwenTTSTemperature(line.Temperature) != temperature ||
		normalizeQwenTTSSeed(line.Seed) != seed
	updates := map[string]interface{}{
		"text":        text,
		"instruct":    instruct,
		"temperature": temperature,
		"seed":        seed,
		"updated_at":  time.Now(),
	}
	if changed {
		_ = removeAudioCloneAsset(line.GeneratedAudio)
		updates["status"] = audioCloneLineStatusDraft
		updates["current_task_id"] = ""
		updates["last_error"] = ""
		updates["generated_audio"] = ""
		updates["generated_workflow"] = ""
	}
	if err := db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新 Qwen3 TTS 行失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Qwen3 TTS 行已更新"})
}

func ResetQwenTTSLineState(c *gin.Context) {
	line, err := loadQwenTTSLineOr404(c)
	if err != nil {
		return
	}
	if err := db.DB.Model(&models.QwenTTSLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
		"status":          audioCloneLineStatusDraft,
		"current_task_id": "",
		"last_error":      "",
		"updated_at":      time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置 Qwen3 TTS 行状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Qwen3 TTS 行状态已重置"})
}
