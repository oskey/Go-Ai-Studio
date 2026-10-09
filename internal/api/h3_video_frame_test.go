package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/workflow"
)

func TestNormalizeH3VideoFrameSize(t *testing.T) {
	cases := []struct {
		w, h         int
		wantW, wantH int
	}{
		{1024, 1024, 976, 976},
		{768, 1344, 736, 1296},
		{512, 512, 512, 512},
		{0, 0, 736, 1296},
		{100, 100, 96, 96},
		{7, 7, 16, 16},
	}
	for _, c := range cases {
		gotW, gotH := normalizeH3VideoFrameSize(c.w, c.h)
		if gotW != c.wantW || gotH != c.wantH {
			t.Errorf("normalizeH3VideoFrameSize(%d,%d) = (%d,%d), want (%d,%d)", c.w, c.h, gotW, gotH, c.wantW, c.wantH)
		}
		if gotW%h3VideoFrameSizeMultiple != 0 || gotH%h3VideoFrameSizeMultiple != 0 {
			t.Errorf("normalizeH3VideoFrameSize(%d,%d) = (%d,%d) not aligned to %d", c.w, c.h, gotW, gotH, h3VideoFrameSizeMultiple)
		}
		if gotW <= 0 || gotH <= 0 {
			t.Errorf("normalizeH3VideoFrameSize(%d,%d) returned non-positive", c.w, c.h)
		}
	}
}

func TestIsVideoOutputFilename(t *testing.T) {
	for _, name := range []string{"a.mp4", "b.WEBM", "c.gif", "d.mov", "e.mkv"} {
		if !isVideoOutputFilename(name) {
			t.Errorf("isVideoOutputFilename(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"a.png", "b.jpg", "noext", "", "  "} {
		if isVideoOutputFilename(name) {
			t.Errorf("isVideoOutputFilename(%q) = true, want false", name)
		}
	}
}

func TestInjectH3T2VParams(t *testing.T) {
	workflowPath := filepath.Join("..", "..", "workflows", h3T2VWorkflowFileName)
	if _, err := os.Stat(workflowPath); err != nil {
		t.Skipf("H3 t2v workflow not present: %v", err)
	}
	meta, err := workflow.ParseWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("ParseWorkflow: %v", err)
	}
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	var wfJSON map[string]interface{}
	if err := json.Unmarshal(data, &wfJSON); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if meta.PositiveNodeID == "" || meta.SeedNodeID == "" || meta.WidthNodeID == "" || meta.HeightNodeID == "" {
		t.Fatalf("parser missed node ids: %+v", meta)
	}
	t.Logf("meta: pos=%s/%s neg=%s/%s seed=%s/%s width=%s/%s height=%s/%s",
		meta.PositiveNodeID, meta.PositiveInputKey, meta.NegativeNodeID, meta.NegativeInputKey,
		meta.SeedNodeID, meta.SeedInputKey, meta.WidthNodeID, meta.WidthInputKey,
		meta.HeightNodeID, meta.HeightInputKey)

	injectH3T2VParams(wfJSON, meta, "test prompt", 12345, 512, 512)

	inputValue := func(nodeID, key string) interface{} {
		node, _ := wfJSON[nodeID].(map[string]interface{})
		inputs, _ := node["inputs"].(map[string]interface{})
		return inputs[key]
	}
	if got := inputValue(meta.PositiveNodeID, meta.PositiveInputKey); got != "test prompt" {
		t.Errorf("positive prompt = %v, want test prompt", got)
	}
	if got := inputValue(meta.SeedNodeID, meta.SeedInputKey); got != int64(12345) && got != 12345 {
		t.Errorf("seed = %v (%T), want 12345", got, got)
	}
	if got := inputValue(meta.WidthNodeID, meta.WidthInputKey); got != 512 {
		t.Errorf("width = %v, want 512", got)
	}
	if got := inputValue(meta.HeightNodeID, meta.HeightInputKey); got != 512 {
		t.Errorf("height = %v, want 512", got)
	}

	foundDuration := false
	for _, node := range wfJSON {
		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		if classType, _ := nodeMap["class_type"].(string); classType != "PrimitiveFloat" {
			continue
		}
		metaMap, _ := nodeMap["_meta"].(map[string]interface{})
		title, _ := metaMap["title"].(string)
		if title == "" {
			continue
		}
		foundDuration = true
		inputs, _ := nodeMap["inputs"].(map[string]interface{})
		if inputs["value"] != h3VideoFrameDurationSeconds {
			t.Errorf("duration node %q value = %v, want %v", title, inputs["value"], h3VideoFrameDurationSeconds)
		}
	}
	if !foundDuration {
		t.Error("no PrimitiveFloat node found in H3 t2v workflow")
	}
}

func TestMergeH3StaticPrompt(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		extra  string
		want   string
	}{
		{"空附加词原样返回", "场景描述", "", "场景描述"},
		{"纯空白附加词原样返回", "场景描述", "  \n ", "场景描述"},
		{"正常追加到末尾", "场景描述", "完全静止", "场景描述\n完全静止"},
		{"已包含时去重", "场景描述\n完全静止", "完全静止", "场景描述\n完全静止"},
		{"空提示词只留附加词", "   ", "完全静止", "完全静止"},
		{"附加词首尾空格被清理", "场景描述", "  完全静止  ", "场景描述\n完全静止"},
	}
	for _, c := range cases {
		if got := mergeH3StaticPrompt(c.prompt, c.extra); got != c.want {
			t.Errorf("%s: mergeH3StaticPrompt(%q, %q) = %q, want %q", c.name, c.prompt, c.extra, got, c.want)
		}
	}
}

