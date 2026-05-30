package harness

import (
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Issaminu/distributed-donut/internal/protocol"
)

// Corruption describes how a worker mangles the results it returns, to exercise
// the server's handling of bad and malicious input.
type Corruption int

const (
	CorruptNone         Corruption = iota
	CorruptWrongSize               // frame batch one byte short — server's NewRenderResult must reject it
	CorruptTruncated               // send only a few bytes of the message
	CorruptOversized               // exceed the server's read limit — server must close the connection
	CorruptGarbage                 // random bytes after a valid type tag
	CorruptUnassignedID            // a well-formed result, but for a task ID never assigned to this worker
)

// WorkerConfig configures a worker's behavior, including deliberate misbehavior.
// The zero value is a well-behaved worker that does not collect broadcasts.
type WorkerConfig struct {
	// Renderer produces frame bytes; defaults to SyntheticRenderer.
	Renderer Renderer
	// RenderDelay is an artificial delay before returning each result. If it
	// exceeds the orchestrator's task timeout, the batch is reassigned.
	RenderDelay time.Duration
	// Silent makes the worker never return results, forcing reassignment.
	Silent bool
	// DropProb is the probability [0,1] of skipping any individual result.
	DropProb float64
	// Corruption mangles returned results.
	Corruption Corruption
	// DisconnectAfter closes the connection just before handling the Nth task
	// (0 = never disconnect).
	DisconnectAfter int
	// CollectBroadcasts records FrameBroadcast payloads so the worker can serve
	// as a viewer for integrity checks.
	CollectBroadcasts bool
}

func (c WorkerConfig) renderer() Renderer {
	if c.Renderer != nil {
		return c.Renderer
	}
	return SyntheticRenderer{}
}

// Worker is a Go stand-in for the browser client: it connects over websocket,
// renders the render tasks it is assigned, and (optionally) collects the frames
// broadcast back to it.
type Worker struct {
	conn     *websocket.Conn
	cfg      WorkerConfig
	renderer Renderer

	tasksReceived  atomic.Int64
	resultsSent    atomic.Int64
	resultsDropped atomic.Int64

	mu       sync.Mutex
	received []byte // concatenated broadcast frame bytes (when CollectBroadcasts)

	notify    chan struct{} // signals new broadcast data to WaitForFrames
	done      chan struct{} // closed when the read loop exits
	closeOnce sync.Once
}

func newWorker(conn *websocket.Conn, cfg WorkerConfig) *Worker {
	return &Worker{
		conn:     conn,
		cfg:      cfg,
		renderer: cfg.renderer(),
		notify:   make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
}

// run is the worker's read loop. The websocket has a single reader (here) and a
// single writer (handleTask, also on this goroutine), so no write locking is
// needed — except for SendRaw, which must only be used on workers that never
// emit results (see SendRaw).
func (w *Worker) run() {
	defer close(w.done)
	for {
		_, data, err := w.conn.ReadMessage()
		if err != nil {
			return
		}
		if len(data) == 0 {
			continue
		}
		switch data[0] {
		case protocol.MessageTypeRenderTask:
			w.handleTask(data[1:])
		case protocol.MessageTypeFrameBroadcast:
			if w.cfg.CollectBroadcasts {
				w.collectBroadcast(data[1:])
			}
		}
	}
}

func (w *Worker) handleTask(body []byte) {
	id, start, end, err := protocol.DecodeRenderTask(body)
	if err != nil {
		return
	}
	n := w.tasksReceived.Add(1)

	if w.cfg.DisconnectAfter > 0 && int(n) >= w.cfg.DisconnectAfter {
		_ = w.conn.Close() // unblocks run() on the next read
		return
	}

	if w.cfg.RenderDelay > 0 {
		time.Sleep(w.cfg.RenderDelay)
	}

	if w.cfg.Silent {
		w.resultsDropped.Add(1)
		return
	}
	if w.cfg.DropProb > 0 && rand.Float64() < w.cfg.DropProb {
		w.resultsDropped.Add(1)
		return
	}

	msg := w.encodeResult(id, w.renderer.Render(start, end))
	if err := w.conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		return
	}
	w.resultsSent.Add(1)
}

func (w *Worker) encodeResult(id uint32, frames []byte) []byte {
	switch w.cfg.Corruption {
	case CorruptWrongSize:
		return protocol.EncodeRenderResult(id, frames[:len(frames)-1])
	case CorruptTruncated:
		return protocol.EncodeRenderResult(id, frames)[:3]
	case CorruptOversized:
		// One byte past the server's read limit (1 + 4 + BatchSize).
		oversize := make([]byte, 1+4+protocol.BatchSize+1)
		oversize[0] = protocol.MessageTypeRenderResult
		return oversize
	case CorruptGarbage:
		junk := make([]byte, 16)
		for i := range junk {
			junk[i] = byte(rand.IntN(256))
		}
		junk[0] = protocol.MessageTypeRenderResult // keep the tag so the server attempts to parse it
		return junk
	case CorruptUnassignedID:
		return protocol.EncodeRenderResult(id+1_000_000, frames)
	default:
		return protocol.EncodeRenderResult(id, frames)
	}
}

func (w *Worker) collectBroadcast(payload []byte) {
	w.mu.Lock()
	w.received = append(w.received, payload...)
	w.mu.Unlock()
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

// SendRaw writes an arbitrary binary message straight to the server, bypassing
// the protocol — for adversarial and fuzz testing. It must only be called on a
// worker that never emits results of its own (e.g. Silent), otherwise it races
// the read loop's writer. For full control prefer Cluster.DialRaw.
func (w *Worker) SendRaw(data []byte) error {
	return w.conn.WriteMessage(websocket.BinaryMessage, data)
}

// FrameCount returns how many whole frames have been collected from broadcasts.
func (w *Worker) FrameCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.received) / protocol.FrameSize
}

// ReceivedFrames returns a copy of all broadcast frame bytes collected so far.
func (w *Worker) ReceivedFrames() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.received...)
}

// WaitForFrames blocks until at least n frames have been collected or timeout
// elapses, returning the frame count reached and an error on timeout/close.
func (w *Worker) WaitForFrames(n int, timeout time.Duration) (int, error) {
	deadline := time.After(timeout)
	for {
		if c := w.FrameCount(); c >= n {
			return c, nil
		}
		select {
		case <-w.notify:
		case <-deadline:
			return w.FrameCount(), fmt.Errorf("timed out waiting for %d frames (have %d)", n, w.FrameCount())
		case <-w.done:
			if c := w.FrameCount(); c >= n {
				return c, nil
			}
			return w.FrameCount(), fmt.Errorf("connection closed with %d frames, wanted %d", w.FrameCount(), n)
		}
	}
}

// TasksReceived reports how many render tasks the worker has been assigned.
func (w *Worker) TasksReceived() int64 { return w.tasksReceived.Load() }

// ResultsSent reports how many results the worker has returned.
func (w *Worker) ResultsSent() int64 { return w.resultsSent.Load() }

// ResultsDropped reports how many assigned tasks the worker deliberately
// skipped (Silent or DropProb).
func (w *Worker) ResultsDropped() int64 { return w.resultsDropped.Load() }

// Close disconnects the worker and waits for its read loop to exit.
func (w *Worker) Close() {
	w.closeOnce.Do(func() { _ = w.conn.Close() })
	<-w.done
}
