// Package buffer holds the server's circular frame buffer: the single source of
// truth for which frames have been rendered and are ready to broadcast.
package buffer

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/Issaminu/distributed-donut/internal/protocol"
)

const (
	MaxBatches = 30 * 60                              // 1800 batches/seconds ~= 30 Minutes (Could go longer but it would consume much more memory for no benefit)
	MaxFrames  = MaxBatches * protocol.FramesPerBatch // Maximum amount of frames we can hold in the buffer
	BufferSize = MaxFrames * protocol.FrameSize       // Buffer size in bytes
)

type FrameBuffer struct {
	buffer [BufferSize]byte
	head   uint64 // byte position, monotonically increasing to ~6 years of playback
	tail   uint64 // byte position, monotonically increasing to ~6 years of playback
	mu     sync.Mutex
	cond   *sync.Cond
}

func NewFrameBuffer() *FrameBuffer {
	fb := FrameBuffer{
		head: 0,
		tail: 0,
	}
	fb.cond = sync.NewCond(&fb.mu)
	return &fb
}

func (fb *FrameBuffer) AddFramesToBuffer(startFrame uint32, endFrame uint32, data *[protocol.BatchSize]byte) error {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	if startFrame > endFrame {
		return errors.New("start frame is greater than end frame")
	}

	batchStartIndex := (startFrame * protocol.FrameSize) % BufferSize
	batchEndIndex := batchStartIndex + protocol.BatchSize

	copy(fb.buffer[batchStartIndex:batchEndIndex], data[:])

	return nil
}

// GetFramesToBroadcast returns the next `seconds` worth of frames starting at the tail.
// It always returns a fresh copy, so the caller can safely hold the result after the lock is released and after the tail advances.
func (fb *FrameBuffer) GetFramesToBroadcast(seconds int) []byte {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	if !fb.isBufferSizeEnoughForBroadcast(seconds) {
		return nil
	}

	deltaToBroadcast := uint64((seconds * protocol.FramesPerBatch) * protocol.FrameSize)
	result := make([]byte, deltaToBroadcast)

	// Copy from the tail, wrapping around the end of the ring if necessary.
	tailIdx := fb.tail % BufferSize
	// copy() copies min(len(dst), len(src)) bytes, so even though buffer[tailIdx:] runs to the end of the ring, this writes only deltaToBroadcast bytes (when they fit before the end), n tells us how many actually have landed.
	n := copy(result, fb.buffer[tailIdx:])
	if uint64(n) < deltaToBroadcast {
		// Didn't fit before the ring's end: copy the remainder from the start.
		copy(result[n:], fb.buffer[:deltaToBroadcast-uint64(n)])
	}

	return result
}

// Push the Frame Buffer tail forwards to not keep the frames
// that were sent as part of the scope
func (fb *FrameBuffer) RemoveSentFramesFromBuffer(secondsToRemove int) {
	fb.mu.Lock()

	framesToRemove := uint64(secondsToRemove * protocol.FramesPerBatch)

	bufferLength := fb.GetLengthInFrames()
	if framesToRemove > bufferLength {
		panic(fmt.Sprintf("Removing %d frames from a buffer that's only %d in size", framesToRemove, bufferLength))
	}
	fb.tail += framesToRemove * protocol.FrameSize

	fb.mu.Unlock()
	fb.cond.Broadcast()
}

func (fb *FrameBuffer) GetLengthInFrames() uint64 { // Frame Buffer length in number of frames
	return (fb.head - fb.tail) / protocol.FrameSize
}

func (fb *FrameBuffer) GetNextFrameNumber() uint64 {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	return (fb.head / protocol.FrameSize) % MaxFrames
}

func (fb *FrameBuffer) isBufferSizeEnoughForBroadcast(seconds int) bool {
	length := fb.GetLengthInFrames()

	return length >= uint64(seconds)*protocol.FramesPerBatch
}

func (fb *FrameBuffer) frameBatchesRemainingToFullBuffer() uint64 {
	if fb.isBufferFull() {
		return 0
	}

	framesInBuffer := fb.GetLengthInFrames()
	framesRemainingToFullBuffer := MaxFrames - framesInBuffer

	frameBatchesRemainingToFullBuffer := framesRemainingToFullBuffer / protocol.FramesPerBatch // this can be 0.something, if so, we consider the FrameBuffer to be full. TODO: add more surgical/precise handling for sub-Batch Render Tasks.

	return frameBatchesRemainingToFullBuffer
}

func (fb *FrameBuffer) isBufferFull() bool {
	return fb.GetLengthInFrames() == MaxFrames
}

// When our buffer is close to empty, we rapidly trigger tasks to quickly fill it up.
// When our buffer is getting closer to full, we start to slow down the rate at which we trigger tasks, since it's no longer urgent.
// The sleep duration grows exponentially as the frame buffer is getting full (i.e. as `frameBatchesRemainingToFullBuffer` approaches 0).
// maxSleep is the upper bound for the pacing delay; the caller decides it (it
// equals the buffered playback duration it is willing to tolerate).
// TODO: make this less aggressive at the early stages of filling the buffer
func (fb *FrameBuffer) timeToSleep(frameBatchesRemainingToFullBuffer uint64, maxSleep time.Duration) time.Duration {
	if frameBatchesRemainingToFullBuffer >= MaxBatches {
		return 0
	}

	// fullness: 0 when buffer is empty, 1 when buffer is full.
	fullness := 1.0 - float64(frameBatchesRemainingToFullBuffer)/float64(MaxBatches)

	// k: the steepness knob of how fast do we want to ramp up
	const k = 2.0
	maxSleepSeconds := maxSleep.Seconds()
	seconds := maxSleepSeconds * (math.Exp(k*fullness) - 1) / (math.Exp(k) - 1)

	return time.Duration(seconds * float64(time.Second))
}

// Park if the buffer doesn't have sufficient enough frames to broadcast
func (fb *FrameBuffer) WaitUntilBufferSizeEnoughForBroadcast(seconds int) {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	for !fb.isBufferSizeEnoughForBroadcast(seconds) {
		fb.cond.Wait()
	}
}

// Advance head with new batches freashly received
func (fb *FrameBuffer) AdvanceHead(batchesToFetch int) {
	fb.mu.Lock()

	fb.head += uint64(batchesToFetch * protocol.BatchSize)
	fb.mu.Unlock()
	fb.cond.Broadcast()
}

// WaitForRoom parks until the buffer has room for batchesToFetch more batches, then returns how long the caller should pace before dispatching the next round. maxSleep bounds that pacing delay.
func (fb *FrameBuffer) WaitForRoom(batchesToFetch int, maxSleep time.Duration) time.Duration {
	fb.mu.Lock()

	for fb.frameBatchesRemainingToFullBuffer() < uint64(batchesToFetch) {
		fb.cond.Wait()
	}
	frameBatchesRemaining := fb.frameBatchesRemainingToFullBuffer()
	fb.mu.Unlock()

	return fb.timeToSleep(frameBatchesRemaining, maxSleep)
}