func TestH3VideoFramePromptPresets(t *testing.T) {
	if len(h3VideoFramePromptPresets) < 3 {
		t.Fatalf("expected at least 3 presets, got %d", len(h3VideoFramePromptPresets))
	}
	seen := map[string]bool{}
	for _, p := range h3VideoFramePromptPresets {
		if p.ID == "" || p.Label == "" || p.Text == "" {
			t.Errorf("preset has empty field: %+v", p)
		}
		if seen[p.ID] {
			t.Errorf("duplicate preset id %q", p.ID)
		}
		seen[p.ID] = true
		if strings.TrimSpace(p.Text) != p.Text {
			t.Errorf("preset %q text has leading/trailing whitespace", p.ID)
		}
		if !strings.Contains(p.Text, "慢动作") {
			t.Errorf("preset %q must disambiguate slow motion", p.ID)
		}
		if n := len([]rune(p.Text)); n < 60 || n > 400 {
			t.Errorf("preset %q rune length = %d, want 60-400", p.ID, n)
		}
	}
	if got := h3VideoFrameDefaultPrompt(); got != h3VideoFramePromptPresets[0].Text {
		t.Error("default prompt must equal the first preset text")
	}
}

func TestInjectH3SegmentDuration(t *testing.T) {
	for _, c := range []struct {
		length, fps int
		want        float64
	}{
		{169, 24, 7},
		{121, 24, 5},
		{0, 24, 0},
		{10, 0, 0},
	} {
		got := 0.0
		if c.fps > 0 && c.length > 1 {
			got = float64(c.length-1) / float64(c.fps)
		}
		if got != c.want {
			t.Errorf("reverseDuration(=%d fps=%d) = %v, want %v", c.length, c.fps, got, c.want)
		}
	}

	workflowPath := filepath.Join("..", "..", "workflows", h3T2VWorkflowFileName)
	if _, err := os.Stat(workflowPath); err != nil {
		t.Skipf("H3 t2v workflow not present: %v", err)
	}
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	var wfJSON map[string]interface{}
	if err := json.Unmarshal(data, &wfJSON); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !isH3R2VWorkflow(wfJSON) {
		t.Fatal("isH3R2VWorkflow(t2v) = false, want true")
	}
	injectH3Duration(wfJSON, 7)
	for _, node := range wfJSON {
		nodeMap, _ := node.(map[string]interface{})
		if classType, _ := nodeMap["class_type"].(string); classType != "PrimitiveFloat" {
			continue
		}
		metaMap, _ := nodeMap["_meta"].(map[string]interface{})
		title, _ := metaMap["title"].(string)
		if strings.Contains(strings.ToLower(title), "duration") {
			inputs, _ := nodeMap["inputs"].(map[string]interface{})
			if inputs["value"] != 7.0 {
				t.Errorf("duration node %q = %v, want 7", title, inputs["value"])
			}
		}
	}
}

