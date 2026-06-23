package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type ResultStore struct {
	dir string
}

func NewResultStore(projectDir string) *ResultStore {
	if strings.TrimSpace(projectDir) == "" {
		projectDir, _ = os.Getwd()
	}
	return &ResultStore{dir: filepath.Join(projectDir, ".cece", "tool-results")}
}

func (s *ResultStore) Apply(toolName string, result Result, policy ResultStoragePolicy) Result {
	if result.Truncated || result.OutputPath != "" || policy.MaxBytes <= 0 {
		return result
	}
	originalBytes := len(result.Content)
	if originalBytes <= policy.MaxBytes {
		return result
	}

	previewBytes := policy.PreviewBytes
	if previewBytes <= 0 || previewBytes > policy.MaxBytes {
		previewBytes = policy.MaxBytes
	}
	preview := prefixBytes(result.Content, previewBytes)

	if s == nil {
		return fallbackTruncatedResult(result, originalBytes)
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fallbackTruncatedResult(result, originalBytes)
	}
	f, err := os.CreateTemp(s.dir, safeToolResultPrefix(toolName)+"-*.txt")
	if err != nil {
		return fallbackTruncatedResult(result, originalBytes)
	}
	path := f.Name()
	_, writeErr := f.WriteString(result.Content)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return fallbackTruncatedResult(result, originalBytes)
	}

	result.Content = largeResultMessage(originalBytes, path, preview, len(preview))
	result.Truncated = true
	result.OutputPath = path
	result.OriginalBytes = originalBytes
	result.PreviewBytes = len(preview)
	return result
}

func safeToolResultPrefix(toolName string) string {
	name := strings.ToLower(toolName)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "tool"
	}
	return b.String()
}

func fallbackTruncatedResult(result Result, originalBytes int) Result {
	result.Content = truncateOutput(result.Content)
	result.Truncated = true
	result.OriginalBytes = originalBytes
	result.PreviewBytes = len(result.Content)
	return result
}

func largeResultMessage(originalBytes int, path, preview string, previewBytes int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Output too large (%d bytes). Full output saved to: %s\n\n", originalBytes, path)
	fmt.Fprintf(&b, "Preview (first %d bytes):\n", previewBytes)
	b.WriteString(preview)
	if !strings.HasSuffix(preview, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("...\n\n")
	b.WriteString("Use Read with offset/limit or Grep to inspect the saved file. Do not read the full file directly unless necessary.")
	return b.String()
}

func prefixBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
