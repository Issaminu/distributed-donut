package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// newLogger builds the process logger: a text handler at the given level,
// wrapped so that debug records (the high-volume hot-path detail) are sampled to
// at most one per sampleInterval per distinct message. Info/Warn/Error always
// pass through unthrottled. A sampleInterval <= 0 disables sampling.
func newLogger(level slog.Level, sampleInterval time.Duration) *slog.Logger {
	base := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	return slog.New(&samplingHandler{
		base:    base,
		sampler: &sampler{interval: sampleInterval, last: make(map[string]time.Time)},
	})
}

// parseLevel maps a level name (debug/info/warn/error, case-insensitive) to its
// slog.Level, falling back to info for anything unrecognized.
func parseLevel(name string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// sampler rate-limits messages by their text, so a hot-path debug line emitted
// thousands of times a second is logged at most once per interval. The state is
// shared (by pointer) across handlers derived via WithAttrs/WithGroup.
type sampler struct {
	mu       sync.Mutex
	interval time.Duration
	last     map[string]time.Time
}

func (s *sampler) allow(msg string) bool {
	if s.interval <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if t, ok := s.last[msg]; ok && now.Sub(t) < s.interval {
		return false
	}
	s.last[msg] = now
	return true
}

// samplingHandler wraps a base slog.Handler and drops debug records that exceed
// the sampler's rate. Records at info level and above are never dropped.
type samplingHandler struct {
	base    slog.Handler
	sampler *sampler
}

func (h *samplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

func (h *samplingHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level <= slog.LevelDebug && !h.sampler.allow(r.Message) {
		return nil
	}
	return h.base.Handle(ctx, r)
}

func (h *samplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &samplingHandler{base: h.base.WithAttrs(attrs), sampler: h.sampler}
}

func (h *samplingHandler) WithGroup(name string) slog.Handler {
	return &samplingHandler{base: h.base.WithGroup(name), sampler: h.sampler}
}
