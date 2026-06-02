package logger

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"
	"time"
)

const location = "Asia/Shanghai"

var (
	file      *os.File
	humanFile *os.File
	bufWriter *bufio.Writer
	humanBuf  *bufio.Writer
	sessionID atomic.Value
)

func generateSessionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Init initializes the global logger with dual output:
//   - JSON format to path (e.g. .cece/cece.log)
//   - Human-friendly format to {dir}/cece-human.log
//
// debug=true enables all levels; otherwise INFO and above.
func Init(path string, debug bool) error {
	loc, err := time.LoadLocation(location)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// JSON log file
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	file = f
	bufWriter = bufio.NewWriter(f)

	// Human-friendly log file
	humanPath := filepath.Join(dir, "cece-human.log")
	hf, err := os.OpenFile(humanPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	humanFile = hf
	humanBuf = bufio.NewWriter(hf)

	jsonHandler := newUTC8Handler(bufWriter, loc, debug, func() { bufWriter.Flush() })
	humanHandler := newHumanHandler(humanBuf, loc, debug, func() { humanBuf.Flush() })

	SetSessionID(generateSessionID())
	tee := &sessionHandler{next: &teeHandler{a: jsonHandler, b: humanHandler}}
	slog.SetDefault(slog.New(tee))

	return nil
}

// Sync flushes buffers and closes log files. Call before program exit.
func Sync() {
	if bufWriter != nil {
		bufWriter.Flush()
	}
	if humanBuf != nil {
		humanBuf.Flush()
	}
	if file != nil {
		file.Sync()
		file.Close()
	}
	if humanFile != nil {
		humanFile.Sync()
		humanFile.Close()
	}
}

func SetSessionID(id string) { sessionID.Store(id) }

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
	r.AddAttrs(slog.String("session_id", id))
	return h.next.Handle(ctx, r)
}

func (h *sessionHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sessionHandler{next: h.next.WithAttrs(attrs)}
}

func (h *sessionHandler) WithGroup(name string) slog.Handler {
	return &sessionHandler{next: h.next.WithGroup(name)}
}

// teeHandler dispatches each log record to two handlers.
type teeHandler struct {
	a, b slog.Handler
}

func (t *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return t.a.Enabled(ctx, level) || t.b.Enabled(ctx, level)
}

func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	if t.a.Enabled(ctx, r.Level) {
		if err := t.a.Handle(ctx, r); err != nil {
			return err
		}
	}
	if t.b.Enabled(ctx, r.Level) {
		if err := t.b.Handle(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{a: t.a.WithAttrs(attrs), b: t.b.WithAttrs(attrs)}
}

func (t *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{a: t.a.WithGroup(name), b: t.b.WithGroup(name)}
}

// utc8Handler wraps slog.JSONHandler, converting timestamps to UTC+8.
type utc8Handler struct {
	*slog.JSONHandler
	loc   *time.Location
	flush func()
}

func newUTC8Handler(w *bufio.Writer, loc *time.Location, debug bool, flush func()) *utc8Handler {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey {
				if s, ok := a.Value.Any().(*slog.Source); ok {
					a.Value = slog.StringValue(filepath.Base(s.File) + ":" + fmt.Sprintf("%d", s.Line))
				}
			}
			return a
		},
	}
	return &utc8Handler{
		JSONHandler: slog.NewJSONHandler(w, opts),
		loc:         loc,
		flush:       flush,
	}
}

func (h *utc8Handler) Handle(ctx context.Context, r slog.Record) error {
	r.Time = r.Time.In(h.loc)
	err := h.JSONHandler.Handle(ctx, r)
	if h.flush != nil {
		h.flush()
	}
	return err
}
