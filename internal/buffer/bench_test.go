package buffer

import (
	"fmt"
	"testing"

	"github.com/Issaminu/distributed-donut/internal/protocol"
)

// These exercise the buffer's two hot copies: committing a freshly rendered
// batch (the dispatcher's write path) and snapshotting a window for broadcast
// (the broadcaster's read path). Both are pure memory moves under the buffer
// mutex, so they set b.SetBytes to report throughput in MB/s.

func BenchmarkAddFramesToBuffer(b *testing.B) {
	fb := NewFrameBuffer()
	var data [protocol.BatchSize]byte
	for i := range data {
		data[i] = byte(i)
	}
	b.SetBytes(protocol.BatchSize)
	b.ReportAllocs()
	for range b.N {
		// Always the first batch slot — we're measuring the copy, not ring
		// placement, and head never advances here.
		if err := fb.AddFramesToBuffer(0, protocol.FramesPerBatch-1, &data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetFramesToBroadcast(b *testing.B) {
	// Sized after the orchestrator's real broadcast windows: 1s is the harness
	// default, 4s the production default, 8s a larger catch-up batch.
	for _, seconds := range []int{1, 4, 8} {
		b.Run(fmt.Sprintf("seconds=%d", seconds), func(b *testing.B) {
			fb := NewFrameBuffer()
			fb.AdvanceHead(seconds) // make `seconds` worth of frames readable from the tail
			b.SetBytes(int64(seconds * protocol.BatchSize))
			b.ReportAllocs()
			for range b.N {
				// Reads from the tail without advancing it, so every call copies
				// the same window — exactly the broadcaster's hot read.
				if got := fb.GetFramesToBroadcast(seconds); got == nil {
					b.Fatal("GetFramesToBroadcast returned nil")
				}
			}
		})
	}
}
