// Package math contains helpers for basic numeric calculations and
// probability-based utilities.
package math

import (
	"crypto/rand"
	"fmt"
	"math"
	"strings"
)

// Min returns the smaller of the two provided integers.
func Min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// Max returns the larger of the two provided integers.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// FormatNumber converts a floating point number to a human friendly string. It
// removes trailing zeros and omits a decimal point for whole numbers. A nil
// pointer results in an empty string.
func FormatNumber(num *float64) string {
	if num == nil {
		return ""
	}
	value := *num
	if value == 0 {
		return "0"
	}
	if value == math.Trunc(value) {
		return fmt.Sprintf("%.0f", value) // Whole number: no decimal places
	}
	// Convert to string with a large precision to avoid scientific notation
	formatted := fmt.Sprintf("%.15f", value)
	formatted = strings.TrimRight(formatted, "0") // Remove trailing zeros
	formatted = strings.TrimRight(formatted, ".") // Remove trailing dot if no decimals left
	return formatted
}

// ChanceOf returns true with the given probability (0.0 to 1.0).
// It uses crypto/rand to ensure a uniform distribution.
func ChanceOf(probability float64) bool {
	if probability <= 0 {
		return false
	}
	if probability >= 1 {
		return true
	}

	// Generate random byte (0-255)
	randomBytes := make([]byte, 1)
	// Fill the buffer with random bytes
	_, err := rand.Read(randomBytes)
	if err != nil {
		fmt.Println("Error reading random bytes:", err)
		return false
	}

	// Convert probability to 0-255 range
	threshold := uint8(probability * 255)

	return randomBytes[0] < threshold
}
