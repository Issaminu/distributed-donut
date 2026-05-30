package harness_test

import (
	"errors"
	"math/rand/v2"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Issaminu/distributed-donut/internal/harness"
	"github.com/Issaminu/distributed-donut/internal/orchestrator"
	"github.com/Issaminu/distributed-donut/internal/protocol"
)

// waitTimeout is deliberately generous: these tests assert correctness, not
// speed, and the race detector slows everything down.
const waitTimeout = 15 * time.Second

func newCluster(t *testing.T, opts ...harness.ClusterOption) *harness.Cluster {
	t.Helper()
	c := harness.NewCluster(opts...)
	t.Cleanup(c.Close)
	return c
}

func connect(t *testing.T, c *harness.Cluster, cfg harness.WorkerConfig) *harness.Worker {
	t.Helper()
	w, err := c.Connect(cfg)
	if err != nil {
		t.Fatalf("connect worker: %v", err)
	}
	t.Cleanup(w.Close)
	return w
}

// connectViewer connects an honest worker that records the frames broadcast
// back to it, so a test can verify what the whole pipeline produced.
func connectViewer(t *testing.T, c *harness.Cluster) *harness.Worker {
	t.Helper()
	return connect(t, c, harness.WorkerConfig{CollectBroadcasts: true})
}

func mustReceiveContiguousFrames(t *testing.T, viewer *harness.Worker, n int) {
	t.Helper()
	if got, err := viewer.WaitForFrames(n, waitTimeout); err != nil {
		t.Fatalf("viewer received %d/%d frames: %v", got, n, err)
	}
	if err := harness.VerifyContiguousFrames(viewer.ReceivedFrames(), n); err != nil {
		t.Fatalf("frame integrity: %v", err)
	}
}

// Happy path: a single honest worker renders the whole stream, and the frames
// broadcast back are exactly frames 0,1,2,... intact.
func TestHappyPathFrameIntegrity(t *testing.T) {
	c := newCluster(t)
	viewer := connectViewer(t, c)
	mustReceiveContiguousFrames(t, viewer, 3*protocol.FramesPerBatch)
}

