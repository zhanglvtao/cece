package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverAllDoesNotLoadBuiltinSkills(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmpDir, "home"))

	skills := DiscoverAll(filepath.Join(tmpDir, "project"))
	if len(skills) != 0 {
		t.Fatalf("expected no skills without user/project skill dirs, got %d", len(skills))
	}
}

func TestDiscoverFromDir_Symlink(t *testing.T) {
	// Create a temp layout:
	//   tmpDir/
	//     real-skills/
	//       my-skill/
	//         SKILL.md
	//     project/
	//       .cece/
	//         skills/
	//           linked-skill -> ../../../real-skills/my-skill
	tmpDir := t.TempDir()

	// Create the real skill directory and SKILL.md
	realSkillDir := filepath.Join(tmpDir, "real-skills", "my-skill")
	if err := os.MkdirAll(realSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := `---
name: my-skill
description: A test skill
---
Do something useful.
`
	if err := os.WriteFile(filepath.Join(realSkillDir, SkillFileName), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create project .agents/skills with a symlink
	skillsDir := filepath.Join(tmpDir, "project", ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(skillsDir, "linked-skill")
	if err := os.Symlink(realSkillDir, linkPath); err != nil {
		t.Fatal(err)
	}

	// Discover
	skills := discoverFromDir(skillsDir, "project")
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "my-skill" {
		t.Errorf("expected skill name 'my-skill', got %q", skills[0].Name)
	}
	if skills[0].Source != "project" {
		t.Errorf("expected source 'project', got %q", skills[0].Source)
	}
}

func TestDiscoverFromDir_RegularDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a regular (non-symlink) skill directory
	skillDir := filepath.Join(tmpDir, "regular-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := `---
name: regular-skill
description: A regular skill
---
Do regular things.
`
	if err := os.WriteFile(filepath.Join(skillDir, SkillFileName), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := discoverFromDir(tmpDir, "project")
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "regular-skill" {
		t.Errorf("expected skill name 'regular-skill', got %q", skills[0].Name)
	}
}

func TestDiscoverFromDir_NestedSymlink(t *testing.T) {
	// Test: skill directory contains a subdirectory that is a symlink
	tmpDir := t.TempDir()

	// Real nested skill
	realSkillDir := filepath.Join(tmpDir, "real", "nested-skill")
	if err := os.MkdirAll(realSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := `---
name: nested-skill
description: Nested via symlink
---
Nested.
`
	if err := os.WriteFile(filepath.Join(realSkillDir, SkillFileName), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Parent skills dir with symlinked child
	skillsDir := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realSkillDir, filepath.Join(skillsDir, "nested-skill")); err != nil {
		t.Fatal(err)
	}

	skills := discoverFromDir(skillsDir, "project")
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "nested-skill" {
		t.Errorf("expected 'nested-skill', got %q", skills[0].Name)
	}
}