func TestH3ReferenceWorkflowLoads(t *testing.T) {
	workflowPath := filepath.Join("..", "..", "workflows", h3Ref2VWorkflowFileName)
	if _, err := os.Stat(workflowPath); err != nil {
		t.Skipf("H3 ref2v workflow not present: %v", err)
	}
	meta, err := workflow.ParseWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("ParseWorkflow: %v", err)
	}
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	var wfJSON map[string]interface{}
	if err := json.Unmarshal(data, &wfJSON); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if meta.PositiveNodeID == "" || meta.SeedNodeID == "" || meta.WidthNodeID == "" || meta.HeightNodeID == "" {
		t.Fatalf("parser missed node ids: %+v", meta)
	}

	stripH3Ref2VExampleAssets(wfJSON)

	// 官方示例的音频/视频素材节点必须被剥离，否则提交校验会因 input 缺文件失败。
	for _, classType := range []string{"LoadAudio", "LoadVideo", "GetVideoComponents"} {
		for id, node := range wfJSON {
			nodeMap, _ := node.(map[string]interface{})
			if got, _ := nodeMap["class_type"].(string); got == classType {
				t.Errorf("node %s remains class %s after strip", id, classType)
			}
		}
	}

	// ReferenceToVideo 节点只能保留 ref_images.* 输入。
	hasRefImage := false
	for _, node := range wfJSON {
		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		if got, _ := nodeMap["class_type"].(string); got != "MiniMaxH3ReferenceToVideo" {
			continue
		}
		inputs, _ := nodeMap["inputs"].(map[string]interface{})
		for key := range inputs {
			if strings.HasPrefix(key, "ref_videos") || strings.HasPrefix(key, "ref_video_audios") || strings.HasPrefix(key, "ref_audios") {
				t.Errorf("ReferenceToVideo retains %q after strip", key)
			}
			if strings.HasPrefix(key, "ref_images") {
				hasRefImage = true
			}
		}
	}
	if !hasRefImage {
		t.Error("ReferenceToVideo lost its ref_images input after strip")
	}
}

func TestH3TargetFrameIndex(t *testing.T) {
	cases := []struct {
		total int
		pick  string
		want  int
	}{
		{5, H3FramePickFirst, 0},
		{5, H3FramePickMiddle, 2},
		{5, H3FramePickLast, 4},
		{5, "", 2},
		{5, "weird", 2},
		{1, H3FramePickMiddle, 0},
		{1, H3FramePickLast, 0},
		{0, H3FramePickMiddle, 0},
		{0, H3FramePickLast, 0},
		{5, "LAST", 4},
	}
for _, c := range cases {
		if got := h3TargetFrameIndex(c.total, c.pick); got != c.want {
			t.Errorf("h3TargetFrameIndex(%d, %q) = %d, want %d", c.total, c.pick, got, c.want)
		}
	}
}

