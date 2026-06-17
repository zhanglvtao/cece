package logger

import (
	"bytes"
	"log/slog"
	"os"
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

func TestCleanOldLogs(t *testing.T) {
	dir := t.TempDir()
	// Create some fake archived log files
	for _, name := range []string{
		"cece-20260101000000.log",
		"cece-20260201000000.log",
		"cece-20260301000000.log",
	} {
		content := strings.Repeat("x", 300) // 300 bytes each
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	// Total is 900 bytes, well under 10GB limit, so no files should be deleted
	cleanOldLogs(dir)

	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (all under 10GB), got %d", len(entries))
	}
}

func TestArchivedLogPath(t *testing.T) {
	path := archivedLogPath("/tmp/logs")
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "cece-") {
		t.Fatalf("archived log name should start with cece-: %q", base)
	}
	if !strings.HasSuffix(base, ".log") {
		t.Fatalf("archived log name should end with .log: %q", base)
	}
}

func TestActiveLogPath(t *testing.T) {
	path := activeLogPath("/tmp/logs")
	if filepath.Base(path) != "cece.log" {
		t.Fatalf("active log name should be cece.log: %q", filepath.Base(path))
	}
}
