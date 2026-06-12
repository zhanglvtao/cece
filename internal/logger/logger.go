package logger

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

const location = "Asia/Shanghai"
const maxLogSize int64 = 512 * 1024 * 1024 // 512MB

var (
	mu        sync.Mutex
	logDir    string
	curFile   *os.File
	humanBuf  *bufio.Writer
	sessionID atomic.Value
)

func generateSessionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func logFilePath(dir string) string {
	ts := time.Now().Format("20060102150405")
	return filepath.Join(dir, fmt.Sprintf("cece-%s.log", ts))
}

// Init initializes the global logger with human-friendly output.
// logDir is the directory for log files (e.g. .cece/log/).
// Each session creates a timestamped file like cece-20260612115032.log.
// debug=true enables all levels; otherwise INFO and above.
func Init(dir string, debug bool) error {
	loc, err := time.LoadLocation(location)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Rotate old logs on startup
	cleanOldLogs(dir)

	logDir = dir
	path := logFilePath(dir)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	curFile = f
	humanBuf = bufio.NewWriter(f)

	flushFn := func() {
		humanBuf.Flush()
		rotateIfNeeded()
	}

	humanHandler := newHumanHandler(humanBuf, loc, debug, flushFn)

	SetSessionID(generateSessionID())
	tee := &sessionHandler{next: humanHandler}
	slog.SetDefault(slog.New(tee))

	return nil
}

// rotateIfNeeded checks if the current log file exceeds maxLogSize.
// If so, it closes the current file and opens a new timestamped one.
func rotateIfNeeded() {
	mu.Lock()
	defer mu.Unlock()

	if curFile == nil {
		return
	}

	info, err := curFile.Stat()
	if err != nil {
		return
	}
	if info.Size() < maxLogSize {
		return
	}

	// Flush and close current file
	humanBuf.Flush()
	curFile.Sync()
	curFile.Close()

	// Open new file
	path := logFilePath(logDir)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	curFile = f
	humanBuf.Reset(f)

	// Clean old logs after rotation
	cleanOldLogs(logDir)
}

// cleanOldLogs removes the oldest log files when total size exceeds maxLogSize.
func cleanOldLogs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type fileEntry struct {
		name string
		size int64
	}
	var files []fileEntry
	var totalSize int64

	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "cece-") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{name: e.Name(), size: info.Size()})
		totalSize += info.Size()
	}

	if totalSize <= maxLogSize || len(files) == 0 {
		return
	}

	// Sort by name ascending (oldest timestamp first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].name < files[j].name
	})

	// Delete oldest files until total size ≤ maxLogSize
	for i := 0; i < len(files) && totalSize > maxLogSize; i++ {
		os.Remove(filepath.Join(dir, files[i].name))
		totalSize -= files[i].size
	}
}

// Sync flushes buffers and closes the log file. Call before program exit.
func Sync() {
	mu.Lock()
	defer mu.Unlock()

	if humanBuf != nil {
		humanBuf.Flush()
	}
	if curFile != nil {
		curFile.Sync()
		curFile.Close()
	}
}

func SetSessionID(id string) { sessionID.Store(id) }

func GetSessionID() string {
	id, _ := sessionID.Load().(string)
	return id
}

func LogPath() string {
	mu.Lock()
	defer mu.Unlock()
	if curFile == nil {
		return ""
	}
	return curFile.Name()
}

func Debug(msg string, args ...any) { log(slog.LevelDebug, msg, args...) }
func Info(msg string, args ...any)  { log(slog.LevelInfo, msg, args...) }
func Warn(msg string, args ...any)  { log(slog.LevelWarn, msg, args...) }
func Error(msg string, args ...any) { log(slog.LevelError, msg, args...) }

func log(level slog.Level, msg string, args ...any) {
	l := slog.Default()
	if !l.Enabled(context.Background(), level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	_ = l.Handler().Handle(context.Background(), r)
}

// sessionHandler injects the current session_id into every log record.
type sessionHandler struct {
	next slog.Handler
}

func (h *sessionHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *sessionHandler) Handle(ctx context.Context, r slog.Record) error {
	id, _ := sessionID.Load().(string)
	if len(id) > 8 {
		id = id[:8]
	}
	r.AddAttrs(slog.String("session_id", id))
	return h.next.Handle(ctx, r)
}

func (h *sessionHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sessionHandler{next: h.next.WithAttrs(attrs)}
}

func (h *sessionHandler) WithGroup(name string) slog.Handler {
	return &sessionHandler{next: h.next.WithGroup(name)}
}
