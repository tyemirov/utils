package crawler

import "strings"

func sanitizeProxyURL(rawProxyURL string) string {
	normalizedProxyURL := strings.TrimSpace(rawProxyURL)
	if normalizedProxyURL == "" {
		return ""
	}
	schemeSeparatorIndex := strings.Index(normalizedProxyURL, "://")
	if schemeSeparatorIndex < 0 {
		return normalizedProxyURL
	}
	authorityStartIndex := schemeSeparatorIndex + len("://")
	authorityEndIndex := len(normalizedProxyURL)
	authorityAndTail := normalizedProxyURL[authorityStartIndex:]
	pathStartIndex := strings.Index(authorityAndTail, "/")
	if pathStartIndex >= 0 && authorityStartIndex+pathStartIndex < authorityEndIndex {
		authorityEndIndex = authorityStartIndex + pathStartIndex
	}
	queryStartIndex := strings.Index(authorityAndTail, "?")
	if queryStartIndex >= 0 && authorityStartIndex+queryStartIndex < authorityEndIndex {
		authorityEndIndex = authorityStartIndex + queryStartIndex
	}
	fragmentStartIndex := strings.Index(authorityAndTail, "#")
	if fragmentStartIndex >= 0 && authorityStartIndex+fragmentStartIndex < authorityEndIndex {
		authorityEndIndex = authorityStartIndex + fragmentStartIndex
	}
	authority := normalizedProxyURL[authorityStartIndex:authorityEndIndex]
	userInfoSeparatorIndex := strings.LastIndex(authority, "@")
	if userInfoSeparatorIndex < 0 {
		return normalizedProxyURL
	}
	sanitizedAuthority := authority[userInfoSeparatorIndex+1:]
	if strings.TrimSpace(sanitizedAuthority) == "" {
		return normalizedProxyURL
	}
	return normalizedProxyURL[:authorityStartIndex] + sanitizedAuthority + normalizedProxyURL[authorityEndIndex:]
}

func describeProxyForLog(rawProxyURL string) string {
	sanitizedProxyURL := sanitizeProxyURL(rawProxyURL)
	if sanitizedProxyURL == "" {
		return "direct"
	}
	return sanitizedProxyURL
}
