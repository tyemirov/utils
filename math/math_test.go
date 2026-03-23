package math

import (
	"errors"
	"io"
	"testing"
)

func TestMin(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{3, 3, 3},
		{-1, 0, -1},
	}
	for _, tt := range tests {
		if got := Min(tt.a, tt.b); got != tt.want {
			t.Errorf("Min(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestMax(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{1, 2, 2},
		{2, 1, 2},
		{3, 3, 3},
		{-1, 0, 0},
	}
	for _, tt := range tests {
		if got := Max(tt.a, tt.b); got != tt.want {
			t.Errorf("Max(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	tests := []struct {
		name string
		num  *float64
		want string
	}{
		{"nil", nil, ""},
		{"zero", f(0), "0"},
		{"whole", f(42), "42"},
		{"decimal", f(3.14), "3.14"},
		{"trailing zeros", f(1.50), "1.5"},
		{"negative whole", f(-7), "-7"},
		{"negative decimal", f(-0.123), "-0.123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatNumber(tt.num); got != tt.want {
				t.Errorf("FormatNumber() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChanceOfBoundaries(t *testing.T) {
	if ChanceOf(0) {
		t.Error("ChanceOf(0) should always return false")
	}
	if ChanceOf(-1) {
		t.Error("ChanceOf(-1) should always return false")
	}
	if !ChanceOf(1) {
		t.Error("ChanceOf(1) should always return true")
	}
	if !ChanceOf(2) {
		t.Error("ChanceOf(2) should always return true")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("rand failure")
}

func TestChanceOfRandError(t *testing.T) {
	original := randReader
	randReader = errReader{}
	defer func() { randReader = original }()

	if ChanceOf(0.5) {
		t.Error("ChanceOf should return false when rand fails")
	}
}

type fixedReader struct{ val byte }

func (r fixedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = r.val
	return 1, nil
}

var _ io.Reader = fixedReader{}

func TestChanceOfMidRange(t *testing.T) {
	// Run 1000 trials at 50% to check it returns both true and false
	trueCount := 0
	for i := 0; i < 1000; i++ {
		if ChanceOf(0.5) {
			trueCount++
		}
	}
	if trueCount == 0 || trueCount == 1000 {
		t.Errorf("ChanceOf(0.5) over 1000 trials returned %d trues, expected a mix", trueCount)
	}
}
