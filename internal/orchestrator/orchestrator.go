// Package orchestrator is the central coordinator: it dispatches render tasks
// to connected clients, collects the resulting frame batches into the shared
// buffer, and periodically broadcasts buffered frames back to everyone.
package orchestrator

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Issaminu/distributed-donut/internal/buffer"
	"github.com/Issaminu/distributed-donut/internal/client"
	"github.com/Issaminu/distributed-donut/internal/protocol"
)

// Config tunes the orchestrator's pacing and fault-tolerance timing. The zero
// value is not valid; start from DefaultConfig and adjust via Options.
type Config struct {
	// FirstSecondsToBroadcast is how many seconds of frames to gather before the
	// very first broadcast. Given SecondsToBroadcast, the extra seconds become a
	// playback buffer on the clients.
	FirstSecondsToBroadcast int
	// SecondsToBroadcast is how many seconds of frames each subsequent broadcast
	// (and each dispatch round) covers.
	SecondsToBroadcast int
	// BroadcastInterval paces the pipeline: it bounds the dispatcher's pre-fetch
	// sleep and is the cooldown between broadcasts so clients can consume what
	// they were just sent.
	BroadcastInterval time.Duration
	// TaskTimeout is how long to wait for a client to return a batch before
	// re-dispatching it to another client.
	TaskTimeout time.Duration
	// ClientCountInterval is how often the live count of connected clients is
	// broadcast to the fleet. Zero disables it.
	ClientCountInterval time.Duration
	// BufferFullnessInterval is how often the server ring-buffer fullness
	// percentage is broadcast to the fleet. Zero disables it.
	BufferFullnessInterval time.Duration
}

// DefaultConfig returns the production timing.
func DefaultConfig() Config {
	return Config{
		FirstSecondsToBroadcast: 6,
		SecondsToBroadcast:      4,
		BroadcastInterval:       4 * time.Second,
		TaskTimeout:             2 * time.Second,
		ClientCountInterval:     2 * time.Second,
		BufferFullnessInterval:  2 * time.Second,
	}
}

// Option mutates a Config; pass Options to New to override the defaults.
type Option func(*Config)

// WithTaskTimeout sets how long a batch may be outstanding before reassignment.
func WithTaskTimeout(d time.Duration) Option {
	return func(c *Config) { c.TaskTimeout = d }
}

// WithBroadcastInterval sets the pipeline pacing/cooldown duration.
func WithBroadcastInterval(d time.Duration) Option {
	return func(c *Config) { c.BroadcastInterval = d }
}

// WithBroadcastThresholds sets how many seconds of frames the first and
// subsequent broadcasts cover.
func WithBroadcastThresholds(first, subsequent int) Option {
	return func(c *Config) {
		c.FirstSecondsToBroadcast = first
		c.SecondsToBroadcast = subsequent
	}
}

// WithClientCountInterval sets how often the connected-client count is broadcast
// to the fleet. Zero disables the broadcast.
func WithClientCountInterval(d time.Duration) Option {
	return func(c *Config) { c.ClientCountInterval = d }
}

// WithBufferFullnessInterval sets how often the ring-buffer fullness percentage
// is broadcast to the fleet. Zero disables the broadcast.
func WithBufferFullnessInterval(d time.Duration) Option {
	return func(c *Config) { c.BufferFullnessInterval = d }
}

// Orchestrator owns the rendering pipeline. Construct it with New, then call Run to start its background loops.
type Orchestrator struct {
	buffer   *buffer.FrameBuffer
	clients  *client.ClientPool
	batchMap *FrameBatchMap
	cfg      Config
}

func New(buf *buffer.FrameBuffer, pool *client.ClientPool, opts ...Option) *Orchestrator {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Orchestrator{
		buffer:   buf,
		clients:  pool,
		batchMap: NewFrameBatchMap(buf),
		cfg:      cfg,
	}
}

// Run starts the broadcaster, task-dispatcher, and telemetry loops. They stop
// when ctx is cancelled.
func (o *Orchestrator) Run(ctx context.Context) {
	go o.broadcaster(ctx)
	go o.dispatcher(ctx)
	go o.telemetryBroadcaster(ctx)
}

// HandleResult stores a render result a client sent back. Its signature
// satisfies client.ResultHandler, so it can be wired directly into NewClient.
func (o *Orchestrator) HandleResult(clientID uint32, result *protocol.RenderResult) error {
	return o.batchMap.SaveRenderResult(clientID, result)
}

