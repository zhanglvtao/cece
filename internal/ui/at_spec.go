package ui

import (
	"os"
	"path/filepath"
	"strings"
)

// atSpec describes an active "@" file mention in the input.
type atSpec struct {
	Active   bool   // true if cursor is inside an @query
	Query    string // text after @ (e.g. "~/claude-code/mo" or "internal/ui/mo")
	BaseDir  string // directory part of query (e.g. "~/claude-code/" or "internal/ui/")
	FileName string // filename part of query (e.g. "mo")
	StartIdx int    // byte index where @ starts in the input
	AbsRoot  string // expanded absolute root for the search (e.g. "/Users/foo/claude-code/")
	IsAbs    bool   // true if query starts with ~/ or /
}

// parseAtSpec finds the last @ in the input and extracts the query.
// It works at any position — no whitespace prefix requirement.
func parseAtSpec(input, projectDir string) atSpec {
	// Find the last @ character
	atIdx := strings.LastIndex(input, "@")
	if atIdx < 0 {
		return atSpec{}
	}

	// Extract everything after @ until whitespace or end
	afterAt := input[atIdx+1:]
	query := afterAt
	if idx := strings.IndexAny(afterAt, " \t\n"); idx >= 0 {
		query = afterAt[:idx]
	}

	// Split into BaseDir and FileName
	baseDir, fileName := "", query
	if slashIdx := strings.LastIndex(query, "/"); slashIdx >= 0 {
		baseDir = query[:slashIdx+1]
		fileName = query[slashIdx+1:]
	}

	// Determine absolute root
	isAbs := strings.HasPrefix(query, "~/") || strings.HasPrefix(query, "/")
	absRoot := projectDir // default: search in project
	if isAbs {
		expanded := expandHome(baseDir)
		if expanded != "" {
			absRoot = expanded
		}
	} else if baseDir != "" {
		absRoot = filepath.Join(projectDir, baseDir)
	}

	return atSpec{
		Active:   true,
		Query:    query,
		BaseDir:  baseDir,
		FileName: fileName,
		StartIdx: atIdx,
		AbsRoot:  absRoot,
		IsAbs:    isAbs,
	}
}

// expandHome expands ~/ to the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, path[2:])
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return home
	}
	return path
}
