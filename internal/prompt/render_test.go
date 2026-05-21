package prompt

import (
	"strings"
	"testing"
	"time"

	"cece/internal/tool"
)

func TestFormatSessionContextBasicFields(t *testing.T) {
	ctx := SessionContext{
		RepoRoot:           "/repo",
		IsGitRepo:          true,
		OSName:             "darwin",
		OSVersion:          "25.0.0",
		SessionStartBranch: "main",
		ModelName:          "claude-sonnet-4-6",
	}

	got := FormatSessionContext(ctx)

	// Must contain environment section
	if !strings.Contains(got, "<environment>") || !strings.Contains(got, "</environment>") {
		t.Fatalf("FormatSessionContext() missing environment tags: %q", got)
	}
	if !strings.Contains(got, "repo_root: /repo") {
		t.Fatalf("FormatSessionContext() missing repo_root: %q", got)
	}
	if !strings.Contains(got, "model: claude-sonnet-4-6") {
		t.Fatalf("FormatSessionContext() missing model: %q", got)
	}
}

func TestFormatSessionContextOmitsMissingFields(t *testing.T) {
	ctx := SessionContext{
		RepoRoot:  "/repo",
		IsGitRepo: true,
	}

	got := FormatSessionContext(ctx)

	if strings.Contains(got, "os_name:") {
		t.Fatalf("FormatSessionContext() should omit empty os_name: %q", got)
	}
	if strings.Contains(got, "model:") {
		t.Fatalf("FormatSessionContext() should omit empty model: %q", got)
	}
}

func TestFormatSessionContextIncludesProjectInstructions(t *testing.T) {
	ctx := SessionContext{
		CLAUDEmd: "always use Chinese",
	}

	got := FormatSessionContext(ctx)
	if !strings.Contains(got, "<project_instructions>") {
		t.Fatalf("FormatSessionContext() missing project_instructions tag: %q", got)
	}
	if !strings.Contains(got, "always use Chinese") {
		t.Fatalf("FormatSessionContext() missing CLAUDEmd content: %q", got)
	}
}

func TestFormatSessionContextIncludesToolDescriptions(t *testing.T) {
	ctx := SessionContext{
		ToolDescriptions: "bash: execute shell commands",
	}

	got := FormatSessionContext(ctx)
	if !strings.Contains(got, "<available_tools>") {
		t.Fatalf("FormatSessionContext() missing available_tools tag: %q", got)
	}
	if !strings.Contains(got, "bash: execute shell commands") {
		t.Fatalf("FormatSessionContext() missing tool descriptions content: %q", got)
	}
}

func TestFormatProjectInstructions(t *testing.T) {
	got := FormatProjectInstructions("use go fmt")
	want := "<project_instructions>\nuse go fmt\n</project_instructions>"
	if got != want {
		t.Fatalf("FormatProjectInstructions() = %q, want %q", got, want)
	}
}

func TestFormatToolDescriptionsTextToXml(t *testing.T) {
	got := FormatToolDescriptionsTextToXml("bash: run commands")
	want := "<available_tools>\nbash: run commands\n</available_tools>"
	if got != want {
		t.Fatalf("FormatToolDescriptionsTextToXml() = %q, want %q", got, want)
	}
}

func TestFormatTurnContextOmitsTimeByDefault(t *testing.T) {
	ctx := TurnContext{
		CurrentWorkingDirectory: "/repo",
		CurrentBranch:           "main",
		Mode:                    "interactive",
	}

	got := FormatTurnContext(ctx)
	want := strings.Join([]string{
		"<turn_context>",
		"current_working_directory: /repo",
		"current_branch: main",
		"mode: interactive",
		"</turn_context>",
	}, "\n")

	if got != want {
		t.Fatalf("FormatTurnContext() = %q, want %q", got, want)
	}
	if strings.Contains(got, "current_time") || strings.Contains(got, "current_date") {
		t.Fatalf("FormatTurnContext() should not include time by default: %q", got)
	}
}

func TestFormatTurnContextIncludesTimeWhenRequested(t *testing.T) {
	now := time.Date(2026, 5, 21, 1, 23, 45, 0, time.FixedZone("CST", 8*60*60))
	ctx := TurnContext{
		IncludeTime:             true,
		Now:                     now,
		CurrentWorkingDirectory: "/repo",
		CurrentBranch:           "main",
		Mode:                    "interactive",
	}

	got := FormatTurnContext(ctx)
	want := strings.Join([]string{
		"<turn_context>",
		"current_date: 2026-05-21",
		"current_time: 2026-05-21T01:23:45+08:00",
		"current_working_directory: /repo",
		"current_branch: main",
		"mode: interactive",
		"</turn_context>",
	}, "\n")

	if got != want {
		t.Fatalf("FormatTurnContext() = %q, want %q", got, want)
	}
}

func TestFormatTurnContextIncludesTurnNumber(t *testing.T) {
	ctx := TurnContext{
		CurrentWorkingDirectory: "/repo",
		Mode:                    "interactive",
		ConversationTurnNumber:  3,
	}

	got := FormatTurnContext(ctx)
	if !strings.Contains(got, "conversation_turn: 3") {
		t.Fatalf("FormatTurnContext() missing conversation_turn: %q", got)
	}
}

func TestShouldInjectTimeForExplicitTimeRequests(t *testing.T) {
	cases := []string{
		"今天是周几？",
		"这个 cron 明天会执行吗？",
		"check whether this token is expired now",
		"parse this timestamp",
	}

	for _, input := range cases {
		if !ShouldInjectTime(input) {
			t.Fatalf("ShouldInjectTime(%q) = false, want true", input)
		}
	}
}

func TestFormatToolDescriptionsTextFromDefs(t *testing.T) {
	defs := []tool.Definition{
		{Name: "Bash", Description: "execute shell commands", InputSchema: map[string]any{"type": "object"}},
		{Name: "Read", Description: "read a file", InputSchema: map[string]any{"type": "object"}},
	}

	got := FormatToolDescriptionsText(defs)

	if !strings.Contains(got, "Bash") || !strings.Contains(got, "execute shell commands") {
		t.Fatalf("FormatToolDescriptionsText() missing Bash entry: %q", got)
	}
	if !strings.Contains(got, "Read") || !strings.Contains(got, "read a file") {
		t.Fatalf("FormatToolDescriptionsText() missing Read entry: %q", got)
	}
}

func TestFormatToolDescriptionsTextEmptyDefs(t *testing.T) {
	got := FormatToolDescriptionsText(nil)
	if got != "" {
		t.Fatalf("FormatToolDescriptionsText(nil) = %q, want empty", got)
	}
}

func TestShouldInjectTimeSkipsNormalCodeRequests(t *testing.T) {
	cases := []string{
		"解释这段代码",
		"修一下这个 bug",
		"帮我重构 runtime",
		"读取当前分支上的文件",
	}

	for _, input := range cases {
		if ShouldInjectTime(input) {
			t.Fatalf("ShouldInjectTime(%q) = true, want false", input)
		}
	}
}
