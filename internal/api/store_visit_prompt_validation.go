package api

import (
	"fmt"
	"strings"
)

type storeVisitVideoPromptInferResponse struct {
	VideoPositivePrompt string `json:"video_positive_prompt"`
	DurationSeconds     int    `json:"duration_seconds"`
}

func validateStoreVisitVideoPromptInferResponse(payload *storeVisitVideoPromptInferResponse) error {
	if payload == nil {
		return fmt.Errorf("infer payload is nil")
	}
	if payload.VideoPositivePrompt == "" {
		return fmt.Errorf("video_positive_prompt is required")
	}
	if strings.Contains(payload.VideoPositivePrompt, `"`) {
		return fmt.Errorf("video_positive_prompt must not contain ASCII double quotes")
	}
	if payload.DurationSeconds < minVideoTotalDurationSeconds || payload.DurationSeconds > maxVideoTotalDurationSeconds {
		return fmt.Errorf("duration_seconds must be between %d and %d", minVideoTotalDurationSeconds, maxVideoTotalDurationSeconds)
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

func validateStoreVisitAudioSection(audio string) error {
	trimmed := strings.TrimSpace(audio)
	if trimmed == "" {
		return fmt.Errorf("Audio section must not be empty")
	}
	if strings.Contains(trimmed, "「") || strings.Contains(trimmed, "」") {
		return fmt.Errorf("Audio section must not contain dialogue")
	}
	sentenceMarks := 0
	for _, marker := range []string{"。", "！", "？", "!", "?"} {
		sentenceMarks += strings.Count(trimmed, marker)
	}
	if sentenceMarks >= 2 {
		return fmt.Errorf("Audio section must describe background or environment sounds only")
	}
	return nil
}
