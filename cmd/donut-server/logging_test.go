package main

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestHandler returns a sampling handler writing to buf, plus the buffer, at
// debug level with the given sample interval.
func newTestHandler(interval time.Duration) (*samplingHandler, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	base := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return &samplingHandler{base: base, sampler: &sampler{interval: interval, last: make(map[string]time.Time)}}, buf
}

func countLines(buf *bytes.Buffer) int {
	s := strings.TrimSpace(buf.String())
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// Debug records with the same message must be throttled to one per interval,
// while info and above always pass through.
func TestSamplingHandlerThrottlesDebug(t *testing.T) {
	h, buf := newTestHandler(time.Hour) // long interval => only the first of each msg passes
	log := slog.New(h)

	for range 100 {
		log.Debug("hot path")
	}
	if got := countLines(buf); got != 1 {
		t.Errorf("repeated debug emitted %d lines, want 1 (sampled)", got)
	}

	buf.Reset()
	for range 100 {
		log.Info("important")
		log.Warn("careful")
		log.Error("broken")
	}
	if got := countLines(buf); got != 300 {
		t.Errorf("info/warn/error emitted %d lines, want 300 (never sampled)", got)
	}
}

// Distinct debug messages get independent throttles.
func TestSamplingHandlerThrottlesPerMessage(t *testing.T) {
	h, buf := newTestHandler(time.Hour)
	log := slog.New(h)

	for range 50 {
		log.Debug("alpha")
		log.Debug("beta")
	}
	if got := countLines(buf); got != 2 {
		t.Errorf("two distinct debug messages emitted %d lines, want 2", got)
	}
}

// A non-positive interval disables sampling entirely.
func TestSamplingHandlerDisabledWhenIntervalZero(t *testing.T) {
	h, buf := newTestHandler(0)
	log := slog.New(h)

	for range 10 {
		log.Debug("hot path")
	}
	if got := countLines(buf); got != 10 {
		t.Errorf("with sampling disabled, emitted %d lines, want 10", got)
	}
}

// The throttle state is shared across handlers derived via WithAttrs/WithGroup,
// so adding context attributes does not defeat sampling.
func TestSamplingHandlerSharesStateAcrossDerivations(t *testing.T) {
	h, buf := newTestHandler(time.Hour)
	root := slog.New(h)
	derived := root.With("client", 7)

	root.Debug("hot path")
	derived.Debug("hot path") // same message, derived handler — must still be throttled
	if got := countLines(buf); got != 1 {
		t.Errorf("derived handler bypassed sampling: %d lines, want 1", got)
	}
}

// The sampler is touched from many goroutines (every hot-path log calls it), so
// allow must be safe under concurrency (meaningfully checked under -race).
func TestSamplerConcurrentAllow(t *testing.T) {
	s := &sampler{interval: time.Millisecond, last: make(map[string]time.Time)}
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.allow("msg")
		}()
	}
	wg.Wait()
}
