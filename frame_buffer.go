package main

import (
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

func (fb *FrameBuffer) AddFramesToBuffer(startFrame uint32, endFrame uint32, data *[BatchSize]byte) {
	if (endFrame - startFrame + 1) != FramesPerBatch {
		log.Println("WARNING: Received incorrect batch size")
		return
	}
	if fb.head == fb.tail && fb.head != 0 {
		log.Println("WARNING: Frame buffer is full, dropping frame")
		return
	}

	for i := 0; i < BatchSize; i += FrameSize {
		newHeadPosition := (fb.head + FrameSize) % BufferSize

		startPosition := min((fb.head)%BufferSize, newHeadPosition)
		endPosition := max((fb.head)%BufferSize, newHeadPosition)
		copy(fb.buffer[startPosition:endPosition], data[i:i+FrameSize])
		fb.head = newHeadPosition
	}

	frameBufferSizeCheck.trigger <- true
}

func (fb *FrameBuffer) GetFrames() []byte {
	length := fb.GetLength()
	if length == 0 {
		return nil
	}
	if fb.head >= fb.tail {
		return fb.buffer[fb.tail%BufferSize : fb.head%BufferSize]
	}
	return fb.buffer[fb.head%BufferSize : fb.tail%BufferSize]
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

func (fb *FrameBuffer) GetLength() uint32 {
	if fb.head >= fb.tail {
		return (fb.head - fb.tail) / FrameSize
	}
	return (BufferSize - fb.tail + fb.head) / FrameSize
}

func (fb *FrameBuffer) GetNextFrameNumber() uint32 {
	return (fb.head / FrameSize) % BufferSize
}

func (fb *FrameBuffer) IsBufferSizeSufficientForBroadcast(isFirstBroadcast bool) bool {
	requiredFrames := SecondsToBroadcast
	if isFirstBroadcast {
		requiredFrames = FirstSecondsToBroadcast
	}
	return fb.GetLength() >= uint32(requiredFrames*FramesPerBatch)
}
