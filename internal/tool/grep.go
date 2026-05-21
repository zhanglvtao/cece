package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxGrepResults   = 100
	maxGrepLineWidth = 500
)

type grepParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Include string `json:"include,omitempty"`
}

type grepMatch struct {
	path     string
	lineNum  int
	lineText string
}

type grepTool struct{}

func NewGrep() Tool { return grepTool{} }

func (grepTool) Info() Definition {
	return Definition{
		Name:        "Grep",
		Description: "Search file contents by regex pattern.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "The regex pattern to search for in file contents",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "The directory to search in. Defaults to the current working directory.",
				},
				"include": map[string]any{
					"type":        "string",
					"description": "File pattern to include (e.g. \"*.go\", \"*.{ts,tsx}\")",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (grepTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	var p grepParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}
	if p.Pattern == "" {
		return Result{Content: "missing pattern", IsError: true}
	}

	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return Result{Content: fmt.Sprintf("invalid regex: %v", err), IsError: true}
	}

	searchPath := p.Path
	if searchPath == "" {
		searchPath, _ = os.Getwd()
	}

	var includeRe *regexp.Regexp
	if p.Include != "" {
		includeRe, err = regexp.Compile(globToRegex(p.Include))
		if err != nil {
			return Result{Content: fmt.Sprintf("invalid include pattern: %v", err), IsError: true}
		}
	}

	if emitter != nil {
		emitter.Emit(fmt.Sprintf("Searching for %q in %s...", p.Pattern, searchPath))
	}

	var matches []grepMatch
	err = filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if info.IsDir() {
			if isHiddenDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if includeRe != nil && !includeRe.MatchString(filepath.Base(path)) {
			return nil
		}
		if !isTextFile(path) {
			return nil
		}
		fileMatches, err := grepFile(path, re)
		if err != nil {
			return nil
		}
		matches = append(matches, fileMatches...)
		if len(matches) >= maxGrepResults {
			matches = matches[:maxGrepResults]
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		return Result{Content: fmt.Sprintf("walk: %v", err), IsError: true}
	}

	if len(matches) == 0 {
		return Result{Content: "No files found"}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d matches\n", len(matches))
	currentFile := ""
	for _, m := range matches {
		if currentFile != m.path {
			if currentFile != "" {
				b.WriteByte('\n')
			}
			currentFile = m.path
			fmt.Fprintf(&b, "%s:\n", filepath.ToSlash(m.path))
		}
		lineText := m.lineText
		if len(lineText) > maxGrepLineWidth {
			lineText = lineText[:maxGrepLineWidth] + "..."
		}
		fmt.Fprintf(&b, "  Line %d: %s\n", m.lineNum, lineText)
	}

	return Result{Content: truncateOutput(b.String())}
}

func grepFile(path string, re *regexp.Regexp) ([]grepMatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matches []grepMatch
	reader := bufio.NewReader(f)
	lineNum := 0
	for {
		line, err := reader.ReadString('\n')
		lineNum++
		line = strings.TrimRight(line, "\r\n")
		if re.MatchString(line) {
			matches = append(matches, grepMatch{
				path:     path,
				lineNum:  lineNum,
				lineText: line,
			})
			if len(matches) >= maxGrepResults {
				return matches, nil
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return matches, err
		}
	}
	return matches, nil
}

func isHiddenDir(path string) bool {
	base := filepath.Base(path)
	return base != "." && strings.HasPrefix(base, ".")
}

func isTextFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}

	ct := http.DetectContentType(buf[:n])
	return strings.HasPrefix(ct, "text/") ||
		ct == "application/json" ||
		ct == "application/xml" ||
		ct == "application/javascript" ||
		ct == "application/x-sh"
}

func globToRegex(glob string) string {
	// Handle brace patterns like *.{ts,tsx} → (.*\.ts|.*\.tsx)
	if braceOpen := strings.Index(glob, "{"); braceOpen >= 0 {
		braceClose := strings.Index(glob, "}")
		if braceClose > braceOpen {
			inner := glob[braceOpen+1 : braceClose]
			alternatives := strings.Split(inner, ",")
			prefix := globToRegex(glob[:braceOpen])
			suffix := globToRegex(glob[braceClose+1:])
			var alts []string
			for _, a := range alternatives {
				alts = append(alts, prefix+globToRegex(a)+suffix)
			}
			return "(" + strings.Join(alts, "|") + ")"
		}
	}
	// Convert basic glob: * → .*, ? → ., escape the rest
	var b strings.Builder
	for _, r := range glob {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return b.String()
}