func TestCountR2VSegments(t *testing.T) {
	cases := []struct {
		total, segmentDuration, want int
	}{
		{8, 5, 2},
		{3, 5, 1},
		{5, 5, 1},
		{10, 5, 2},
		{7, 5, 2},
		{0, 5, 1},
		{-1, 5, 1},
		{10, 3, 4},
		{9, 3, 3},
		{8, 3, 3},
		{3, 0, 1},
	}
	for _, c := range cases {
		if got := countR2VSegments(c.total, c.segmentDuration); got != c.want {
			t.Errorf("countR2VSegments(%d, %d) = %d, want %d", c.total, c.segmentDuration, got, c.want)
		}
	}
}

func h3TestRefChars(n int) []models.Character {
	chars := make([]models.Character, 0, n)
	for i := 1; i <= n; i++ {
		chars = append(chars, models.Character{
			RefImage: fmt.Sprintf("/output/test/ref_char_%d.png", i),
		})
	}
	return chars
}

func TestPlanH3CharacterRefInjections(t *testing.T) {
	chars := h3TestRefChars(10)
	cases := []struct {
		name        string
		chars       []models.Character
		prompt      string
		wantInject  int
		wantBridged string
	}{
		{"无引用原样返回", chars, "一个房间", 0, "一个房间"},
		{"空提示词返回", chars, "", 0, ""},
		{"单个引用桥接为 Picture 2", chars, "参考图@图1在门口", 1, "参考图<Picture 2>在门口"},
		{"@图3 注入前3个并桥接", chars, "参考图@图3走进房间", 3, "参考图<Picture 4>走进房间"},
		{"多处引用取最大编号", chars, "a@图2 b@图5", 5, "a<Picture 3> b<Picture 6>"},
		{"重复引用只取一次", chars, "@图2和@图2", 2, "<Picture 3>和<Picture 3>"},
		{"引用超槽位上限截断为8", chars, "@图9", 8, "@图9"},
		{"引用超槽位上限桥接照常", chars, "@图8走进来", 8, "<Picture 9>走进来"},
		{"资产数小于槽位上限时按资产截断", h3TestRefChars(5), "@图7", 5, "@图7"},
		{"资产数截断时桥接只命中可注入范围", h3TestRefChars(5), "@图5进来", 5, "<Picture 6>进来"},
		{"@图0 无效忽略", chars, "参考图@图0", 0, "参考图@图0"},
		{"@图字面量非数字", chars, "约等于@图X", 0, "约等于@图X"},
	}
	for _, c := range cases {
		gotChars, bridged := planH3CharacterRefInjections(c.prompt, c.chars)
		if len(gotChars) != c.wantInject {
			t.Errorf("%s: injected %d chars, want %d", c.name, len(gotChars), c.wantInject)
		}
		if bridged != c.wantBridged {
			t.Errorf("%s: bridged = %q, want %q", c.name, bridged, c.wantBridged)
		}
	}
}

