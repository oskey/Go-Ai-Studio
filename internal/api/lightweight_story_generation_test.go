package api

import "testing"

func TestEstimatePromptTokens(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantLow  int
		wantHigh int
	}{
		{"chinese-only", "请根据以下探店项目总文案，一次性生成全部区域的介绍摘要和视频提示词。", 20, 40},
		{"mixed", "hello 世界 你好 world foo bar", 6, 12},
		{"empty", "", 0, 0},
	}
	for _, c := range cases {
		got := estimatePromptTokens(c.input)
		if got < c.wantLow || got > c.wantHigh {
			t.Errorf("%s: estimatePromptTokens(%q) = %d, want within [%d, %d]", c.name, c.input, got, c.wantLow, c.wantHigh)
		}
	}
}