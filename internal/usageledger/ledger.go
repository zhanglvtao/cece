package usageledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const CLIID = "claude-code"

type Usage struct {
	SessionID              string
	Model                  string
	WorkingDir             string
	InputTokens            int
	OutputTokens           int
	CacheReadTokens        int
	CacheCreateTokens      int
	TotalInputTokens       int
	TotalOutputTokens      int
	TotalCacheReadTokens   int
	TotalCacheCreateTokens int
}

type Record struct {
	Version                int    `json:"v"`
	RecordID               string `json:"recordId"`
	Timestamp              string `json:"ts"`
	Epoch                  int    `json:"epoch"`
	SessionID              string `json:"sessionId"`
	CLIID                  string `json:"cliId"`
	WorkingDir             string `json:"workingDir,omitempty"`
	Model                  string `json:"model"`
	InputTokens            int    `json:"inputTokens"`
	OutputTokens           int    `json:"outputTokens"`
	CacheReadTokens        int    `json:"cacheReadTokens"`
	CacheCreateTokens      int    `json:"cacheCreateTokens"`
	TotalInputTokens       int    `json:"totalInputTokens"`
	TotalOutputTokens      int    `json:"totalOutputTokens"`
	TotalCacheReadTokens   int    `json:"totalCacheReadTokens"`
	TotalCacheCreateTokens int    `json:"totalCacheCreateTokens"`
}

type Options struct {
	Dir string
	Now time.Time
}

func DefaultDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("BOTMUX_USAGE_DIR")); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".botmux", "usage"), nil
}

func Append(ctx context.Context, usage Usage, opts Options) (*Record, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(usage.SessionID) == "" {
		return nil, "", nil
	}
	if !hasPositiveDelta(usage) {
		return nil, "", nil
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()

	dir := opts.Dir
	if strings.TrimSpace(dir) == "" {
		var err error
		dir, err = DefaultDir()
		if err != nil {
			return nil, "", err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create usage ledger dir: %w", err)
	}

	record := Record{
		Version:                1,
		RecordID:               recordID(usage),
		Timestamp:              now.Format(time.RFC3339Nano),
		Epoch:                  0,
		SessionID:              usage.SessionID,
		CLIID:                  CLIID,
		WorkingDir:             usage.WorkingDir,
		Model:                  usage.Model,
		InputTokens:            usage.InputTokens,
		OutputTokens:           usage.OutputTokens,
		CacheReadTokens:        usage.CacheReadTokens,
		CacheCreateTokens:      usage.CacheCreateTokens,
		TotalInputTokens:       usage.TotalInputTokens,
		TotalOutputTokens:      usage.TotalOutputTokens,
		TotalCacheReadTokens:   usage.TotalCacheReadTokens,
		TotalCacheCreateTokens: usage.TotalCacheCreateTokens,
	}

	line, err := json.Marshal(record)
	if err != nil {
		return nil, "", fmt.Errorf("marshal usage ledger record: %w", err)
	}
	path := filepath.Join(dir, "usage-"+now.Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, "", fmt.Errorf("open usage ledger: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return nil, "", fmt.Errorf("append usage ledger: %w", err)
	}
	return &record, path, nil
}

func hasPositiveDelta(usage Usage) bool {
	return usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.CacheReadTokens > 0 || usage.CacheCreateTokens > 0
}

func recordID(usage Usage) string {
	h := sha256.New()
	fmt.Fprintf(h, "cece-kaboo-ledger-v1|%s|%s|%s|%d|%d|%d|%d|%d|%d|%d|%d",
		usage.SessionID,
		usage.Model,
		usage.WorkingDir,
		usage.InputTokens,
		usage.OutputTokens,
		usage.CacheReadTokens,
		usage.CacheCreateTokens,
		usage.TotalInputTokens,
		usage.TotalOutputTokens,
		usage.TotalCacheReadTokens,
		usage.TotalCacheCreateTokens,
	)
	return hex.EncodeToString(h.Sum(nil))[:32]
}
