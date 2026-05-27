package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	maxOutputLen              = 30000
	DefaultBashTimeoutSeconds = 10
)

type bashParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"` // seconds, 0 uses the default timeout
}

type bashTool struct{}

func NewBash() Tool { return bashTool{} }

func (bashTool) Effect() Effect { return EffectExec }

func (bashTool) Info() Definition {
	return Definition{
		Name:        "Bash",
		Description: "Execute a bash command and return its output. In plan mode, use only read-only exploration commands such as ls, pwd, git status, git log, git diff, find, grep, cat, head, and tail. Do not run commands that modify state, including mkdir, touch, rm, mv, cp, redirection writes (> or >>), heredocs that write files, git add/commit/push/checkout/reset, package installs, config changes, or generated-file commands.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The bash command to execute",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Optional timeout in seconds. Defaults to 10 when omitted or set to 0.",
					"default":     10,
					"minimum":     0,
				},
			},
			"required": []string{"command"},
		},
	}
}

func ResolveBashTimeoutSeconds(timeout int) (int, error) {
	return resolveBashTimeoutSeconds(timeout)
}

func resolveBashTimeoutSeconds(timeout int) (int, error) {
	switch {
	case timeout < 0:
		return 0, fmt.Errorf("must be >= 0")
	case timeout == 0:
		return DefaultBashTimeoutSeconds, nil
	default:
		return timeout, nil
	}
}

func (bashTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	var p bashParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}
	if p.Command == "" {
		return Result{Content: "missing command", IsError: true}
	}

	timeoutSeconds, err := resolveBashTimeoutSeconds(p.Timeout)
	if err != nil {
		return Result{Content: fmt.Sprintf("invalid timeout: %v", err), IsError: true}
	}

	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", p.Command)
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Result{Content: fmt.Sprintf("stdout pipe: %v", err), IsError: true}
	}

	if err := cmd.Start(); err != nil {
		return Result{Content: fmt.Sprintf("start: %v", err), IsError: true}
	}

	// When the context is cancelled (timeout / user cancel), kill the entire
	// process group so that child processes spawned by bash are also terminated.
	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()

	var stdoutBuf bytes.Buffer
	var teeReader io.Reader = stdoutPipe
	if emitter != nil {
		teeReader = io.TeeReader(stdoutPipe, &lineWriter{emitter: emitter})
	}
	io.Copy(&stdoutBuf, teeReader)

	waitErr := cmd.Wait()

	var b strings.Builder
	if stdoutBuf.Len() > 0 {
		b.Write(stdoutBuf.Bytes())
	}
	if stderr.Len() > 0 {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.Write(stderr.Bytes())
	}
	if waitErr != nil && ctx.Err() == context.DeadlineExceeded {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("command timed out")
	} else if waitErr != nil {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(waitErr.Error())
	}

	return Result{Content: truncateOutput(b.String()), IsError: waitErr != nil}
}

type lineWriter struct {
	emitter Emitter
	partial string
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.partial += string(p)
	for {
		i := strings.IndexByte(w.partial, '\n')
		if i < 0 {
			break
		}
		w.emitter.Emit(w.partial[:i])
		w.partial = w.partial[i+1:]
	}
	return len(p), nil
}

func truncateOutput(s string) string {
	if len(s) <= maxOutputLen {
		return s
	}
	half := maxOutputLen / 2
	truncated := countLines(s[half : len(s)-half])
	return fmt.Sprintf("%s\n\n... [%d lines truncated] ...\n\n%s", s[:half], truncated, s[len(s)-half:])
}

func countLines(s string) int {
	n := strings.Count(s, "\n")
	if len(s) > 0 && !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}
