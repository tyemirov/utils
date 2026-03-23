// Package text provides utilities for normalising and sanitising strings that
// are commonly needed when generating text or HTML content.
package text

import (
	"regexp"
	"strings"
)

// Normalize removes excess whitespace from the provided string. Each line is
// trimmed and empty lines are removed to avoid Markdown formatting
// mismatches.
func Normalize(input string) string {
	lines := strings.Split(input, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" { // Skip empty lines
			cleaned = append(cleaned, trimmed)
		}
	}
	return strings.Join(cleaned, "\n")
}

// SanitizeToCamelCase sanitizes a string and converts it to camel case for use
// as an HTML ID.
func SanitizeToCamelCase(input string) string {
	// Use regex to split by characters that are not letters or apostrophes.
	reg := regexp.MustCompile(`[^a-zA-Z']+`)
	rawWords := reg.Split(input, -1)

	var cleanedWords []string
	// Remove apostrophes from within words and filter out empties.
	for _, word := range rawWords {
		if word != "" {
			word = strings.ReplaceAll(word, "'", "")
			if word != "" {
				cleanedWords = append(cleanedWords, word)
			}
		}
	}

	if len(cleanedWords) == 0 {
		return ""
	}

	// Single-word scenario
	if len(cleanedWords) == 1 {
		word := cleanedWords[0]
		// Ensure first letter is lowercase
		word = strings.ToLower(word[:1]) + word[1:]

		// Find the first uppercase letter after the first character
		firstUpperIndex := -1
		for idx, ch := range word[1:] {
			if ch >= 'A' && ch <= 'Z' {
				firstUpperIndex = idx + 1 // index relative to whole string
				break
			}
		}

		// If no uppercase found after first char, return fully lowercased word
		if firstUpperIndex == -1 {
			return strings.ToLower(word)
		}

		before := word[:firstUpperIndex]
		firstUpper := string(word[firstUpperIndex])
		after := strings.ToLower(word[firstUpperIndex+1:])
		return before + firstUpper + after
	}

	// Multiple words scenario
	var result []string
	// Lowercase the entire first word
	result = append(result, strings.ToLower(cleanedWords[0]))

	uppercaseUsed := false
	for i := 1; i < len(cleanedWords); i++ {
		word := cleanedWords[i]
		if !uppercaseUsed {
			// For the first subsequent word, uppercase its first letter
			// and lowercase the rest
			firstChar := strings.ToUpper(word[:1])
			rest := strings.ToLower(word[1:])
			result = append(result, firstChar+rest)
			uppercaseUsed = true
		} else {
			// For any additional words, lowercase the entire word
			result = append(result, strings.ToLower(word))
		}
	}

	return strings.Join(result, "")
}
