package lint

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultTimeout = 10 * time.Second

// Runner executes lint commands based on file extension.
type Runner struct {
	rules      map[string]string // ext (without dot) → command template
	projectDir string
}

// NewRunner creates a lint runner. rules may be nil (all calls return empty).
func NewRunner(rules map[string]string, projectDir string) *Runner {
	if rules == nil {
		rules = map[string]string{}
	}
	return &Runner{rules: rules, projectDir: projectDir}
}

// Enabled returns true if at least one lint rule is configured.
func (r *Runner) Enabled() bool {
	return len(r.rules) > 0
}

// Run executes the lint command for the given file.
// Returns empty string if no rule matches or lint passes cleanly.
// Only non-empty output (errors/warnings) is returned.
func (r *Runner) Run(ctx context.Context, filePath string) string {
	ext := strings.TrimPrefix(filepath.Ext(filePath), ".")
	if ext == "" {
		return ""
	}
	cmdTpl, ok := r.rules[ext]
	if !ok {
		return ""
	}

	cmdStr := strings.ReplaceAll(cmdTpl, "{file}", filePath)

	timeout := defaultTimeout
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	if runtime.GOOS != "windows" {
		cmd.Dir = r.projectDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Lint tools typically exit non-zero when they find issues.
	// If exit 0 and no output → clean, return empty.
	// If context deadline → skip silently.
	if ctx.Err() == context.DeadlineExceeded {
		return ""
	}

	var out strings.Builder
	if stdout.Len() > 0 {
		out.Write(stdout.Bytes())
	}
	if stderr.Len() > 0 {
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.Write(stderr.Bytes())
	}

	// No output means clean — return empty regardless of exit code.
	if out.Len() == 0 {
		return ""
	}

	// Only return output when the command actually found issues (non-zero exit).
	if err != nil {
		return fmt.Sprintf("<lint>\n%s\n</lint>", strings.TrimSpace(out.String()))
	}

	// Exit 0 but has output (some linters warn on stderr) — also show.
	return fmt.Sprintf("<lint>\n%s\n</lint>", strings.TrimSpace(out.String()))
}