func (o *Orchestrator) broadcaster(ctx context.Context) {
	var isFirstBroadcast = true
	for {
		select {
		case <-ctx.Done():
			slog.Info("frame broadcaster shutting down")
			return
		default:
			seconds := o.cfg.SecondsToBroadcast
			if isFirstBroadcast {
				seconds = o.cfg.FirstSecondsToBroadcast
			}

			o.buffer.WaitUntilBufferSizeEnoughForBroadcast(seconds)
			o.clients.WaitForAtLeastOne() // No point broadcasting into the void

			frames := o.buffer.GetFramesToBroadcast(seconds)
			o.clients.Broadcast(frames)
			o.buffer.RemoveSentFramesFromBuffer(seconds)
			// Hot-path detail, sampled so it doesn't flood the logs (see the
			// sampling handler in cmd/donut-server). Only emitted at debug level.
			slog.Debug("broadcast frames", "frames", len(frames)/protocol.FrameSize, "clients", o.clients.GetClientCount())
			if isFirstBroadcast {
				isFirstBroadcast = false
			}
			time.Sleep(o.cfg.BroadcastInterval) // Sleep before allowing the next broadcast, so clients consume what we just sent
		}
	}
}

func (o *Orchestrator) dispatcher(ctx context.Context) {
	isFirstRound := true // the first round prefetches more, see batchesToFetch below

	for {
		select {
		case <-ctx.Done():
			slog.Info("task dispatcher shutting down")
			return
		default:
			batchesToFetch := o.cfg.SecondsToBroadcast
			if isFirstRound {
				batchesToFetch = o.cfg.FirstSecondsToBroadcast
			}

			sleep := o.buffer.WaitForRoom(batchesToFetch, o.cfg.BroadcastInterval)
			o.clients.WaitForAtLeastOne() // no point dispatching with no workers connected
			time.Sleep(sleep)

			var currentFrame = uint32(o.buffer.GetNextFrameNumber())

			var wg sync.WaitGroup
			for range batchesToFetch {
				// Select a random client to do the work needed for the current batch
				startFrame := (currentFrame) % buffer.MaxFrames
				endFrame := (currentFrame + protocol.FramesPerBatch - 1) % buffer.MaxFrames
				currentFrame += protocol.FramesPerBatch
				wg.Add(1)
				go func(start, end uint32) {
					defer wg.Done()
					o.dispatchRenderTask(start, end)
				}(startFrame, endFrame)
			}
			wg.Wait() // Wait for every task batch to finish in this round before broadcasting

			o.buffer.AdvanceHead(batchesToFetch)
			// Hot-path detail, sampled (debug level only) like the broadcast log.
			slog.Debug("dispatched round", "batches", batchesToFetch, "fullness_pct", o.buffer.FullnessPercent())
			isFirstRound = false
		}
	}
}

// telemetryBroadcaster periodically pushes server-side stats to the whole fleet:
// the live count of connected clients and the ring-buffer fullness percentage,
// each on its own configurable interval. An interval of zero disables that
// stat (its ticker channel stays nil, which blocks forever in the select).
func (o *Orchestrator) telemetryBroadcaster(ctx context.Context) {
	var clientCountC, fullnessC <-chan time.Time
	if o.cfg.ClientCountInterval > 0 {
		t := time.NewTicker(o.cfg.ClientCountInterval)
		defer t.Stop()
		clientCountC = t.C
	}
	if o.cfg.BufferFullnessInterval > 0 {
		t := time.NewTicker(o.cfg.BufferFullnessInterval)
		defer t.Stop()
		fullnessC = t.C
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("telemetry broadcaster shutting down")
			return
		case <-clientCountC:
			o.clients.BroadcastClientCount(uint32(o.clients.GetClientCount()))
		case <-fullnessC:
			o.clients.BroadcastBufferFullness(o.buffer.FullnessPercent())
		}
	}
}

// dispatchRenderTask assigns one batch to a client and blocks until that batch has been rendered and stored.
// It returns as soon as the client responds (via the task's done channel).
// If no response arrives within taskTimeout it re-dispatches to a different client when one is available.
// dispatchRenderTask never gives up since abandoning a batch would let AdvanceHead advance over an unwritten slot.
func (o *Orchestrator) dispatchRenderTask(startFrame uint32, endFrame uint32) {
	worker := o.clients.PickNewClient()
	renderTaskID := worker.GenerateNewRenderTaskID()
	frameBatch := NewFrameBatchMetadata(renderTaskID, startFrame, endFrame)
	o.batchMap.AddFrameBatch(worker.ID(), frameBatch)
	done := frameBatch.done // same channel travels with the task across executor switches

	worker.RequestWork(renderTaskID, startFrame, endFrame)

	for attempt := 1; ; attempt++ {
		select {
		case <-done:
			o.batchMap.DeleteRenderTask(worker.ID(), renderTaskID)
			return
		case <-time.After(o.cfg.TaskTimeout): // timeout exceeded, picking new client
			next := o.clients.PickNewClient()
			if next.ID() == worker.ID() {
				slog.Warn("render task timed out, retrying with the same client", "attempt", attempt, "client", worker.ID())
			} else {
				slog.Warn("render task timed out, switching executor", "attempt", attempt, "from_client", worker.ID(), "to_client", next.ID())
				renderTaskID = o.batchMap.SwitchRenderTaskExecutor(renderTaskID, worker.ID(), next)
				worker = next
			}
			worker.RequestWork(renderTaskID, startFrame, endFrame)
		}
	}
}
