package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatStableSystemPromptLoadsEmbeddedPrompt(t *testing.T) {
	got := FormatStableSystemPrompt("")

	if strings.TrimSpace(got) == "" {
		t.Fatal("FormatStableSystemPrompt() returned empty prompt")
	}
	if got != strings.TrimSpace(got) {
		t.Fatalf("FormatStableSystemPrompt() should return trimmed prompt, got %q", got)
	}
}

func TestFormatStableSystemPromptExcludesDynamicContext(t *testing.T) {
	got := FormatStableSystemPrompt("")
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

func TestFormatStableSystemPromptContainsAllSections(t *testing.T) {
	got := FormatStableSystemPrompt("")

	expectedSections := []string{
		"# Identity",
		"# Constraints",
		"# Output Style",
		"# Tool Usage",
		"# Runtime Signals",
		"# Safety",
		"# Decision Making",
		"# Meta-Cognition",
	}

	for _, section := range expectedSections {
		if !strings.Contains(got, section) {
			t.Fatalf("missing section %q in system prompt", section)
		}
	}
}

func TestFormatStableSystemPromptOverrideWithSYSTEMmd(t *testing.T) {
	tmpDir := t.TempDir()

	customContent := "# Custom System Prompt\n- Custom rule 1\n- Custom rule 2"
	if err := os.WriteFile(filepath.Join(tmpDir, "SYSTEM.md"), []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	got := FormatStableSystemPrompt(tmpDir)
	if !strings.Contains(got, "Custom rule 1") {
		t.Fatalf("expected SYSTEM.md override content, got %q", got)
	}
	if strings.Contains(got, "# Identity") {
		t.Fatal("should not contain default prompt sections when overridden")
	}
}

func TestFormatStableSystemPromptFallsBackToDefaultWhenNoFile(t *testing.T) {
	tmpDir := t.TempDir()

	got := FormatStableSystemPrompt(tmpDir)
	defaultGot := FormatStableSystemPrompt("")

	if got != defaultGot {
		t.Fatalf("expected default prompt when no override file exists\ngot:      %q\ndefault:  %q", got, defaultGot)
	}
}

func TestFormatStableSystemPromptIgnoresEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "SYSTEM.md"), []byte("   \n\t\n  "), 0644)

	got := FormatStableSystemPrompt(tmpDir)
	defaultGot := FormatStableSystemPrompt("")

	if got != defaultGot {
		t.Fatalf("empty SYSTEM.md should fall back to default")
	}
}
