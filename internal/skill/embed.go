package skill

import (
	"embed"
	"log/slog"
	"path/filepath"
)

//go:embed builtin/*
var builtinFS embed.FS

// DiscoverBuiltin finds all valid skills embedded in the binary.
func DiscoverBuiltin() []*Skill {
	var discovered []*Skill

	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		slog.Warn("failed to read builtin skills", "error", err)
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join("builtin", entry.Name(), SkillFileName)
		data, err := builtinFS.ReadFile(skillPath)
		if err != nil {
			continue
		}

		skill, err := ParseContent(data)
		if err != nil {
			slog.Warn("failed to parse builtin skill", "path", skillPath, "error", err)
			continue
		}

		if err := skill.Validate(); err != nil {
			slog.Warn("builtin skill validation failed", "path", skillPath, "error", err)
			continue
		}

		skill.Source = "builtin"
		skill.FilePath = "cece://skills/" + entry.Name() + "/" + SkillFileName
		discovered = append(discovered, skill)
	}

	return discovered
}
