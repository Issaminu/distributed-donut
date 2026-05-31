package harness

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Issaminu/distributed-donut/internal/buffer"
	"github.com/Issaminu/distributed-donut/internal/client"
	"github.com/Issaminu/distributed-donut/internal/orchestrator"
	"github.com/Issaminu/distributed-donut/internal/server"
)

// fastOptions tunes the orchestrator for tests/benchmarks: tiny frame
// thresholds and short pacing so a full render→broadcast cycle takes
// milliseconds instead of the production seconds.
func fastOptions() []orchestrator.Option {
	return []orchestrator.Option{
		orchestrator.WithBroadcastThresholds(1, 1),
		orchestrator.WithBroadcastInterval(10 * time.Millisecond),
		orchestrator.WithTaskTimeout(150 * time.Millisecond),
		// Telemetry broadcasts are off in the harness so they can't interleave
		// with the frame stream the integrity checks rely on.
		orchestrator.WithClientCountInterval(0),
		orchestrator.WithBufferFullnessInterval(0),
	}
}

// Cluster is an in-process server + orchestrator under test, reachable over a
// real loopback websocket. It backs both integration tests and benchmarks.
// Always Close it when done.
type Cluster struct {
	pool   *client.ClientPool
	ts     *httptest.Server
	wsURL  string
	cancel context.CancelFunc
}

type clusterConfig struct {
	orchOptions []orchestrator.Option
}

// ClusterOption customizes a Cluster.
type ClusterOption func(*clusterConfig)

// WithOrchestratorOptions appends orchestrator options on top of the harness's
// fast defaults; later options win, so you can override just the pacing you
// care about (e.g. thresholds) and keep the rest fast.
func WithOrchestratorOptions(opts ...orchestrator.Option) ClusterOption {
	return func(c *clusterConfig) { c.orchOptions = append(c.orchOptions, opts...) }
}

// NewCluster wires up and starts an in-process cluster.
func NewCluster(opts ...ClusterOption) *Cluster {
	cfg := clusterConfig{orchOptions: fastOptions()}
	for _, opt := range opts {
		opt(&cfg)
	}

	buf := buffer.NewFrameBuffer()
	pool := client.NewClientPool()
	orch := orchestrator.New(buf, pool, cfg.orchOptions...)
	srv := server.New(pool, orch.HandleResult, fstest.MapFS{})

	ctx, cancel := context.WithCancel(context.Background())
	orch.Run(ctx)
	ts := httptest.NewServer(srv.Handler(ctx))

	return &Cluster{
		pool:   pool,
		ts:     ts,
		wsURL:  "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws",
		cancel: cancel,
	}
}

// Connect dials a new worker into the cluster and starts its read loop.
func (c *Cluster) Connect(cfg WorkerConfig) (*Worker, error) {
	conn, _, err := websocket.DefaultDialer.Dial(c.wsURL, nil)
	if err != nil {
		return nil, err
	}
	w := newWorker(conn, cfg)
	go w.run()
	return w, nil
}

// DialRaw returns a bare websocket connection with no behavior attached, for
// adversarial and fuzz testing. The caller owns it entirely.
func (c *Cluster) DialRaw() (*websocket.Conn, error) {
	conn, _, err := websocket.DefaultDialer.Dial(c.wsURL, nil)
	return conn, err
}

// ClientCount returns the number of workers currently registered server-side.
func (c *Cluster) ClientCount() int {
	return c.pool.GetClientCount()
}

// WaitForClientCount blocks until the server registers exactly n clients, or
// the timeout elapses.
func (c *Cluster) WaitForClientCount(n int, timeout time.Duration) error {
	deadline := time.After(timeout)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		if c.ClientCount() == n {
			return nil
		}
		select {
		case <-deadline:
			return fmt.Errorf("timed out waiting for %d clients (have %d)", n, c.ClientCount())
		case <-tick.C:
		}
	}
}

// Close shuts the cluster down. Note: because the orchestrator's background
// loops can park on condition variables that aren't context-aware, a couple of
// its goroutines may remain blocked until the process exits — acceptable for
// tests and for a benchmark's single long-lived cluster, but not yet a clean
// teardown (tracked separately).
func (c *Cluster) Close() {
	c.cancel()
	c.ts.Close()
}
