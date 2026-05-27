package skill

import (
	"bytes"
	"fmt"
	"strings"
)

// ParseContent parses a SKILL.md file's content into a Skill.
// The file format is YAML frontmatter delimited by --- followed by markdown instructions.
func ParseContent(data []byte) (*Skill, error) {
	content := string(data)
	frontmatter, body, ok := splitFrontmatter(content)
	if !ok {
		return nil, fmt.Errorf("no frontmatter found: expected --- delimiter")
	}

	skill := &Skill{
		Instructions: strings.TrimSpace(body),
	}

	if err := parseFrontmatter(frontmatter, skill); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	return skill, nil
}

// splitFrontmatter splits content into frontmatter and body.
// Returns ("", "", false) if no valid frontmatter is found.
func splitFrontmatter(content string) (frontmatter, body string, ok bool) {
	trimmed := strings.TrimLeft(content, " \t\n\r")
	if !strings.HasPrefix(trimmed, "---") {
		return "", "", false
	}

	// Find the closing ---
	afterFirst := trimmed[3:]
	// Skip newline after opening ---
	afterFirst = strings.TrimLeft(afterFirst, "\r\n")

	closingIdx := strings.Index(afterFirst, "\n---")
	if closingIdx < 0 {
		return "", "", false
	}

	frontmatter = afterFirst[:closingIdx]
	// Skip the closing --- and following whitespace
	bodyStart := closingIdx + 4 // len("\n---")
	body = afterFirst[bodyStart:]
	body = strings.TrimLeft(body, "\r\n")

	return frontmatter, body, true
}

// parseFrontmatter parses simple YAML key-value pairs into a Skill.
// Only supports flat string/bool/array-of-string values.
func parseFrontmatter(text string, skill *Skill) error {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := splitKeyValue(line)
		if !ok {
			continue
		}

		switch key {
		case "name":
			skill.Name = unquote(value)
		case "description":
			skill.Description = unquote(value)
		case "paths":
			// Single-line array like ["*.go", "*.ts"]
			skill.Paths = parseStringArray(value)
		}
	}
	return nil
}

func splitKeyValue(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	return key, value, true
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func parseBool(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return lower == "true" || lower == "yes"
}

func parseStringArray(s string) []string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil
	}
	inner := s[1 : len(s)-1]
	if strings.TrimSpace(inner) == "" {
		return nil
	}
	parts := bytes.Split([]byte(inner), []byte(","))
	var result []string
	for _, p := range parts {
		clean := strings.TrimSpace(string(p))
		clean = unquote(clean)
		if clean != "" {
			result = append(result, clean)
		}
	}
	return result
}
