package api

import (
	"strings"

	"kt-ai-studio/internal/models"
)

func resolveProjectStyleDescription(project models.Project) string {
	return strings.TrimSpace(project.ArtStyle.Description)
}

func appendProjectStylePrompt(prompt string, project models.Project) string {
	base := strings.TrimSpace(prompt)
	stylePrompt := resolveProjectStyleDescription(project)
	if stylePrompt == "" {
		return base
	}
	if base == "" {
		return stylePrompt
	}
	if strings.Contains(base, stylePrompt) {
		return base
	}
	return stylePrompt + "\n" + base
}
