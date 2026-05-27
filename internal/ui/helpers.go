package ui

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	toolPreviewBytes    = 2000
	toolPreviewMaxLines = 20
)

func formatJSONPreview(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return summarizeText(string(raw), 1000, 15)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return summarizeText(string(raw), 1000, 15)
	}
	return summarizeText(string(pretty), 1000, 15)
}

func summarizeText(s string, maxBytes, maxLines int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	truncated := false
	if len(s) > maxBytes {
		s = s[:maxBytes]
		truncated = true
	}
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		head := maxLines / 2
		tail := maxLines - head
		kept := append([]string{}, lines[:head]...)
		kept = append(kept, fmt.Sprintf("... %d lines hidden ...", len(lines)-maxLines))
		kept = append(kept, lines[len(lines)-tail:]...)
		lines = kept
		truncated = true
	}
	out := strings.Join(lines, "\n")
	if truncated {
		out += "\n... truncated ..."
	}
	return out
}

func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
