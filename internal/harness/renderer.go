// Package harness is an in-process end-to-end test/benchmark rig for the
// distributed-donut server. It stands up the real server + orchestrator over a
// loopback websocket and drives it with Go "workers" that stand in for the
// browser client — including workers that misbehave (slow, silent, flaky,
// malformed, malicious) so the system's fault tolerance can be exercised.
//
// The package is deliberately testing-agnostic (it does not import testing) so
// the same rig backs both integration tests and benchmarks.
package harness

import (
	"encoding/binary"
	"fmt"

	"github.com/Issaminu/distributed-donut/internal/protocol"
)

// Renderer produces the encoded frame bytes for a render task covering frames
// [startFrame, endFrame]. The result must be exactly
// (endFrame-startFrame+1)*protocol.FrameSize bytes.
type Renderer interface {
	Render(startFrame, endFrame uint32) []byte
}

// SyntheticRenderer produces deterministic, watermarked frames cheaply. Each
// frame's first 4 bytes hold its frame number (big-endian) and the remaining
// bytes are filled from that number. This lets a viewer verify, byte for byte,
// that the frames broadcast back are exactly the ones requested, in order — an
// end-to-end integrity check that real donut math could not provide.
//
// It assumes a batch does not straddle the ring's wrap point (endFrame >=
// startFrame), which holds for any test/benchmark running well under
// buffer.MaxFrames frames.
type SyntheticRenderer struct{}

func (SyntheticRenderer) Render(startFrame, endFrame uint32) []byte {
	n := int(endFrame-startFrame) + 1
	out := make([]byte, n*protocol.FrameSize)
	for i := range n {
		frame := startFrame + uint32(i)
		writeFrame(out[i*protocol.FrameSize:(i+1)*protocol.FrameSize], frame)
	}
	return out
}

// writeFrame stamps a single FrameSize-byte frame with its frame number.
func writeFrame(frame []byte, n uint32) {
	binary.BigEndian.PutUint32(frame[0:4], n)
	fill := byte(n)
	for i := 4; i < len(frame); i++ {
		frame[i] = fill
	}
}

// frameWatermark reads the frame number stamped into a FrameSize-byte frame.
func frameWatermark(frame []byte) uint32 {
	return binary.BigEndian.Uint32(frame[0:4])
}

// VerifyContiguousFrames checks that stream holds at least n frames and that
// frame j (0-indexed) carries watermark j with a matching body — i.e. every
// frame 0..n-1 was rendered, stored at the right slot, and broadcast in order,
// intact. It only makes sense for streams produced by SyntheticRenderer-backed
// workers.
func VerifyContiguousFrames(stream []byte, n int) error {
	if have := len(stream) / protocol.FrameSize; have < n {
		return fmt.Errorf("stream has %d frames, want at least %d", have, n)
	}
	for j := range n {
		frame := stream[j*protocol.FrameSize : (j+1)*protocol.FrameSize]
		if got := frameWatermark(frame); got != uint32(j) {
			return fmt.Errorf("frame %d has watermark %d, want %d (out-of-order or missing frame)", j, got, j)
		}
		fill := byte(j)
		for k := 4; k < len(frame); k++ {
			if frame[k] != fill {
				return fmt.Errorf("frame %d body corrupted at byte %d: got %#x, want %#x", j, k, frame[k], fill)
			}
		}
	}
	return nil
}
