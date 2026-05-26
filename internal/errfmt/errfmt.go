package errfmt

import (
	"regexp"
	"strings"
)

var whitespaceRe = regexp.MustCompile(`\s+`)

func NormalizeUserFacingError(message string, maxLen int) string {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return ""
	}
	msg = strings.ReplaceAll(msg, "\r\n", " ")
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = whitespaceRe.ReplaceAllString(msg, " ")

	lower := strings.ToLower(msg)
	if looksLikeHTMLPage(lower) {
		return classifyHTMLGatewayError(lower)
	}

	if idx := strings.Index(strings.ToLower(msg), "body: <!doctype html"); idx >= 0 {
		prefix := strings.TrimSpace(msg[:idx])
		if prefix == "" {
			prefix = "上游返回 HTML 网关页"
		}
		msg = prefix + "；返回内容不是 JSON"
	}

	if maxLen > 0 && len([]rune(msg)) > maxLen {
		runes := []rune(msg)
		msg = string(runes[:maxLen]) + "..."
	}
	return msg
}

func looksLikeHTMLPage(lower string) bool {
	trimmed := strings.TrimSpace(lower)
	return strings.HasPrefix(trimmed, "<!doctype html") ||
		strings.HasPrefix(trimmed, "<html") ||
		strings.Contains(trimmed, "<title>") ||
		(strings.Contains(trimmed, "cloudflare") && strings.Contains(trimmed, "<body"))
}

func classifyHTMLGatewayError(lower string) string {
	switch {
	case strings.Contains(lower, "524") || strings.Contains(lower, "timeout occurred"):
		return "上游网关超时（524），返回了 HTML 网页而不是 JSON"
	case strings.Contains(lower, "502") || strings.Contains(lower, "bad gateway"):
		return "上游网关错误（502），返回了 HTML 网页而不是 JSON"
	case strings.Contains(lower, "503") || strings.Contains(lower, "service unavailable"):
		return "上游服务暂时不可用（503），返回了 HTML 网页而不是 JSON"
	default:
		return "上游返回了 HTML 网页而不是 JSON，通常表示代理网关或服务异常"
	}
}
