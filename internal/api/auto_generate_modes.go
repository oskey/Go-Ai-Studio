package api

import "strings"

const (
	AutoGenerateModeNarration   = "narration"
	AutoGenerateModeHighQuality = "high_quality"
	AutoGenerateModeStoryboard  = "storyboard"
	AutoGenerateModeH3Short     = "h3_short"
)

func normalizeAutoGenerateGenerationMode(raw string, allowCharacterSpeech bool) string {
	switch strings.TrimSpace(raw) {
	case AutoGenerateModeNarration:
		return AutoGenerateModeNarration
	case AutoGenerateModeHighQuality:
		return AutoGenerateModeHighQuality
	case AutoGenerateModeStoryboard:
		return AutoGenerateModeStoryboard
	case AutoGenerateModeH3Short:
		return AutoGenerateModeH3Short
	case "r2v":
		return AutoGenerateModeH3Short
	}
	if allowCharacterSpeech {
		return AutoGenerateModeHighQuality
	}
	return AutoGenerateModeNarration
}

func autoGenerateModeAllowsCharacterSpeech(mode string) bool {
	switch normalizeAutoGenerateGenerationMode(mode, false) {
	case AutoGenerateModeHighQuality, AutoGenerateModeStoryboard, AutoGenerateModeH3Short:
		return true
	default:
		return false
	}
}

func autoGenerateModeRequiresEmptyNarration(mode string) bool {
	_ = mode
	return false
}

func autoGenerateModeUsesFlowingVideoPrompt(mode string) bool {
	switch normalizeAutoGenerateGenerationMode(mode, false) {
	case AutoGenerateModeHighQuality, AutoGenerateModeStoryboard, AutoGenerateModeH3Short:
		return true
	default:
		return false
	}
}

func inferGenerationModeFromPayload(payload *lightweightStoryResponse) string {
	if payload == nil {
		return AutoGenerateModeNarration
	}
	hasFlowingVideoPrompt := false
	for _, scene := range payload.Scenes {
		if strings.TrimSpace(scene.VideoPrompt) != "" {
			if _, err := parseFlowingVideoPrompt(scene.VideoPrompt); err == nil {
				hasFlowingVideoPrompt = true
			}
		}
	}
	if hasFlowingVideoPrompt {
		return AutoGenerateModeHighQuality
	}
	for _, scene := range payload.Scenes {
		if strings.HasPrefix(strings.TrimSpace(scene.VideoPrompt), "integrated_multimodal_description:") {
			return AutoGenerateModeH3Short
		}
	}
	return AutoGenerateModeNarration
}
