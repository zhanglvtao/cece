package skill

import (
	"strings"
	"testing"
)

func TestStoreListingOnlyEnabled(t *testing.T) {
	skills := []*Skill{
		{Name: "brainstorming", Description: "brainstorm desc", Instructions: "inst1"},
		{Name: "diagnose", Description: "diagnose desc", Instructions: "inst2"},
		{Name: "review", Description: "review desc", Instructions: "inst3"},
	}
	store := NewStore(skills)
	store.SetEnabled([]string{"brainstorming"})

	listing := store.Listing()
	if strings.Contains(listing, "review") {
		t.Error("listing should not contain disabled skill 'review'")
	}
	if strings.Contains(listing, "diagnose") {
		t.Error("listing should not contain disabled skill 'diagnose'")
	}
	if !strings.Contains(listing, "brainstorming") {
		t.Error("listing should contain enabled skill 'brainstorming'")
	}
}

func TestStoreSetEnabledEmptyEnablesAll(t *testing.T) {
	skills := []*Skill{
		{Name: "a", Description: "a", Instructions: "a"},
		{Name: "b", Description: "b", Instructions: "b"},
	}
	store := NewStore(skills)
	store.SetEnabled(nil) // nil = all enabled

	if !store.AllEnabled() {
		t.Error("nil SetEnabled should enable all")
	}
	if len(store.Enabled()) != 2 {
		t.Errorf("expected 2 enabled skills, got %d", len(store.Enabled()))
	}
}

// bodyMarkdown exercises the special characters that HTML-escaping would corrupt:
// shell operators, angle brackets, quotes, and a fenced code block.
const bodyMarkdown = "Use `Read` for files < 2000 lines && prefer <Grep> over \"cat\".\n\n```go\nif a < b && c > d {}\n```"

func TestFormatToolResultDoesNotEscapeBody(t *testing.T) {
	s := &Skill{Name: "demo", Description: "d", Instructions: bodyMarkdown}

	out := FormatToolResult(s, "")

	if !strings.Contains(out, bodyMarkdown) {
		t.Errorf("body must be injected verbatim, got:\n%s", out)
	}
	if strings.Contains(out, "&lt;") || strings.Contains(out, "&amp;") || strings.Contains(out, "&quot;") {
		t.Errorf("body must not be HTML-escaped, got:\n%s", out)
	}
}

func TestFormatInvocationDoesNotEscapeBodyOrArgs(t *testing.T) {
	s := &Skill{Name: "demo", Description: "d", Instructions: bodyMarkdown}

	out := FormatInvocation(s, "a < b && c")

	if !strings.Contains(out, bodyMarkdown) {
		t.Errorf("body must be injected verbatim, got:\n%s", out)
	}
	if !strings.Contains(out, "a < b && c") {
		t.Errorf("args must be injected verbatim, got:\n%s", out)
	}
	// The name still lives in an XML tag and must stay escaped when it needs it.
	esc := &Skill{Name: "demo", Description: "a < b", Instructions: "x"}
	if !strings.Contains(FormatInvocation(esc, ""), "a &lt; b") {
		t.Error("description inside XML tag should still be escaped")
	}
}

func TestFormatToolResultIncludesBaseDir(t *testing.T) {
	s := &Skill{
		Name:         "demo",
		Description:  "d",
		Instructions: "do the thing",
		FilePath:     "/home/u/.agents/skills/demo/SKILL.md",
	}

	out := FormatToolResult(s, "")

	if !strings.Contains(out, "Base directory for this skill: /home/u/.agents/skills/demo") {
		t.Errorf("expected base directory header, got:\n%s", out)
	}
}

func TestFormatToolResultOmitsBaseDirWhenNoPath(t *testing.T) {
	s := &Skill{Name: "demo", Description: "d", Instructions: "do the thing"}

	if strings.Contains(FormatToolResult(s, ""), "Base directory") {
		t.Error("skills without a file path must not emit a base directory header")
	}
}
