package test

import (
	stdlibmath "math"
	"testing"

	"github.com/tyemirov/utils/math"
	"github.com/tyemirov/utils/pointers"
)

// TestFormatNumber is a table-driven test for the FormatNumber function.
func TestFormatNumber(t *testing.T) {
	negativeZero := stdlibmath.Copysign(0, -1)
	tests := []struct {
		name     string
		input    *float64
		expected string
	}{
		{"Whole number", pointers.FromFloat(4.0), "4"},
		{"Whole number with trailing zeros", pointers.FromFloat(5.000), "5"},
		{"Decimal number", pointers.FromFloat(4.5), "4.5"},
		{"Decimal number with trailing zeros", pointers.FromFloat(4.500), "4.5"},
		{"Decimal number with multiple decimal places", pointers.FromFloat(4.657), "4.657"},
		{"Negative whole number", pointers.FromFloat(-3.0), "-3"},
		{"Negative decimal number", pointers.FromFloat(-3.14), "-3.14"},
		{"Zero", pointers.FromFloat(0.0), "0"},
		{"Negative zero", pointers.FromFloat(negativeZero), "0"},
		{"Large whole number", pointers.FromFloat(123456789.0), "123456789"},
		{"Null pointer (nil)", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := math.FormatNumber(tt.input)
			if result != tt.expected {
				t.Errorf("FormatNumber(%v) = %v; expected %v", tt.input, result, tt.expected)
			}
		})
	}
}
