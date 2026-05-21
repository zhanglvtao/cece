package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const maxOutputLen = 30000

type bashParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"` // seconds, 0 = no timeout
}

type bashTool struct{}

func NewBash() Tool { return bashTool{} }

func (bashTool) Info() Definition {
	return Definition{
		Name:        "Bash",
		Description: "Execute a bash command and return its output.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The bash command to execute",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Optional timeout in seconds (default: 120)",
				},
			},
			"required": []string{"command"},
		},
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

	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(p.Timeout)*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", p.Command)

	// Capture stderr directly — no streaming needed for stderr.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// stdout: pipe through TeeReader so we stream to emitter AND capture for Result.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Result{Content: fmt.Sprintf("stdout pipe: %v", err), IsError: true}
	}

	if err := cmd.Start(); err != nil {
		return Result{Content: fmt.Sprintf("start: %v", err), IsError: true}
	}

	// TeeReader: every read goes to both the buffer (for Result) and the emitter.
	var stdoutBuf bytes.Buffer
	var teeReader io.Reader = stdoutPipe
	if emitter != nil {
		teeReader = io.TeeReader(stdoutPipe, &lineWriter{emitter: emitter})
	}
	// Must drain the pipe fully; otherwise cmd.Wait() can deadlock.
	io.Copy(&stdoutBuf, teeReader)

	waitErr := cmd.Wait()

	// Build result content: stdout + stderr + error
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

// lineWriter is an io.Writer that emits complete lines to an Emitter.
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
