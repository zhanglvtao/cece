package skill

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverAll discovers skills from builtin + user ~/.agents/skills/ + project .agents/skills/.
// Project skills override user skills; user skills override builtin with the same name.
func DiscoverAll(projectDir string) []*Skill {
	all := DiscoverBuiltin()

	// User-level skills from ~/.agents/skills/
	home, _ := os.UserHomeDir()
	if home != "" {
		userSkillsDir := filepath.Join(home, ".agents", "skills")
		userSkills := discoverFromDir(userSkillsDir, "user")
		all = append(all, userSkills...)
	}

	// Project-level skills from .agents/skills/
	skillsDir := filepath.Join(projectDir, ".agents", "skills")
	projectSkills := discoverFromDir(skillsDir, "project")
	all = append(all, projectSkills...)

	return Deduplicate(all)
}

// discoverFromDir walks a directory tree looking for SKILL.md files.
// Symlink directories are resolved and walked recursively.
func discoverFromDir(dir, source string) []*Skill {
	var discovered []*Skill

	info, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn("failed to read skills directory", "path", dir, "error", err)
		return nil
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		// Handle symlinks: resolve and recurse into symlink directories.
		if entry.Type()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				slog.Warn("failed to resolve skill symlink", "path", path, "error", err)
				continue
			}
			fi, err := os.Stat(resolved)
			if err != nil {
				slog.Warn("failed to stat resolved symlink target", "path", resolved, "error", err)
				continue
			}
			if fi.IsDir() {
				sub := discoverFromDir(resolved, source)
				discovered = append(discovered, sub...)
			} else if filepath.Base(resolved) == SkillFileName {
				discovered = append(discovered, parseSkillFile(resolved, source)...)
			}
			continue
		}

		if entry.IsDir() {
			sub := discoverFromDir(path, source)
			discovered = append(discovered, sub...)
			continue
		}

		if entry.Name() == SkillFileName {
			discovered = append(discovered, parseSkillFile(path, source)...)
		}
	}

	return discovered
}

// parseSkillFile reads and parses a SKILL.md file, returning a slice with one skill on success.
func parseSkillFile(path, source string) []*Skill {
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("failed to read skill file", "path", path, "error", err)
		return nil
	}

	s, err := ParseContent(data)
	if err != nil {
		slog.Warn("failed to parse skill file", "path", path, "error", err)
		return nil
	}

	if err := s.Validate(); err != nil {
		slog.Warn("skill validation failed", "path", path, "error", err)
		return nil
	}

	s.Source = source
	s.FilePath = path
	return []*Skill{s}
}

// Deduplicate removes duplicate skills by name. Last occurrence wins,
// so project skills override builtin skills with the same name.
func Deduplicate(all []*Skill) []*Skill {
	seen := make(map[string]int, len(all))
	for i, s := range all {
		seen[s.Name] = i
	}

	result := make([]*Skill, 0, len(seen))
	for i, s := range all {
		if seen[s.Name] == i {
			result = append(result, s)
		}
	}
	return result
}

// homeDir returns the user's home directory.
func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

// ExpandPath handles ~/ expansion in paths.
func ExpandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), p[2:])
	}
	return p
}
