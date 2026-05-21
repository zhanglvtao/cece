package prompt

import (
	"fmt"
	"strings"

	"cece/internal/tool"
)

func FormatSessionContext(ctx SessionContext) string {
	var parts []string

	// Environment section
	var envLines []string
	envLines = append(envLines, "<environment>")
	if ctx.RepoRoot != "" {
		envLines = append(envLines, "repo_root: "+ctx.RepoRoot)
	}
	if ctx.IsGitRepo {
		envLines = append(envLines, "is_git_repo: true")
	}
	if ctx.OSName != "" {
		envLines = append(envLines, "os_name: "+ctx.OSName)
	}
	if ctx.OSVersion != "" {
		envLines = append(envLines, "os_version: "+ctx.OSVersion)
	}
	if ctx.SessionStartBranch != "" {
		envLines = append(envLines, "session_start_branch: "+ctx.SessionStartBranch)
	}
	if ctx.ModelName != "" {
		envLines = append(envLines, "model: "+ctx.ModelName)
	}
	envLines = append(envLines, "</environment>")
	parts = append(parts, strings.Join(envLines, "\n"))

	// Project instructions section
	if ctx.CLAUDEmd != "" {
		parts = append(parts, FormatProjectInstructions(ctx.CLAUDEmd))
	}

	// Tool descriptions section
	if ctx.ToolDescriptions != "" {
		parts = append(parts, FormatToolDescriptionsTextToXml(ctx.ToolDescriptions))
	}

	return strings.Join(parts, "\n\n")
}

func FormatProjectInstructions(content string) string {
	return "<project_instructions>\n" + content + "\n</project_instructions>"
}

// FormatToolDescriptionsTextToXml wraps pre-rendered tool description text in a tag.
func FormatToolDescriptionsTextToXml(descriptions string) string {
	return "<available_tools>\n" + descriptions + "\n</available_tools>"
}

// FormatToolDescriptionsText generates a human-readable summary from tool definitions.
// Each tool is rendered as "Name: Description" on its own line.
func FormatToolDescriptionsText(defs []tool.Definition) string {
	if len(defs) == 0 {
		return ""
	}
	var lines []string
	for _, d := range defs {
		lines = append(lines, fmt.Sprintf("%s: %s", d.Name, d.Description))
	}
	return strings.Join(lines, "\n")
}

func FormatTurnContext(ctx TurnContext) string {
	var lines []string
	lines = append(lines, "<turn_context>")
	if ctx.IncludeTime && !ctx.Now.IsZero() {
		lines = append(lines, "current_date: "+ctx.Now.Format("2006-01-02"))
		lines = append(lines, "current_time: "+ctx.Now.Format("2006-01-02T15:04:05Z07:00"))
	}
	if ctx.CurrentWorkingDirectory != "" {
		lines = append(lines, "current_working_directory: "+ctx.CurrentWorkingDirectory)
	}
	if ctx.CurrentBranch != "" {
		lines = append(lines, "current_branch: "+ctx.CurrentBranch)
	}
	if ctx.Mode != "" {
		lines = append(lines, "mode: "+ctx.Mode)
	}
	if ctx.ConversationTurnNumber > 0 {
		lines = append(lines, fmt.Sprintf("conversation_turn: %d", ctx.ConversationTurnNumber))
	}
	lines = append(lines, "</turn_context>")
	return strings.Join(lines, "\n")
}

func ShouldInjectTime(input string) bool {
	text := strings.ToLower(input)
	chineseKeywords := []string{
		"现在",
		"当前时间",
		"今天",
		"昨天",
		"明天",
		"最近",
		"本周",
		"下周",
		"截止",
		"过期",
	}
	for _, keyword := range chineseKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}

	englishKeywords := map[string]bool{
		"expired":   true,
		"cron":      true,
		"schedule":  true,
		"timestamp": true,
		"date":      true,
		"time":      true,
		"timezone":  true,
	}
	for _, token := range strings.FieldsFunc(text, isTokenSeparator) {
		if englishKeywords[token] {
			return true
		}
	}
	return false
}

func isTokenSeparator(r rune) bool {
	return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
}
