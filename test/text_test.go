package test

import (
	"testing"

	"github.com/tyemirov/utils/text"
)

// TestSanitizeToCamelCase tests the SanitizeToCamelCase function
func TestSanitizeToCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Title's length", "titlesLength"},           // Regular input with special character
		{"simple test", "simpleTest"},                // Input with space
		{"alreadyCamelCase", "alreadyCamelcase"},     // Input already in camel case
		{"MixedUPPERandlower", "mixedUpperandlower"}, // Mixed upper and lower cases
		{"123 numbers and text", "numbersAndtext"},   // Input with numbers
		{"special#characters!", "specialCharacters"}, // Input with special characters
		{"   leading spaces", "leadingSpaces"},       // Input with leading spaces
		{"trailing spaces   ", "trailingSpaces"},     // Input with trailing spaces
		{"", ""},                                     // Empty input
		{"singleWord", "singleWord"},                 // Single word
		{"nonAlpha-123!@#", "nonAlpha"},              // Non-alphanumeric characters
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := text.SanitizeToCamelCase(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeToCamelCase(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
