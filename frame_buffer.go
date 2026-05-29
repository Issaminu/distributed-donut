package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
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

var frameBuffer = NewFrameBuffer()

func (fb *FrameBuffer) AddFramesToBuffer(startFrame uint32, endFrame uint32, data *[BatchSize]byte) error {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	log.Println("====== Adding frames from", startFrame, "to", endFrame, "======")

	if startFrame > endFrame {
		return errors.New("start frame is greater than end frame")
	}

	batchStartIndex := (startFrame * FrameSize) % BufferSize
	batchEndIndex := (endFrame*FrameSize + FrameSize) % BufferSize // `+FrameSize` because `endFrame*FrameSize` defines where the last frame starts. But what we actually meed os where it ends. i.e. the position of the last byte of the frameBatch

	if batchEndIndex-batchStartIndex != BatchSize {
		return errors.New("received incorrect batch size")
	}

	if uint64(batchStartIndex) < fb.tail {
		return errors.New("appending frames that should have been already sent out")
	}

	copy(fb.buffer[batchStartIndex:batchEndIndex], data[:])

	return nil
}

func (fb *FrameBuffer) GetAllFramesInBuffer() []byte {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	length := fb.GetLengthInFrames()
	if length == 0 {
		return nil
	}
	if fb.head >= fb.tail {
		return fb.buffer[fb.tail%BufferSize : fb.head%BufferSize]
	}
	// When head < tail the valid data is from tail..end and from 0..head
	result := make([]byte, fb.GetLengthInBytes())
	tailIdx := fb.tail % BufferSize
	headIdx := fb.head % BufferSize
	copy(result, fb.buffer[tailIdx:])
	copy(result[BufferSize-tailIdx:], fb.buffer[:headIdx])
	return result
}

func (fb *FrameBuffer) GetFramesToBroadcast(seconds int) []byte {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	if !fb.isBufferSizeEnoughForBroadcast(seconds) {
		return nil
	}

	deltaToBroadcast := uint64((seconds * FramesPerBatch) * FrameSize)

	// deltaHead is the index in the frame buffer of the element after the last byte to broadcast in this turn
	// we're broadcasting from fb.tail .. fb.tail+deltaToBroadcast, `deltaHead` is the element right after that.
	deltaHead := (fb.tail%BufferSize + deltaToBroadcast) % BufferSize

	if deltaHead > fb.tail%BufferSize {
		return fb.buffer[fb.tail%BufferSize : deltaHead]
	}

	result := make([]byte, deltaToBroadcast)
	tailIdx := fb.tail % BufferSize
	headIdx := deltaHead % BufferSize
	copy(result, fb.buffer[tailIdx:])
	copy(result[BufferSize-tailIdx:], fb.buffer[:headIdx])

	return result
}

// Push the Frame Buffer tail forwards to not keep the frames
// that were sent as part of the scope
func (fb *FrameBuffer) RemoveSentFramesFromBuffer(secondsToRemove int) {
	fb.mu.Lock()

	framesToRemove := uint64(secondsToRemove * FramesPerBatch)

	bufferLength := fb.GetLengthInFrames()
	if framesToRemove > bufferLength {
		panic(fmt.Sprintf("Removing %d frames from a buffer that's only %d in size", framesToRemove, bufferLength))
	}
	fb.tail += framesToRemove * FrameSize

	fb.mu.Unlock()
	fb.cond.Broadcast()
}

func (fb *FrameBuffer) GetLengthInFrames() uint64 { // Frame Buffer length in number of frames
	if fb.head >= fb.tail {
		return (fb.head - fb.tail) / FrameSize
	}
	return (BufferSize - fb.tail + fb.head) / FrameSize
}

func (fb *FrameBuffer) GetLengthInBytes() uint64 { // Frame Buffer length in bytes
	fb.mu.Lock()
	defer fb.mu.Unlock()

	length := fb.GetLengthInFrames()
	if length == 0 {
		return 0
	}
	if fb.tail >= fb.head {
		return BufferSize - (fb.tail%BufferSize - fb.head%BufferSize)
	}

	return fb.head%BufferSize - fb.tail%BufferSize
}

func (fb *FrameBuffer) GetNextFrameNumber() uint64 {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	return (fb.head / FrameSize) % MaxFrames
}

func (fb *FrameBuffer) isBufferSizeEnoughForBroadcast(seconds int) bool {
	length := fb.GetLengthInFrames()

	return length >= uint64(seconds)*FramesPerBatch
}

func (fb *FrameBuffer) frameBatchesRemainingToFullBuffer() uint64 {
	if fb.isBufferFull() {
		return 0
	}

	framesInBuffer := fb.GetLengthInFrames()
	framesRemainingToFullBuffer := MaxFrames - framesInBuffer

	frameBatchesRemainingToFullBuffer := framesRemainingToFullBuffer / FramesPerBatch // this can be 0.something, if so, we consider the FrameBuffer to be full. TODO: add more surgical/precise handling for sub-Batch Render Tasks.

	return frameBatchesRemainingToFullBuffer
}

func (fb *FrameBuffer) isBufferFull() bool {
	return fb.GetLengthInFrames() == MaxFrames
}

// When our buffer is close to empty, we rapidly trigger tasks to quickly fill it up.
// When our buffer is getting closer to full, we start to slow down the rate at which we trigger tasks, since it's no longer urgent.
// The sleep duration grows exponentially as the frame buffer is getting full (i.e. as `frameBatchesRemainingToFullBuffer` approaches 0).
func (fb *FrameBuffer) timeToSleep(frameBatchesRemainingToFullBuffer uint64) time.Duration {
	if frameBatchesRemainingToFullBuffer >= MaxBatches {
		return 0
	}

	// fullness: 0 when buffer is empty, 1 when buffer is full.
	fullness := 1.0 - float64(frameBatchesRemainingToFullBuffer)/float64(MaxBatches)

	// k: the steepness knob of how fast do we want to ramp up
	const k = 2.0
	// Scale the exponential to the buffered playback duration: a full buffer holds
	// secondsToBroadcast seconds of playback, so that's the upper bound for how long it makes
	// sense to wait before dispatching the next task.
	const maxSleepSeconds = float64(secondsToBroadcast) // = 4
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

	fb.head += uint64(batchesToFetch * BatchSize)
	fb.mu.Unlock()
	fb.cond.Broadcast()
}

// Park if the buffer is full or if there's no connected clients to work
func (fb *FrameBuffer) WaitForRoom(batchesToFetch int) {
	fb.mu.Lock()

	for fb.frameBatchesRemainingToFullBuffer() < uint64(batchesToFetch) {
		fb.cond.Wait()
	}
	frameBatchesRemaining := fb.frameBatchesRemainingToFullBuffer()
	fb.mu.Unlock()

	clientPool.WaitForAtLeastOne()

	sleepTime := fb.timeToSleep(frameBatchesRemaining)
	log.Printf("Triggering task dispatcher in %0.2f seconds", sleepTime.Seconds())
	time.Sleep(sleepTime)
}
