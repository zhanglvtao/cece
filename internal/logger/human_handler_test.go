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

func TestHumanHandlerSessionIDPrefix(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	var buf bytes.Buffer
	h := newHumanHandler(&buf, loc, true, nil)
	sh := &sessionHandler{next: h}
	log := slog.New(sh)

	SetSessionID("abc-123")
	t.Cleanup(func() { SetSessionID("") })

	log.Info("stream request", "url", "https://api.example.com")

	line := buf.String()
	if !strings.Contains(line, "[abc-123] stream request") {
		t.Fatalf("session_id should appear as prefix: %q", line)
	}
	if strings.Contains(line, "session_id=abc-123") {
		t.Fatalf("session_id should NOT appear as trailing attr: %q", line)
	}
}

func TestHumanHandlerNoSessionID(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	var buf bytes.Buffer
	h := newHumanHandler(&buf, loc, true, nil)
	// Log directly without sessionHandler — no session_id attr injected.
	log := slog.New(h)

	log.Info("hello")

	line := buf.String()
	if strings.Contains(line, "[") {
		t.Fatalf("no bracket prefix when session_id is empty: %q", line)
	}
	if !strings.Contains(line, "INFO  hello") {
		t.Fatalf("message should appear without session prefix: %q", line)
	}
}

func TestGenerateSessionID(t *testing.T) {
	id := generateSessionID()
	if len(id) != 8 {
		t.Fatalf("session id length = %d, want 8: %q", len(id), id)
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("session id contains non-hex char: %q", id)
		}
	}
	// Should generate different IDs on successive calls.
	id2 := generateSessionID()
	if id == id2 {
		t.Fatalf("two generated IDs should differ: %q == %q", id, id2)
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
