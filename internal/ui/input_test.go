package ui

import "testing"

func TestSanitizePasteContent(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Ctrl+J newlines are restored",
			input: "line1\x1b[27;5;106~line2\x1b[27;5;106~line3",
			want:  "line1\nline2\nline3",
		},
		{
			name:  "Ctrl+M carriage returns are restored",
			input: "line1\x1b[27;5;13~line2",
			want:  "line1\nline2",
		},
		{
			name:  "unknown CSI sequences are stripped",
			input: "hello\x1b[1;2Aworld",
			want:  "helloworld",
		},
		{
			name:  "normal text unchanged",
			input: "hello world\nsecond line",
			want:  "hello world\nsecond line",
		},
		{
			name:  "mixed CSI and normal newlines",
			input: "a\x1b[27;5;106~b\nc\x1b[27;5;106~d",
			want:  "a\nb\nc\nd",
		},
		{
			name:  "F5 sequence stripped",
			input: "text\x1b[15~more",
			want:  "textmore",
		},
		{
			name:  "multiple modifiers",
			input: "a\x1b[27;5;106~b\x1b[27;2;65~c",
			want:  "a\nbc",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizePasteContent(tc.input)
			if got != tc.want {
				t.Errorf("sanitizePasteContent(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
