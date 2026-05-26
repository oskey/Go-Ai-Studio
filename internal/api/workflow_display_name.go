package api

import (
	"path/filepath"
	"strings"
)

func workflowDisplayNameFromPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(trimmed), filepath.Ext(trimmed))
}
