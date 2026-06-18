package usageledger

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendWritesBotmuxCompatibleLedgerRecord(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 18, 12, 34, 56, 0, time.UTC)
	usage := Usage{
		SessionID:              "session-1",
		Model:                  "glm-5.1",
		WorkingDir:             "/repo",
		InputTokens:            10,
		OutputTokens:           20,
		CacheReadTokens:        3,
		CacheCreateTokens:      4,
		TotalInputTokens:       100,
		TotalOutputTokens:      200,
		TotalCacheReadTokens:   30,
		TotalCacheCreateTokens: 40,
	}

	record, path, err := Append(context.Background(), usage, Options{Dir: dir, Now: now})
	if err != nil {
		t.Fatalf("Append error = %v", err)
	}
	if record == nil {
		t.Fatal("record = nil")
	}
	wantPath := filepath.Join(dir, "usage-2026-06-18.jsonl")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	var got Record
	if err := json.Unmarshal(data[:len(data)-1], &got); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if got.Version != 1 || got.CLIID != "claude-code" || got.SessionID != "session-1" || got.Model != "glm-5.1" || got.WorkingDir != "/repo" {
		t.Fatalf("record meta = %#v", got)
	}
	if got.InputTokens != 10 || got.OutputTokens != 20 || got.CacheReadTokens != 3 || got.CacheCreateTokens != 4 {
		t.Fatalf("record deltas = %#v", got)
	}
	if got.TotalInputTokens != 100 || got.TotalOutputTokens != 200 || got.TotalCacheReadTokens != 30 || got.TotalCacheCreateTokens != 40 {
		t.Fatalf("record totals = %#v", got)
	}
}

func TestAppendRecordIDIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	usage := Usage{SessionID: "session-1", Model: "m", InputTokens: 1, TotalInputTokens: 1}

	first, _, err := Append(context.Background(), usage, Options{Dir: dir, Now: time.Date(2026, 6, 18, 1, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("first Append error = %v", err)
	}
	second, _, err := Append(context.Background(), usage, Options{Dir: dir, Now: time.Date(2026, 6, 18, 2, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("second Append error = %v", err)
	}
	if first.RecordID != second.RecordID {
		t.Fatalf("RecordID = %q then %q, want deterministic", first.RecordID, second.RecordID)
	}
}

func TestAppendSkipsEmptyUsage(t *testing.T) {
	dir := t.TempDir()
	record, path, err := Append(context.Background(), Usage{SessionID: "session-1"}, Options{Dir: dir, Now: time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Append error = %v", err)
	}
	if record != nil || path != "" {
		t.Fatalf("record = %#v path = %q, want skip", record, path)
	}
	if _, err := os.Stat(filepath.Join(dir, "usage-2026-06-18.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("ledger file should not exist, stat err = %v", err)
	}
}
