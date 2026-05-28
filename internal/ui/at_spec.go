package ui

import (
	"strings"
)

// atSpec describes an active "@" file mention in the input.
type atSpec struct {
	Active   bool   // true if cursor is inside an @query
	Query    string // text after @ (e.g. "internal/ui/mo")
	BaseDir  string // directory part of query (e.g. "internal/ui/")
	FileName string // filename part of query (e.g. "mo")
	StartIdx int    // byte index where @ starts in the input
}

// parseAtSpec finds the last @ in the input and extracts the query.
// It works at any position — no whitespace prefix requirement.
func parseAtSpec(input string) atSpec {
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

	return atSpec{
		Active:   true,
		Query:    query,
		BaseDir:  baseDir,
		FileName: fileName,
		StartIdx: atIdx,
	}
}
