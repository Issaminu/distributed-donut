package harness_test

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/Issaminu/distributed-donut/internal/harness"
	"github.com/Issaminu/distributed-donut/internal/orchestrator"
	"github.com/Issaminu/distributed-donut/internal/protocol"
)

// TestMain silences the server's production logging for the whole test binary.
// Even with the hot-path spam gone, the server still logs per-connection and
// per-reassignment events; under a benchmark's load those serialize goroutines
// through the log handler and bury the results. (This also quiets the e2e tests.)
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// These benchmarks drive the real server + orchestrator over a loopback
// websocket, so they measure the coordination machinery end to end: dispatch
// round-trips, the buffer copies, broadcast compression, and websocket I/O.
//
// Two caveats on interpretation:
//   - The pacing is turned down (BroadcastInterval = 1ms) so the bottleneck is
//     the machinery, not the production sleeps. These numbers are a ceiling on
//     coordination throughput, not the ~60fps the server deliberately paces to.
//   - Workers render with the SyntheticRenderer, whose frames are cheap to
//     produce and highly compressible. Real donut frames cost more to render
//     (client-side anyway) and compress ~4.8x rather than ~∞, so production
//     broadcast-compression cost is higher than what shows up here. This
//     isolates the server's overhead, which is the point.

// benchCluster tunes a cluster for throughput measurement. width is how many
// batches the dispatcher fans out per round; set it to the worker count so
// every worker can be busy at once.
func benchCluster(width int) *harness.Cluster {
	return harness.NewCluster(harness.WithOrchestratorOptions(
		orchestrator.WithBroadcastThresholds(width, width),
		orchestrator.WithBroadcastInterval(time.Millisecond),
		orchestrator.WithTaskTimeout(time.Second), // honest workers respond in microseconds; never hit
	))
}

// benchTimeout is a generous safety net for delivering n frames; a real stall
// is caught by `go test`'s overall -timeout rather than here.
func benchTimeout(n int) time.Duration {
	return time.Duration(n)*time.Millisecond + 15*time.Second
}

// BenchmarkThroughput measures end-to-end frame delivery as workers (and the
// dispatch width with them) scale up. ns/op is the per-frame steady-state time;
// the frames/s metric is the headline.
func BenchmarkThroughput(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			c := benchCluster(workers)
			defer c.Close()

			// One worker doubles as the viewer (it records broadcasts); the rest
			// are plain renderers. All `workers` of them render.
			viewer, err := c.Connect(harness.WorkerConfig{CollectBroadcasts: true})
			if err != nil {
				b.Fatalf("connect viewer: %v", err)
			}
			defer viewer.Close()
			for range workers - 1 {
				w, err := c.Connect(harness.WorkerConfig{})
				if err != nil {
					b.Fatalf("connect worker: %v", err)
				}
				defer w.Close()
			}
			if err := c.WaitForClientCount(workers, 5*time.Second); err != nil {
				b.Fatal(err)
			}

			// Warm past the first-broadcast burst so we measure steady state.
			if _, err := viewer.WaitForFrames(2*workers*protocol.FramesPerBatch, 30*time.Second); err != nil {
				b.Fatalf("warm-up: %v", err)
			}

			start := viewer.FrameCount()
			b.ResetTimer()
			if _, err := viewer.WaitForFrames(start+b.N, benchTimeout(b.N)); err != nil {
				b.Fatalf("waiting for %d frames: %v", b.N, err)
			}
			b.StopTimer()

			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "frames/s")
		})
	}
}

// BenchmarkThroughputUnderChurn measures the cost of fault tolerance: a fixed
// fleet in which some workers randomly drop results, forcing the orchestrator
// to time out and reassign those batches. Compared against BenchmarkThroughput
// at the same worker count, the gap is what reassignment costs.
func BenchmarkThroughputUnderChurn(b *testing.B) {
	const workers = 8
	for _, dropProb := range []float64{0, 0.25, 0.5} {
		b.Run(fmt.Sprintf("drop=%.0f%%", dropProb*100), func(b *testing.B) {
			// A short task timeout keeps each reassignment cheap; honest results
			// still come back far faster than this.
			c := harness.NewCluster(harness.WithOrchestratorOptions(
				orchestrator.WithBroadcastThresholds(workers, workers),
				orchestrator.WithBroadcastInterval(time.Millisecond),
				orchestrator.WithTaskTimeout(25*time.Millisecond),
			))
			defer c.Close()

			// The viewer is always honest, so every frame can be sourced from
			// someone; the remaining workers are flaky.
			viewer, err := c.Connect(harness.WorkerConfig{CollectBroadcasts: true})
			if err != nil {
				b.Fatalf("connect viewer: %v", err)
			}
			defer viewer.Close()
			for range workers - 1 {
				w, err := c.Connect(harness.WorkerConfig{DropProb: dropProb})
				if err != nil {
					b.Fatalf("connect worker: %v", err)
				}
				defer w.Close()
			}
			if err := c.WaitForClientCount(workers, 5*time.Second); err != nil {
				b.Fatal(err)
			}

			if _, err := viewer.WaitForFrames(2*workers*protocol.FramesPerBatch, 30*time.Second); err != nil {
				b.Fatalf("warm-up: %v", err)
			}

			start := viewer.FrameCount()
			b.ResetTimer()
			if _, err := viewer.WaitForFrames(start+b.N, benchTimeout(b.N)); err != nil {
				b.Fatalf("waiting for %d frames: %v", b.N, err)
			}
			b.StopTimer()

			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "frames/s")
		})
	}
}