// Work spread across several workers (multiple batches dispatched per round)
// still reassembles into one correct, ordered stream.
func TestMultipleWorkersDeliverFrames(t *testing.T) {
	c := newCluster(t, harness.WithOrchestratorOptions(
		orchestrator.WithBroadcastThresholds(5, 5),
	))
	viewer := connectViewer(t, c)
	workers := []*harness.Worker{viewer}
	for range 3 {
		workers = append(workers, connect(t, c, harness.WorkerConfig{}))
	}
	if err := c.WaitForClientCount(4, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	const want = 10 * protocol.FramesPerBatch
	mustReceiveContiguousFrames(t, viewer, want)

	var total int64
	for _, w := range workers {
		total += w.ResultsSent()
	}
	if minBatches := int64(want / protocol.FramesPerBatch); total < minBatches {
		t.Errorf("workers sent %d results, want at least %d (one per batch)", total, minBatches)
	}
}

// A worker that never answers must not stall the pipeline: its batches are
// reassigned to a worker that does answer.
func TestSilentWorkerIsReassigned(t *testing.T) {
	c := newCluster(t)
	viewer := connectViewer(t, c)
	silent := connect(t, c, harness.WorkerConfig{Silent: true})
	if err := c.WaitForClientCount(2, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	mustReceiveContiguousFrames(t, viewer, 3*protocol.FramesPerBatch)

	if silent.ResultsSent() != 0 {
		t.Errorf("silent worker sent %d results, want 0", silent.ResultsSent())
	}
}

// A worker that drops its connection mid-stream must not lose frames: its
// outstanding batch is reassigned and the stream stays intact.
func TestWorkerDisconnectRecovery(t *testing.T) {
	c := newCluster(t)
	viewer := connectViewer(t, c)
	connect(t, c, harness.WorkerConfig{DisconnectAfter: 1})
	if err := c.WaitForClientCount(2, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	mustReceiveContiguousFrames(t, viewer, 3*protocol.FramesPerBatch)
}

// The system keeps producing a correct stream even with a worker that returns
// malformed (wrong-size) results.
func TestSystemSurvivesMalformedWorker(t *testing.T) {
	c := newCluster(t)
	viewer := connectViewer(t, c)
	connect(t, c, harness.WorkerConfig{Corruption: harness.CorruptWrongSize})
	if err := c.WaitForClientCount(2, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	mustReceiveContiguousFrames(t, viewer, 3*protocol.FramesPerBatch)
}

// A malicious worker that submits well-formed results for tasks it was never
// assigned must not corrupt the shared buffer — and, because its messages are
// well-formed, it is not disconnected, just ignored.
func TestMaliciousUnassignedResultsIgnored(t *testing.T) {
	c := newCluster(t)
	viewer := connectViewer(t, c)
	connect(t, c, harness.WorkerConfig{Corruption: harness.CorruptUnassignedID})
	if err := c.WaitForClientCount(2, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	mustReceiveContiguousFrames(t, viewer, 3*protocol.FramesPerBatch)

	if got := c.ClientCount(); got != 2 {
		t.Errorf("client count = %d, want 2 (a well-formed but unauthorized result must be ignored, not punished)", got)
	}
}

// Malformed messages must cause the server to close the offending connection
// (and never crash). We read past anything the dispatcher sends us and require
// a genuine connection close (not merely a read timeout).
func TestMalformedMessagesCloseConnection(t *testing.T) {
	c := newCluster(t)

	full := protocol.EncodeRenderResult(0, make([]byte, protocol.BatchSize))
	oversized := make([]byte, 1+4+protocol.BatchSize+1)
	oversized[0] = protocol.MessageTypeRenderResult

	cases := map[string][]byte{
		"empty":      {},
		"wrong size": protocol.EncodeRenderResult(0, make([]byte, protocol.BatchSize-1)),
		"truncated":  full[:3],
		"garbage":    {protocol.MessageTypeRenderResult, 0, 0, 0, 1, 0xff},
		"oversized":  oversized,
	}

	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			conn, err := c.DialRaw()
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer conn.Close()

			if err := conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				t.Fatalf("write: %v", err)
			}
			expectServerClosesConn(t, conn)
		})
	}
}

// An unrecognized message type is ignored, and the connection stays open (the
// server only tears down on a genuine protocol error).
func TestUnknownMessageTypeKeepsConnectionOpen(t *testing.T) {
	c := newCluster(t)
	conn, err := c.DialRaw()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := c.WaitForClientCount(1, 3*time.Second); err != nil {
		t.Fatal(err)
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x7, 1, 2, 3}); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // give the server a chance to (wrongly) react

	if got := c.ClientCount(); got != 1 {
		t.Errorf("client count = %d after an unknown-type message, want 1 (connection should stay open)", got)
	}
}

// Under a continuous flood of random bytes from churning connections, the
// server stays up and an honest viewer still receives a correct, ordered
// stream. The randomness is seeded so failures are reproducible. (Cluster.DialRaw
// is also the entry point for native testing.F fuzzers.)
func TestRandomizedInputResilience(t *testing.T) {
	c := newCluster(t)
	viewer := connectViewer(t, c)

	rng := rand.New(rand.NewPCG(0xD0, 0x47))
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			conn, err := c.DialRaw()
			if err != nil {
				return
			}
			for range rng.IntN(4) + 1 {
				buf := make([]byte, rng.IntN(64))
				for j := range buf {
					buf[j] = byte(rng.IntN(256))
				}
				if err := conn.WriteMessage(websocket.BinaryMessage, buf); err != nil {
					break
				}
			}
			conn.Close()
			time.Sleep(10 * time.Millisecond) // keep the pool from being swamped with junk
		}
	}()

	got, err := viewer.WaitForFrames(3*protocol.FramesPerBatch, waitTimeout)
	close(stop)
	wg.Wait()

	if err != nil {
		t.Fatalf("viewer received %d frames while being flooded with random input: %v", got, err)
	}
	if err := harness.VerifyContiguousFrames(viewer.ReceivedFrames(), 3*protocol.FramesPerBatch); err != nil {
		t.Fatalf("frame integrity under random input flood: %v", err)
	}
}

// expectServerClosesConn reads until the server closes the connection, ignoring
// any messages it sends in the meantime (e.g. a RenderTask). A read timeout is
// treated as a failure: it means the server did NOT close the connection.
func expectServerClosesConn(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	for {
		if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue // ignore whatever the dispatcher sent us; keep reading
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatal("server did not close the connection after a malformed message (read timed out)")
		}
		return // non-timeout error: the server closed the connection, as expected
	}
}
