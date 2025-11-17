package main

import (
	"errors"
	"log"
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

func (fb *FrameBuffer) AddFramesToBuffer(startFrame uint32, endFrame uint32, data *[BatchSize]byte) error {
	log.Println("====== Adding frames from", startFrame, "to", endFrame, "======")

	if startFrame > endFrame {
		return errors.New("start frame is greater than end frame")
	}

	batchStartIndex := (startFrame * FrameSize) % BufferSize
	batchEndIndex := (endFrame*FrameSize + FrameSize) % BufferSize // `+FrameSize` because `endFrame*FrameSize` defines where the last frame starts. But what we actually need is where it ends. i.e. the position of the last byte of the frameBatch

	// Fix: Handle wrap-around case for batch size validation
	var batchActualSize uint32
	if batchEndIndex >= batchStartIndex {
		batchActualSize = batchEndIndex - batchStartIndex
	} else {
		// Wrapped around: from start to end of buffer + from start of buffer to end index
		batchActualSize = (BufferSize - batchStartIndex) + batchEndIndex
	}

	if batchActualSize != BatchSize {
		return errors.New("received incorrect batch size")
	}

	if batchStartIndex < fb.tail {
		return errors.New("appending frames that should have been already sent out")
	}

	if fb.head == fb.tail && fb.head != 0 {
		return errors.New("framebuffer length is 0")
	}

	// Fix: Handle wrap-around case for copying data
	if batchEndIndex >= batchStartIndex {
		// Normal case: no wrap-around
		copy(fb.buffer[batchStartIndex:batchEndIndex], data[:])
	} else {
		// Wrap-around case: copy in two parts
		firstPartSize := BufferSize - batchStartIndex
		copy(fb.buffer[batchStartIndex:], data[:firstPartSize])
		copy(fb.buffer[:batchEndIndex], data[firstPartSize:])
	}

	fb.head = max(fb.head, batchEndIndex)
	log.Println("startFramePosition", batchStartIndex)
	log.Println("endFramePosition", batchEndIndex)

	frameBufferSizeCheck.trigger <- true
	return nil
}

func (fb *FrameBuffer) GetFrames() []byte {
	length := fb.GetLength()
	if length == 0 {
		return nil
	}
	
	// Fix: Handle wrap-around case properly
	if fb.head >= fb.tail {
		// Normal case: no wrap-around
		return fb.buffer[fb.tail%BufferSize : fb.head%BufferSize]
	} else {
		// Wrap-around case: concatenate two parts
		// From tail to end of buffer + from start of buffer to head
		part1 := fb.buffer[fb.tail:]
		part2 := fb.buffer[:fb.head]
		result := make([]byte, len(part1)+len(part2))
		copy(result, part1)
		copy(result[len(part1):], part2)
		return result
	}
}

func (fb *FrameBuffer) RemoveSentFramesFromBuffer() {
	framesToRemove := uint32(SecondsToBroadcast * FramesPerBatch)

	bufferLength := fb.GetLength()
	if framesToRemove > bufferLength {
		framesToRemove = bufferLength
	}
	fb.tail = (fb.tail + framesToRemove*FrameSize) % BufferSize
	frameBufferSizeCheck.trigger <- true
}

func (fb *FrameBuffer) GetLength() uint32 { // Length is in number of frames, not bytes
	if fb.head >= fb.tail {
		return (fb.head - fb.tail) / FrameSize
	}
	return (BufferSize - fb.tail + fb.head) / FrameSize
}

func (fb *FrameBuffer) GetNextFrameNumber() uint32 {
	return (fb.head / FrameSize) % BufferSize
}

func (fb *FrameBuffer) IsBufferSizeSufficientForBroadcast() bool {
	requiredFrames := SecondsToBroadcast * FramesPerBatch
	if isFirstBroadcast {
		requiredFrames = FirstSecondsToBroadcast * FramesPerBatch
	}
	return fb.GetLength() >= uint32(requiredFrames)
}
