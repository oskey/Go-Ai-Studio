package api

import (
	"fmt"
	"strings"
)

var negativePromptLeadIns = []string{
	"不要出现", "不要有", "不要让", "不要再有", "不要再", "不要",
	"别出现", "别有", "别让", "别再有", "别再", "别",
	"禁止出现", "禁止有", "禁止让", "禁止",
	"严禁出现", "严禁有", "严禁让", "严禁",
	"不得出现", "不得有", "不得让", "不得",
	"不应出现", "不应有", "不应让", "不应",
	"避免出现", "避免有", "避免让", "避免",
	"排除", "去掉", "移除",
	"do not show", "do not include", "do not generate", "do not add", "do not use", "do not have", "do not",
	"don't show", "don't include", "don't generate", "don't add", "don't use", "don't have", "don't",
	"avoid", "exclude", "without", "no ",
}

var negativePromptStyleTerms = []string{
	"卡通风", "卡通风格",
	"写实风", "写实风格",
	"二次元", "二次元风", "二次元风格",
	"国漫风", "国漫风格",
	"油画风", "油画风格",
	"赛博朋克风", "赛博朋克风格",
	"照片风", "照片风格",
	"摄影风", "摄影风格",
	"3d风", "3d风格", "3d渲染感",
	"3D风", "3D风格", "3D渲染感",
	"风格漂移", "风格错误",
	"cartoon", "anime", "oil painting", "photo style", "photographic style", "3d render", "rendered look",
}

func buildNegativePromptNoStyleInstruction() string {
	return "负向提示词里禁止出现任何风格类词语，包括但不限于“卡通风、卡通风格、写实风、写实风格、二次元、二次元风格、国漫风、油画风、油画风格、赛博朋克风、照片风、摄影风、3D风、3D渲染感、风格漂移、风格错误”等。若你本来想排除某种风格倾向，必须改写成具体画面错误、结构错误、材质错误或时代/道具错误，例如“塑料材质、错误高光、纹理缺失、错误透视、额外人物、错误道具、时代错误”；不得保留任何风格词语本身。"
}

func collectNegativePromptLeadInWarnings(label string, input string) []string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil
	}

	segments := strings.FieldsFunc(trimmed, func(r rune) bool {
		switch r {
		case ',', '，', '、', ';', '；', '\n', '\r':
			return true
		default:
			return false
		}
	})
	if len(segments) == 0 {
		segments = []string{trimmed}
	}

	warnings := make([]string, 0, len(segments))
	for _, segment := range segments {
		cleaned := strings.TrimSpace(strings.Trim(segment, "\"'"))
		if cleaned == "" {
			continue
		}
		lower := strings.ToLower(cleaned)
		for _, leadIn := range negativePromptLeadIns {
			if strings.HasPrefix(lower, strings.ToLower(leadIn)) {
				warnings = append(warnings, fmt.Sprintf("%s 使用了带引导词的负向提示词片段，系统仅告警不改写：%s", label, cleaned))
				break
			}
		}
	}

	return warnings
}

func collectNegativePromptStyleWarnings(label string, input string) []string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil
	}

	segments := strings.FieldsFunc(trimmed, func(r rune) bool {
		switch r {
		case ',', '，', '、', ';', '；', '\n', '\r':
			return true
		default:
			return false
		}
	})
	if len(segments) == 0 {
		segments = []string{trimmed}
	}

	warnings := make([]string, 0, len(segments))
	for _, segment := range segments {
		cleaned := strings.TrimSpace(strings.Trim(segment, "\"'"))
		if cleaned == "" {
			continue
		}
		lower := strings.ToLower(cleaned)
		for _, term := range negativePromptStyleTerms {
			if strings.Contains(lower, strings.ToLower(term)) {
				warnings = append(warnings, fmt.Sprintf("%s 使用了风格类负向提示词片段，系统仅告警不改写：%s", label, cleaned))
				break
			}
		}
	}

	return warnings
}

func warnNegativePromptLeadIn(label string, input string) {
	for _, warning := range collectNegativePromptLeadInWarnings(label, input) {
		Log(LogLevelWarn, "负向提示词格式告警", warning)
	}
	for _, warning := range collectNegativePromptStyleWarnings(label, input) {
		Log(LogLevelWarn, "负向提示词风格词告警", warning)
	}
}

func hasNegativePromptLeadIn(segment string) bool {
	cleaned := strings.TrimSpace(strings.Trim(segment, "\"'"))
	if cleaned == "" {
		return false
	}

	lower := strings.ToLower(cleaned)
	for _, leadIn := range negativePromptLeadIns {
		if strings.HasPrefix(lower, strings.ToLower(leadIn)) {
			return true
		}
	}
	return false
}
