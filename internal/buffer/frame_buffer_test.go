package buffer

import (
	"bytes"
	"testing"
	"time"

	"github.com/Issaminu/distributed-donut/internal/protocol"
)

func TestNewFrameBufferIsEmpty(t *testing.T) {
	fb := NewFrameBuffer()
	if got := fb.GetLengthInFrames(); got != 0 {
		t.Errorf("GetLengthInFrames() = %d, want 0", got)
	}
	if got := fb.GetNextFrameNumber(); got != 0 {
		t.Errorf("GetNextFrameNumber() = %d, want 0", got)
	}
}

// Write one batch, make it available, broadcast it, then drain it — the bytes
// out must equal the bytes in, and the buffer must be empty afterward.
func TestAddAndBroadcastRoundTrip(t *testing.T) {
	fb := NewFrameBuffer()

	var data [protocol.BatchSize]byte
	for i := range data {
		data[i] = byte(i % 251)
	}
	if err := fb.AddFramesToBuffer(0, protocol.FramesPerBatch-1, &data); err != nil {
		t.Fatalf("AddFramesToBuffer: %v", err)
	}
	fb.AdvanceHead(1)

	if got := fb.GetLengthInFrames(); got != protocol.FramesPerBatch {
		t.Fatalf("length = %d, want %d", got, protocol.FramesPerBatch)
	}

	got := fb.GetFramesToBroadcast(1)
	if !bytes.Equal(got, data[:]) {
		t.Fatal("broadcast bytes don't match what was written")
	}

	fb.RemoveSentFramesFromBuffer(1)
	if got := fb.GetLengthInFrames(); got != 0 {
		t.Fatalf("length after removal = %d, want 0", got)
	}
}

func TestGetFramesToBroadcastReturnsNilWhenInsufficient(t *testing.T) {
	fb := NewFrameBuffer()
	if got := fb.GetFramesToBroadcast(1); got != nil {
		t.Fatalf("expected nil from empty buffer, got %d bytes", len(got))
	}
}

// A broadcast read that starts near the end of the ring must stitch the tail
// end and the wrapped-around start into a single contiguous result.
func TestGetFramesToBroadcastWrapsAroundRing(t *testing.T) {
	fb := NewFrameBuffer()

	delta := uint64(protocol.FramesPerBatch * protocol.FrameSize) // one second == BatchSize bytes
	const headRoom = 1000
	fb.tail = BufferSize - headRoom
	fb.head = fb.tail + delta

	tailIdx := fb.tail % BufferSize
	for i := tailIdx; i < BufferSize; i++ {
		fb.buffer[i] = 0xAA // bytes before the wrap
	}
	for i := uint64(0); i < delta-headRoom; i++ {
		fb.buffer[i] = 0xBB // bytes after the wrap
	}

	got := fb.GetFramesToBroadcast(1)
	if uint64(len(got)) != delta {
		t.Fatalf("len = %d, want %d", len(got), delta)
	}
	for i := 0; i < headRoom; i++ {
		if got[i] != 0xAA {
			t.Fatalf("byte %d = %#x, want 0xAA (pre-wrap region)", i, got[i])
		}
	}
	for i := headRoom; uint64(i) < delta; i++ {
		if got[i] != 0xBB {
			t.Fatalf("byte %d = %#x, want 0xBB (post-wrap region)", i, got[i])
		}
	}
}

func TestAddFramesToBufferRejectsStartAfterEnd(t *testing.T) {
	fb := NewFrameBuffer()
	var data [protocol.BatchSize]byte
	if err := fb.AddFramesToBuffer(100, 50, &data); err == nil {
		t.Fatal("expected error when startFrame > endFrame")
	}
}

func TestRemoveSentFramesFromBufferPanicsWhenInsufficient(t *testing.T) {
	fb := NewFrameBuffer()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when removing more frames than are buffered")
		}
	}()
	fb.RemoveSentFramesFromBuffer(1) // buffer is empty
}

func TestTimeToSleep(t *testing.T) {
	fb := NewFrameBuffer()
	const maxSleep = 4 * time.Second

	// Lots of room left (>= MaxBatches remaining) => no need to slow down.
	if got := fb.timeToSleep(MaxBatches, maxSleep); got != 0 {
		t.Errorf("timeToSleep(MaxBatches) = %v, want 0", got)
	}
	if got := fb.timeToSleep(MaxBatches+10, maxSleep); got != 0 {
		t.Errorf("timeToSleep(>MaxBatches) = %v, want 0", got)
	}
	// Buffer full (0 remaining) => pace at the maximum.
	if got := fb.timeToSleep(0, maxSleep); got != maxSleep {
		t.Errorf("timeToSleep(0) = %v, want %v", got, maxSleep)
	}
	// As remaining room shrinks, the pacing delay must grow monotonically and
	// never exceed maxSleep.
	prev := time.Duration(-1)
	for _, remaining := range []uint64{MaxBatches - 1, MaxBatches / 2, MaxBatches / 4, 1, 0} {
		got := fb.timeToSleep(remaining, maxSleep)
		if got < prev {
			t.Errorf("not monotonic: timeToSleep(%d) = %v < previous %v", remaining, got, prev)
		}
		if got > maxSleep {
			t.Errorf("timeToSleep(%d) = %v exceeds maxSleep %v", remaining, got, maxSleep)
		}
		prev = got
	}
}

func TestWaitUntilBufferSizeEnoughForBroadcastUnblocksWhenFilled(t *testing.T) {
	fb := NewFrameBuffer()

	done := make(chan struct{})
	go func() {
		fb.WaitUntilBufferSizeEnoughForBroadcast(1)
		close(done)
	}()

	// Empty buffer: the waiter must stay parked.
	select {
	case <-done:
		t.Fatal("returned before the buffer had enough frames")
	case <-time.After(50 * time.Millisecond):
	}

	var data [protocol.BatchSize]byte
	if err := fb.AddFramesToBuffer(0, protocol.FramesPerBatch-1, &data); err != nil {
		t.Fatalf("AddFramesToBuffer: %v", err)
	}
	fb.AdvanceHead(1)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("did not unblock after enough frames were added")
	}
}

func TestWaitForRoomReturnsPacingDelay(t *testing.T) {
	fb := NewFrameBuffer()
	// An empty buffer has maximum room, so this must return promptly with a
	// pacing delay of zero (no need to throttle).
	got := fb.WaitForRoom(1, 4*time.Second)
	if got != 0 {
		t.Errorf("WaitForRoom on empty buffer = %v, want 0", got)
	}
}
