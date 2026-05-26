package api

import (
	"encoding/json"
	"strings"
)

type LocalizedPromptText struct {
	ZH string `json:"zh"`
	EN string `json:"en"`
}

func parseLocalizedPromptText(raw string) LocalizedPromptText {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return LocalizedPromptText{}
	}

	var payload LocalizedPromptText
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		payload.ZH = strings.TrimSpace(payload.ZH)
		payload.EN = strings.TrimSpace(payload.EN)
		if payload.ZH != "" || payload.EN != "" {
			return payload
		}
	}

	return LocalizedPromptText{ZH: trimmed}
}

func marshalLocalizedPromptText(zh string, en string) string {
	payload := LocalizedPromptText{
		ZH: strings.TrimSpace(zh),
		EN: strings.TrimSpace(en),
	}
	if payload.ZH == "" && payload.EN == "" {
		return ""
	}

	bytes, err := json.Marshal(payload)
	if err != nil {
		if payload.ZH != "" {
			return payload.ZH
		}
		return payload.EN
	}
	return string(bytes)
}

func resolveLocalizedPromptText(raw string, lang string) string {
	payload := parseLocalizedPromptText(raw)
	if lang == "en" {
		if payload.EN != "" {
			return payload.EN
		}
		return payload.ZH
	}
	if payload.ZH != "" {
		return payload.ZH
	}
	return payload.EN
}

func hasLocalizedPromptText(raw string) bool {
	payload := parseLocalizedPromptText(raw)
	return payload.ZH != "" || payload.EN != ""
}
