package text

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"only whitespace", "   \n\n   ", ""},
		{"trims lines", "  hello  \n  world  ", "hello\nworld"},
		{"removes empty lines", "a\n\n\nb", "a\nb"},
		{"single line", "  foo  ", "foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.input); got != tt.want {
				t.Errorf("Normalize() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeToCamelCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"single word lowercase", "hello", "hello"},
		{"single word uppercase", "HELLO", "hEllo"},
		{"single word mixed", "helloWorld", "helloWorld"},
		{"two words", "hello world", "helloWorld"},
		{"multiple words", "hello big world", "helloBigworld"},
		{"special characters", "hello--world!!foo", "helloWorldfoo"},
		{"with apostrophe", "it's a test", "itsAtest"},
		{"only special chars", "---!!!", ""},
		{"apostrophe only word", "hello '", "hello"},
		{"numbers in between", "hello123world", "helloWorld"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeToCamelCase(tt.input); got != tt.want {
				t.Errorf("SanitizeToCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
