package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

const maxAttrValueLen = 1000

// humanHandler implements slog.Handler with a human-friendly text format.
//
// Output format:
//
//	2026-05-21 14:30:00.123 INFO  stream request url=https://api.anthropic.com/v1/messages
type humanHandler struct {
	w     io.Writer
	loc   *time.Location
	level slog.Leveler
	flush func()
	opts  *slog.HandlerOptions
}

func newHumanHandler(w io.Writer, loc *time.Location, debug bool, flush func()) *humanHandler {
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
					a.Value = slog.StringValue(fmt.Sprintf("%s:%d", filepath.Base(s.File), s.Line))
				}
			}
			return a
		},
	}
	return &humanHandler{w: w, loc: loc, level: level, flush: flush, opts: opts}
}

func (h *humanHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *humanHandler) Handle(_ context.Context, r slog.Record) error {
	ts := r.Time.In(h.loc).Format("2006-01-02 15:04:05.000")
	level := levelString(r.Level)

	// Extract session_id from attrs to use as prefix instead of trailing attr.
	var sessionID string
	var remainingAttrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "session_id" {
			sessionID = a.Value.String()
			return true
		}
		remainingAttrs = append(remainingAttrs, a)
		return true
	})

	var b strings.Builder
	b.WriteString(ts)
	b.WriteByte(' ')
	b.WriteString(level)
	b.WriteByte(' ')
	if sessionID != "" {
		b.WriteString("[")
		b.WriteString(sessionID)
		b.WriteString("] ")
	}
	b.WriteString(r.Message)

	for _, a := range remainingAttrs {
		b.WriteByte(' ')
		b.WriteString(formatAttr(a))
	}

	b.WriteByte('\n')

	if _, err := io.WriteString(h.w, b.String()); err != nil {
		return err
	}
	if h.flush != nil {
		h.flush()
	}
	return nil
}

func (h *humanHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Not needed for our use case — we create a fresh handler per log call.
	return h
}

func (h *humanHandler) WithGroup(name string) slog.Handler {
	return h
}

func levelString(l slog.Level) string {
	switch {
	case l < slog.LevelDebug:
		return "DEBUG"
	case l < slog.LevelInfo:
		return "DEBUG"
	case l < slog.LevelWarn:
		return "INFO "
	case l < slog.LevelError:
		return "WARN "
	default:
		return "ERROR"
	}
}

func formatAttr(a slog.Attr) string {
	key := a.Key
	val := formatValue(a.Value)
	if strings.ContainsAny(val, " \t\n") {
		return fmt.Sprintf("%s=%q", key, val)
	}
	return fmt.Sprintf("%s=%s", key, val)
}

func formatValue(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		if len(s) > maxAttrValueLen {
			return s[:maxAttrValueLen] + "..."
		}
		return s
	case slog.KindInt64:
		return fmt.Sprintf("%d", v.Int64())
	case slog.KindFloat64:
		return fmt.Sprintf("%f", v.Float64())
	case slog.KindBool:
		return fmt.Sprintf("%t", v.Bool())
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	case slog.KindAny:
		return fmt.Sprintf("%v", v.Any())
	default:
		return fmt.Sprintf("%v", v.Any())
	}
}
