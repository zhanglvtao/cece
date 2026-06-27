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
		"# Coding Workflow",
		"# Architecture Mindset",
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

func TestFormatStableSystemPromptUsesScenarioBasedOutputStyle(t *testing.T) {
	got := FormatStableSystemPrompt("")

	expected := []string{
		"Match response length to task complexity",
		"tiny status updates can be terse",
		"plans, design choices, verification results, failures, and risk trade-offs",
		"complete enough to be useful",
		"Avoid filler",
	}
	for _, value := range expected {
		if !strings.Contains(got, value) {
			t.Fatalf("missing scenario-based output style phrase %q", value)
		}
	}

	disallowed := []string{
		"Keep text output under 4 lines",
		"One-word answers when possible",
	}
	for _, value := range disallowed {
		if strings.Contains(got, value) {
			t.Fatalf("system prompt should not contain hard brevity rule %q", value)
		}
	}
}

func TestFormatStableSystemPromptContainsArchitectureMindset(t *testing.T) {
	got := FormatStableSystemPrompt("")

	expected := []string{
		"systems architect",
		"whole-system view",
		"ownership boundaries",
		"data flow",
		"long-term maintenance cost",
		"Prefer reusing and extending existing patterns",
		"critical design decisions",
	}

	for _, value := range expected {
		if !strings.Contains(got, value) {
			t.Fatalf("missing architecture mindset phrase %q", value)
		}
	}
}

func TestFormatStableSystemPromptContainsCodingWorkflow(t *testing.T) {
	got := FormatStableSystemPrompt("")

	expected := []string{
		"don't leave work half-done",
		"stopping at the first passing symptom",
		"extract every concrete example",
		"identify the root cause",
		"Verify the original reproduction",
		"diagnose why before switching tactics",
		"Report outcomes faithfully",
	}

	for _, value := range expected {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(value)) {
			t.Fatalf("missing coding workflow phrase %q", value)
		}
	}
}

func TestFormatStableSystemPromptIgnoresRepoSYSTEMmd(t *testing.T) {
	tmpDir := t.TempDir()

	customContent := "# Custom System Prompt\n- Custom rule 1\n- Custom rule 2"
	if err := os.WriteFile(filepath.Join(tmpDir, "SYSTEM.md"), []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	got := FormatStableSystemPrompt(tmpDir)
	defaultGot := FormatStableSystemPrompt("")
	if got != defaultGot {
		t.Fatalf("expected embedded prompt even when repo SYSTEM.md exists\ngot:     %q\ndefault: %q", got, defaultGot)
	}
	if strings.Contains(got, "Custom rule 1") {
		t.Fatalf("repo SYSTEM.md must not override embedded stable prompt: %q", got)
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
