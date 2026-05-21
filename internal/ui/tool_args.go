package ui

import (
	"fmt"
	"strings"
)

// formatToolArgs renders tool call arguments in a human-readable format
// instead of raw JSON. Each tool type has specific display logic:
//   - Bash: shows the command only
//   - Read: shows file_path, plus offset/limit if present
//   - Edit: shows file_path, plus replace_all if true
//   - Write: shows file_path only (content is too large)
//   - Glob: shows pattern, plus path if present
//   - Grep: shows pattern, plus include/path if present
//   - Unknown: shows all key:value pairs
func formatToolArgs(name string, args map[string]any) string {
	if len(args) == 0 {
		return ""
	}

	switch name {
	case "Bash":
		return strVal(args, "command")

	case "Read":
		return filePathWithOpts(args, "file_path", "offset", "limit")

	case "Edit":
		return filePathWithBoolOpt(args, "file_path", "replace_all")

	case "Write":
		return strVal(args, "file_path")

	case "Glob":
		return patternWithPath(args, "pattern", "path")

	case "Grep":
		return grepArgs(args)

	default:
		return genericArgs(args)
	}
}

// strVal returns the string value for a single key, or "" if missing.
func strVal(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// filePathWithOpts shows the primary file_path followed by optional numeric keys.
// e.g. "/foo/bar.go  offset:10  limit:20"
func filePathWithOpts(args map[string]any, primary string, opts ...string) string {
	path := strVal(args, primary)
	if path == "" {
		return genericArgs(args)
	}
	var parts []string
	parts = append(parts, path)
	for _, opt := range opts {
		if v, ok := args[opt]; ok {
			parts = append(parts, fmt.Sprintf("%s:%v", opt, v))
		}
	}
	return strings.Join(parts, "  ")
}

// filePathWithBoolOpt shows the primary file_path followed by a boolean key
// only when its value is true.
func filePathWithBoolOpt(args map[string]any, primary string, boolKey string) string {
	path := strVal(args, primary)
	if path == "" {
		return genericArgs(args)
	}
	var parts []string
	parts = append(parts, path)
	if v, ok := args[boolKey]; ok && v == true {
		parts = append(parts, fmt.Sprintf("%s:true", boolKey))
	}
	return strings.Join(parts, "  ")
}

// patternWithPath shows the primary pattern followed by an optional path.
func patternWithPath(args map[string]any, primary string, pathKey string) string {
	pattern := strVal(args, primary)
	if pattern == "" {
		return genericArgs(args)
	}
	var parts []string
	parts = append(parts, pattern)
	if v, ok := args[pathKey]; ok {
		parts = append(parts, fmt.Sprintf("%s:%v", pathKey, v))
	}
	return strings.Join(parts, "  ")
}

// grepArgs shows pattern, include, and path for the Grep tool.
func grepArgs(args map[string]any) string {
	pattern := strVal(args, "pattern")
	if pattern == "" {
		return genericArgs(args)
	}
	var parts []string
	parts = append(parts, pattern)
	if v, ok := args["include"]; ok {
		parts = append(parts, fmt.Sprintf("include:%v", v))
	}
	if v, ok := args["path"]; ok {
		parts = append(parts, fmt.Sprintf("path:%v", v))
	}
	return strings.Join(parts, "  ")
}

// genericArgs renders all key:value pairs for unknown tools.
func genericArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	var parts []string
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s:%v", k, v))
	}
	return strings.Join(parts, "  ")
}
