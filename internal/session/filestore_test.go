package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreDeleteRemovesSessionFiles(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{dir: filepath.Join(dir, ".cece", "sessions")}

	sess, err := s.Create(context.Background(), "test session")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AppendMessage(context.Background(), sess.ID, json.RawMessage(`{"role":"user","content":"hi"}`)); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	if err := s.Delete(context.Background(), sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(s.jsonlPath(sess.ID)); !os.IsNotExist(err) {
		t.Fatalf("jsonl err = %v, want not exist", err)
	}
	if _, err := os.Stat(s.metaPath(sess.ID)); !os.IsNotExist(err) {
		t.Fatalf("meta err = %v, want not exist", err)
	}
}

func TestFileStoreUpdateMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{dir: filepath.Join(dir, ".cece", "sessions")}

	sess, err := s.Create(context.Background(), "test session")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	meta := SessionMeta{
		Model:             "claude-sonnet-4-20250514",
		ContextWindow:     200000,
		LastInputTokens:   321,
		TotalInputTokens:  1234,
		TotalOutputTokens: 567,
	}
	if err := s.UpdateMeta(context.Background(), sess.ID, meta); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}

	got, err := s.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Model != meta.Model {
		t.Errorf("Model = %q, want %q", got.Model, meta.Model)
	}
	if got.ContextWindow != meta.ContextWindow {
		t.Errorf("ContextWindow = %d, want %d", got.ContextWindow, meta.ContextWindow)
	}
	if got.TotalInputTokens != meta.TotalInputTokens {
		t.Errorf("TotalInputTokens = %d, want %d", got.TotalInputTokens, meta.TotalInputTokens)
	}
	if got.LastInputTokens != meta.LastInputTokens {
		t.Errorf("LastInputTokens = %d, want %d", got.LastInputTokens, meta.LastInputTokens)
	}
	if got.TotalOutputTokens != meta.TotalOutputTokens {
		t.Errorf("TotalOutputTokens = %d, want %d", got.TotalOutputTokens, meta.TotalOutputTokens)
	}

	// Update again to verify overwrite
	meta2 := SessionMeta{
		Model:             "gpt-4o",
		ContextWindow:     128000,
		LastInputTokens:   654,
		TotalInputTokens:  9999,
		TotalOutputTokens: 888,
	}
	if err := s.UpdateMeta(context.Background(), sess.ID, meta2); err != nil {
		t.Fatalf("UpdateMeta2: %v", err)
	}

	got2, err := s.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Get2: %v", err)
	}
	if got2.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", got2.Model)
	}
	if got2.TotalInputTokens != 9999 {
		t.Errorf("TotalInputTokens = %d, want 9999", got2.TotalInputTokens)
	}
	if got2.LastInputTokens != 654 {
		t.Errorf("LastInputTokens = %d, want 654", got2.LastInputTokens)
	}
}
