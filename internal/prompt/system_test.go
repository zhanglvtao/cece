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
		"# How the System Works",
		"# Constraints",
		"# Architecture Mindset",
		"# Output Style",
		"# Safety",
		"# Decision Making",
		"# Autonomy",
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

func TestFormatStableSystemPromptContainsHowTheSystemWorks(t *testing.T) {
	got := FormatStableSystemPrompt("")

	expected := []string{
		"# How the System Works",
		"Permission Modes",
		"default",
		"auto-accept",
		"require_confirmation",
		"Core tools",
		"Mode tools",
		"Context tools",
		"Auto-Compression",
		"system-reminder",
	}

	for _, value := range expected {
		if !strings.Contains(got, value) {
			t.Fatalf("missing How the System Works phrase %q", value)
		}
	}
}

func TestFormatStableSystemPromptContainsBugFixWorkflow(t *testing.T) {
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
			t.Fatalf("missing bug fix workflow phrase %q", value)
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

func TestFormatInteractiveSystemPromptIncludesBuiltInAgentGuidance(t *testing.T) {
	prompt := FormatInteractiveSystemPrompt("/repo")
	for _, want := range []string{
		"built-in agents",
		"explore",
		"coding",
		"review",
		"execution",
		"independent subtasks",
		"agent_type",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("interactive prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestFormatSubAgentSystemPromptIncludesProfileGuidance(t *testing.T) {
	cases := []struct {
		profile string
		want    string
	}{
		{profile: "explore", want: "collect evidence before concluding"},
		{profile: "coding", want: "keep code changes focused"},
		{profile: "review", want: "inspect for risks and omissions"},
		{profile: "execution", want: "drive progress and report status"},
	}

	for _, tc := range cases {
		prompt := FormatSubAgentSystemPrompt("/repo", tc.profile, "")
		if !strings.Contains(strings.ToLower(prompt), strings.ToLower(tc.want)) {
			t.Fatalf("profile %s missing %q:\n%s", tc.profile, tc.want, prompt)
		}
	}
}

func TestFormatSubAgentSystemPromptKeepsBaseAndExtraInstructions(t *testing.T) {
	base := FormatStableSystemPrompt("/repo")
	prompt := FormatSubAgentSystemPrompt("/repo", "coding", "follow repo conventions")

	if !strings.Contains(prompt, strings.Split(base, "\n")[0]) {
		t.Fatalf("sub-agent prompt should keep stable base:\n%s", prompt)
	}
	if !strings.Contains(prompt, "follow repo conventions") {
		t.Fatalf("sub-agent prompt missing extra instructions:\n%s", prompt)
	}
}
