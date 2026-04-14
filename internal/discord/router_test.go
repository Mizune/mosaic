package discord

import "testing"

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"preserves newlines", "line1\nline2", "line1\nline2"},
		{"preserves tabs", "col1\tcol2", "col1\tcol2"},
		{"strips null bytes", "hello\x00world", "helloworld"},
		{"strips escape", "hello\x1b[31mred", "hello[31mred"},
		{"strips bell", "alert\x07here", "alerthere"},
		{"strips carriage return", "over\rwrite", "overwrite"},
		{"empty string", "", ""},
		{"unicode preserved", "日本語テスト", "日本語テスト"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeInput(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeInput(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
