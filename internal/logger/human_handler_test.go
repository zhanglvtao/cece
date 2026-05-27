package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHumanHandlerOutputFormat(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	var buf bytes.Buffer
	h := newHumanHandler(&buf, loc, true, nil)
	log := slog.New(h)

	log.Info("stream request", "url", "https://api.anthropic.com/v1/messages", "model", "claude-sonnet-4-6")

	line := buf.String()
	if !strings.Contains(line, "INFO ") {
		t.Fatalf("missing INFO level: %q", line)
	}
	if !strings.Contains(line, "stream request") {
		t.Fatalf("missing message: %q", line)
	}
	if !strings.Contains(line, "url=https://api.anthropic.com/v1/messages") {
		t.Fatalf("missing url attr: %q", line)
	}
	if !strings.Contains(line, "model=claude-sonnet-4-6") {
		t.Fatalf("missing model attr: %q", line)
	}
}

func TestHumanHandlerTruncatesLongValues(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	var buf bytes.Buffer
	h := newHumanHandler(&buf, loc, true, nil)
	log := slog.New(h)

	longBody := strings.Repeat("x", 2000)
	log.Debug("api request body", "body", longBody)

	line := buf.String()
	if strings.Contains(line, strings.Repeat("x", 2000)) {
		t.Fatal("long value should be truncated")
	}
	if !strings.Contains(line, "...") {
		t.Fatalf("truncated value should end with ...: %q", line)
	}
}

func TestHumanHandlerQuotesValuesWithSpaces(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	var buf bytes.Buffer
	h := newHumanHandler(&buf, loc, true, nil)
	log := slog.New(h)

	log.Info("test", "msg", "hello world")

	line := buf.String()
	if !strings.Contains(line, `msg="hello world"`) {
		t.Fatalf("space-containing value should be quoted: %q", line)
	}
}

func TestHumanHandlerLevelFiltering(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	var buf bytes.Buffer
	h := newHumanHandler(&buf, loc, false, nil) // debug=false
	log := slog.New(h)

	log.Debug("should not appear")
	log.Info("should appear")

	line := buf.String()
	if strings.Contains(line, "should not appear") {
		t.Fatal("DEBUG should be filtered when debug=false")
	}
	if !strings.Contains(line, "should appear") {
		t.Fatal("INFO should appear when debug=false")
	}
}

func TestSessionHandlerInjectsSessionID(t *testing.T) {
	SetSessionID("sess-123")
	t.Cleanup(func() { SetSessionID("") })

	var buf bytes.Buffer
	h := &sessionHandler{next: slog.NewJSONHandler(&buf, nil)}
	slog.New(h).Info("test")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal log: %v", err)
	}
	if got["session_id"] != "sess-123" {
		t.Fatalf("session_id = %v, want sess-123", got["session_id"])
	}
}

func TestLoggerWrappersUseCallerSource(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(&sessionHandler{next: slog.NewJSONHandler(&buf, &slog.HandlerOptions{AddSource: true})}))
	t.Cleanup(func() { slog.SetDefault(previous) })

	Info("caller source")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal log: %v", err)
	}
	source, ok := got["source"].(map[string]any)
	if !ok {
		t.Fatalf("missing source: %#v", got["source"])
	}
	if filepath.Base(source["file"].(string)) != "human_handler_test.go" {
		t.Fatalf("source file = %v, want human_handler_test.go", source["file"])
	}
	if source["line"].(float64) == 0 {
		t.Fatalf("source line missing: %#v", source)
	}
}