func TestInjectH3Ref2VCharacterRefs(t *testing.T) {
	workflowPath := filepath.Join("..", "..", "workflows", h3Ref2VWorkflowFileName)
	if _, err := os.Stat(workflowPath); err != nil {
		t.Skipf("H3 ref2v workflow not present: %v", err)
	}
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	var wfJSON map[string]interface{}
	if err := json.Unmarshal(data, &wfJSON); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	stripH3Ref2VExampleAssets(wfJSON)

	refNodeByClass := func() map[string]interface{} {
		for _, node := range wfJSON {
			nodeMap, ok := node.(map[string]interface{})
			if !ok {
				continue
			}
			if ct, _ := nodeMap["class_type"].(string); ct == "MiniMaxH3ReferenceToVideo" {
				return nodeMap
			}
		}
		return nil
	}

	loadImageNodes := func() map[string]bool {
		nodes := map[string]bool{}
		for id, node := range wfJSON {
			nodeMap, _ := node.(map[string]interface{})
			if ct, _ := nodeMap["class_type"].(string); ct == "LoadImage" {
				nodes[id] = true
			}
		}
		return nodes
	}

	// 无引用：不动 workflow、不新增节点
	beforeCount := len(wfJSON)
	orig, err := injectH3Ref2VCharacterRefs(wfJSON, "一个房间", h3TestRefChars(3), func(p string) (string, error) {
		return filepath.Base(p), nil
	})
	if err != nil {
		t.Fatalf("no-ref inject errored: %v", err)
	}
	if orig != "一个房间" {
		t.Errorf("no-ref bridged = %q, want original", orig)
	}
	if len(wfJSON) != beforeCount {
		t.Errorf("no-ref added %d nodes", len(wfJSON)-beforeCount)
	}

	// 有引用：@图2 → 注入前 2 个（图1+图2）LoadImage，接到 ref_image_1/ref_image_2，
	// ref_image_0（场景参考图）不被覆盖
	existingLoadImages := loadImageNodes()
	refNodeBefore := refNodeByClass()
	refImage0, has0 := refNodeBefore["inputs"].(map[string]interface{})["ref_images.ref_image_0"]
	bridged, err := injectH3Ref2VCharacterRefs(wfJSON, "参考图@图2在门口", h3TestRefChars(3), func(p string) (string, error) {
		return filepath.Base(p), nil
	})
	if err != nil {
		t.Fatalf("inject errored: %v", err)
	}
	if bridged != "参考图<Picture 3>在门口" {
		t.Errorf("bridged = %q, want 参考图<Picture 3>在门口", bridged)
	}
	addNodes := loadImageNodes()
	newNodes := []string{}
	for id := range addNodes {
		if !existingLoadImages[id] {
			newNodes = append(newNodes, id)
		}
	}
	if len(newNodes) != 2 {
		t.Fatalf("expected 2 new LoadImage nodes, got %d (%v)", len(newNodes), newNodes)
	}
	refNode := refNodeByClass()
	inputs := refNode["inputs"].(map[string]interface{})
	if has0 && !reflect.DeepEqual(inputs["ref_images.ref_image_0"], refImage0) {
		t.Errorf("ref_image_0 overwritten: %v -> %v", refImage0, inputs["ref_images.ref_image_0"])
	}
	wire1, ok := inputs["ref_images.ref_image_1"].([]interface{})
	if !ok || len(wire1) != 2 || !addNodes[fmt.Sprintf("%v", wire1[0])] {
		t.Fatalf("ref_image_1 not wired to new LoadImage, got %v", inputs["ref_images.ref_image_1"])
	}
	wire2, ok := inputs["ref_images.ref_image_2"].([]interface{})
	if !ok || len(wire2) != 2 || !addNodes[fmt.Sprintf("%v", wire2[0])] {
		t.Fatalf("ref_image_2 not wired to new LoadImage, got %v", inputs["ref_images.ref_image_2"])
	}
	if wire1[0] == wire2[0] {
		t.Error("ref_image_1 and ref_image_2 point to the same node")
	}
	if _, ok := inputs["ref_images.ref_image_3"]; ok {
		t.Error("ref_image_3 should not be wired (只注入 @图2)")
	}

	// 测试 upload 失败传播：不新增节点、不接线
	before2 := len(wfJSON)
	_, err = injectH3Ref2VCharacterRefs(wfJSON, "@图1", h3TestRefChars(2), func(p string) (string, error) {
		return "", fmt.Errorf("upload fail")
	})
	if err == nil {
		t.Error("upload failure not propagated")
	}
	if len(wfJSON) != before2 {
		t.Error("failed inject mutated workflow")
	}
}

func TestH3NextFreeNodeID(t *testing.T) {
	wf := map[string]interface{}{
		"100": map[string]interface{}{},
		"300": map[string]interface{}{},
		"40":  map[string]interface{}{},
		"abc": map[string]interface{}{},
	}
	if got := h3NextFreeNodeID(wf); got != 301 {
		t.Errorf("h3NextFreeNodeID = %d, want 301", got)
	}
}
