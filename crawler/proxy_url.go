package crawler

import "strings"

// SanitizeProxyURL strips credentials from a proxy URL for safe logging.
func SanitizeProxyURL(rawProxyURL string) string {
	normalized := strings.TrimSpace(rawProxyURL)
	if normalized == "" {
		return ""
	}
	schemeSep := strings.Index(normalized, "://")
	if schemeSep < 0 {
		return normalized
	}
	authStart := schemeSep + len("://")
	authEnd := len(normalized)
	tail := normalized[authStart:]
	if idx := strings.Index(tail, "/"); idx >= 0 && authStart+idx < authEnd {
		authEnd = authStart + idx
	}
	if idx := strings.Index(tail, "?"); idx >= 0 && authStart+idx < authEnd {
		authEnd = authStart + idx
	}
	if idx := strings.Index(tail, "#"); idx >= 0 && authStart+idx < authEnd {
		authEnd = authStart + idx
	}
	authority := normalized[authStart:authEnd]
	userInfoSep := strings.LastIndex(authority, "@")
	if userInfoSep < 0 {
		return normalized
	}
	sanitized := authority[userInfoSep+1:]
	if strings.TrimSpace(sanitized) == "" {
		return normalized
	}
	return normalized[:authStart] + sanitized + normalized[authEnd:]
}

// DescribeProxyForLog returns a safe-to-log proxy description.
func DescribeProxyForLog(rawProxyURL string) string {
	sanitized := SanitizeProxyURL(rawProxyURL)
	if sanitized == "" {
		return "direct"
	}
	return sanitized
}
