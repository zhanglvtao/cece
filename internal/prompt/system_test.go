package prompt

import (
	"strings"
	"testing"
)

func TestFormatStableSystemPromptLoadsEmbeddedPrompt(t *testing.T) {
	got := FormatStableSystemPrompt()

	if strings.TrimSpace(got) == "" {
		t.Fatal("FormatStableSystemPrompt() returned empty prompt")
	}
	if got != strings.TrimSpace(got) {
		t.Fatalf("FormatStableSystemPrompt() should return trimmed prompt, got %q", got)
	}
}

func TestFormatStableSystemPromptExcludesDynamicContext(t *testing.T) {
	got := FormatStableSystemPrompt()
	disallowed := []string{
		"current_time",
		"current_date",
		"current_working_directory",
		"current_branch",
		"repo_root",
		"session_start_branch",
	}

	for _, value := range disallowed {
		if strings.Contains(got, value) {
			t.Fatalf("FormatStableSystemPrompt() contains dynamic field %q: %q", value, got)
		}
	}
}
