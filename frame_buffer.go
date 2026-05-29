package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

type FrameBuffer struct {
	buffer [BufferSize]byte
	head   uint32
	tail   uint32
}

func NewFrameBuffer() *FrameBuffer {
	return &FrameBuffer{
		head: 0,
		tail: 0,
	}
}

var frameBuffer = NewFrameBuffer()
var triggerFrameBufferChange = make(chan bool, 1)

func (fb *FrameBuffer) AddFramesToBuffer(startFrame uint32, endFrame uint32, data *[BatchSize]byte) error {
	log.Println("====== Adding frames from", startFrame, "to", endFrame, "======")

	if startFrame > endFrame {
		return errors.New("start frame is greater than end frame")
	}

	batchStartIndex := (startFrame * FrameSize) % BufferSize
	batchEndIndex := (endFrame*FrameSize + FrameSize) % BufferSize // `+FrameSize` because `endFrame*FrameSize` defines where the last frame starts. But what we actually meed os where it ends. i.e. the position of the last byte of the frameBatch

	if batchEndIndex-batchStartIndex != BatchSize {
		return errors.New("received incorrect batch size")
	}

	if batchStartIndex < fb.tail {
		return errors.New("appending frames that should have been already sent out")
	}

	copy(fb.buffer[batchStartIndex:batchEndIndex], data[:])

	fb.head = max(fb.head%BatchSize, batchEndIndex)
	log.Println("startFramePosition", batchStartIndex)
	log.Println("endFramePosition", batchEndIndex)

	return nil
}

func (fb *FrameBuffer) GetAllFramesInBuffer() []byte {
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

func (fb *FrameBuffer) GetFramesToBroadcast() []byte {
	if !fb.isBufferSizeEnoughForBroadcast() {
		return nil
	}

	seconds := SecondsToBroadcast
	if isFirstBroadcast {
		seconds = FirstSecondsToBroadcast
	}

	deltaToBroadcast := uint32((seconds * FramesPerBatch) * FrameSize)

	// deltaHead is the index in the frame buffer of the element after the last byte to broadcast in this turn
	// we're broadcasting from fb.tail .. fb.tail+deltaToBroadcast, `deltaHead` is the element right after that.
	deltaHead := (fb.tail%BufferSize + deltaToBroadcast) % BufferSize

	if deltaHead > fb.tail {
		return fb.buffer[fb.tail:deltaHead]
	}

	result := make([]byte, deltaToBroadcast)
	tailIdx := fb.tail % BufferSize
	headIdx := deltaHead % BufferSize
	copy(result, fb.buffer[tailIdx:])
	copy(result[BufferSize-tailIdx:], fb.buffer[:headIdx])

	return result
}

func (fb *FrameBuffer) RemoveSentFramesFromBuffer() {
	secondsToRemove := SecondsToBroadcast
	if isFirstBroadcast {
		secondsToRemove = FirstSecondsToBroadcast
	}

	framesToRemove := uint32(secondsToRemove * FramesPerBatch)

	bufferLength := fb.GetLengthInFrames()
	if framesToRemove > bufferLength {
		panic(fmt.Sprintf("Removing %d frames from a buffer that's only %d in size", framesToRemove, bufferLength))
	}
	fb.tail = (fb.tail + framesToRemove*FrameSize) % BufferSize
}

func (fb *FrameBuffer) GetLengthInFrames() uint32 { // Frame Buffer length in number of frames
	if fb.head >= fb.tail {
		return (fb.head - fb.tail) / FrameSize
	}
	return (BufferSize - fb.tail + fb.head) / FrameSize
}

func (fb *FrameBuffer) GetLengthInBytes() uint32 { // Frame Buffer length in bytes
	length := fb.GetLengthInFrames()
	if length == 0 {
		return 0
	}
	if fb.tail >= fb.head {
		return BufferSize - (fb.tail%BufferSize - fb.head%BufferSize)
	}

	return fb.head%BufferSize - fb.tail%BufferSize
}

func (fb *FrameBuffer) GetNextFrameNumber() uint32 {
	return (fb.head / FrameSize) % BufferSize
}

func (fb *FrameBuffer) isBufferSizeEnoughForBroadcast() bool {
	length := fb.GetLengthInFrames()
	if isFirstBroadcast {
		return length >= FirstSecondsToBroadcast*FramesPerBatch
	}

	return length >= SecondsToBroadcast*FramesPerBatch
}

func (fb *FrameBuffer) isBufferFull() bool {
	return fb.GetLengthInFrames() == MaxFrames
}

// When our buffer is close to empty, we rapidly trigger tasks to quickly fill it up.
// When our buffer is getting closer to full, we start to slow down the rate at which we trigger tasks, since it's no longer urgent.
func (fb *FrameBuffer) timeToSleep() time.Duration {
	length := fb.GetLengthInBytes()
	ratio := float64(length) / float64(BufferSize)
	if ratio > 1 {
		ratio = 1
	}

	seconds := (float64(SecondsToBroadcast) * ratio * ratio)
	return time.Duration(seconds * float64(time.Second))
}

// Frame Buffer Monitor is a Goroutine that is responsible for 2 things:
// (1) Checking if we have enough frames to broadcast, if yes, trigger Frame Broadcaster.
// (2) Checking if we can dispatch more render tasks, if yes, trigger Task Dispatcher.
func (fb *FrameBuffer) frameBufferMonitor(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("Frame buffer monitor shutting down...")
			return
		case <-triggerFrameBufferChange:
			// (1) Checking if we have enough frames to broadcast
			if fb.isBufferSizeEnoughForBroadcast() {
				triggerFrameBroadcaster <- true
			}

			// (2) Checking if we want to trigger fetching for more batches or if we're full
			if fb.isBufferFull() {
				continue
			}

			// Triggering task dispatching in an expenetial backoff way, see timeToSleep() for explanation.
			go func() {
				sleepTime := fb.timeToSleep()
				log.Printf("Triggering task dispatcher in %d seconds", sleepTime)
				time.Sleep(sleepTime)
				triggerTaskDispatcher <- true
			}()

		}
	}
}
