package skill

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	SkillFileName        = "SKILL.md"
	MaxNameLength        = 64
	MaxDescriptionLength = 1024
)

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9]+(-[a-zA-Z0-9]+)*$`)

// Skill represents a parsed SKILL.md file.
type Skill struct {
	Name         string   `yaml:"name" json:"name"`
	Description  string   `yaml:"description" json:"description"`
	Paths        []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	Instructions string   `yaml:"-" json:"instructions"`
	Source       string   `yaml:"-" json:"source"` // "user" | "project"
	FilePath     string   `yaml:"-" json:"file_path"`
}

// BaseDir returns the directory containing the skill's SKILL.md, or "" when the
// skill was constructed without a file path (e.g. in tests). The model uses this
// to locate the skill's bundled resources (scripts/, references/, assets/).
func (s *Skill) BaseDir() string {
	if s.FilePath == "" {
		return ""
	}
	return filepath.Dir(s.FilePath)
}

// Validate checks that the skill has required fields with valid values.
func (s *Skill) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	if len(s.Name) > MaxNameLength {
		return fmt.Errorf("skill name exceeds %d characters: %s", MaxNameLength, s.Name)
	}
	if !namePattern.MatchString(s.Name) {
		return fmt.Errorf("skill name must match %s: %s", namePattern.String(), s.Name)
	}
	if len(s.Description) > MaxDescriptionLength {
		return fmt.Errorf("skill description exceeds %d characters", MaxDescriptionLength)
	}
	if strings.TrimSpace(s.Instructions) == "" {
		return fmt.Errorf("skill instructions are required")
	}
	return nil
}

// FormatListing renders all skills as a name+description listing for system prompt.
func FormatListing(skills []*Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<available_skills>\n")
	for _, s := range skills {
		sb.WriteString("  <skill>\n")
		fmt.Fprintf(&sb, "    <name>%s</name>\n", escape(s.Name))
		fmt.Fprintf(&sb, "    <description>%s</description>\n", escape(s.Description))
		sb.WriteString("  </skill>\n")
	}
	sb.WriteString("</available_skills>")
	return sb.String()
}

// FormatInvocation renders a skill's instructions wrapped in <loaded_skill> XML.
// Used when a user triggers a skill via slash command — injected as a user message.
//
// Name and description sit inside XML tags, so they are escaped. Instructions are
// markdown prose (may contain code blocks, <tags>, shell operators) and are injected
// verbatim — escaping them would corrupt the skill's directions.
func FormatInvocation(s *Skill, args string) string {
	var sb strings.Builder
	sb.WriteString("<loaded_skill>\n")
	fmt.Fprintf(&sb, "  <name>%s</name>\n", escape(s.Name))
	fmt.Fprintf(&sb, "  <description>%s</description>\n", escape(s.Description))
	sb.WriteString("  <instructions>\n")
	sb.WriteString(renderBody(s, args))
	sb.WriteString("\n  </instructions>\n")
	sb.WriteString("</loaded_skill>")
	return sb.String()
}

// FormatSkillList renders all skills as a human-readable listing for /skills command.
func FormatSkillList(skills []*Skill) string {
	if len(skills) == 0 {
		return "No skills loaded."
	}
	var sb strings.Builder
	for _, s := range skills {
		sb.WriteString(s.Name)
		if s.Description != "" {
			sb.WriteString(" — ")
			sb.WriteString(s.Description)
		}
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// FormatToolResult renders a skill's instructions as a SkillTool result.
// Used when the model invokes the Skill tool — returned as tool_result.
//
// The skill name sits in an XML attribute and is escaped; the body is markdown
// prose injected verbatim (see renderBody).
func FormatToolResult(s *Skill, args string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<skill name=\"%s\">\n", escape(s.Name))
	sb.WriteString(renderBody(s, args))
	sb.WriteString("\n</skill>")
	return sb.String()
}

// renderBody builds the loaded-skill body shared by FormatInvocation and
// FormatToolResult: an optional base-directory header (so the model can read the
// skill's bundled files under scripts/, references/, assets/), the verbatim
// instructions, and any additional user args.
//
// Instructions and args are NOT HTML-escaped — they are markdown prose that may
// legitimately contain '<', '>', '&', quotes, and code fences.
func renderBody(s *Skill, args string) string {
	var sb strings.Builder
	if dir := s.BaseDir(); dir != "" {
		fmt.Fprintf(&sb, "Base directory for this skill: %s\n\n", dir)
	}
	sb.WriteString(s.Instructions)
	if args != "" {
		fmt.Fprintf(&sb, "\n\n## Additional context from user\n\n%s", args)
	}
	return sb.String()
}

var promptReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"\"", "&quot;",
	"'", "&apos;",
)

func escape(s string) string {
	return promptReplacer.Replace(s)
}
