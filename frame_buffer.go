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

	startFramePosition := (startFrame * FrameSize) % BufferSize
	endFramePosition := (endFrame * FrameSize) % BufferSize

	if endFramePosition-startFramePosition+FrameSize != BatchSize { // `+FrameSize` because len(arr) == end-begin+1 , here `+FrameSize` is that `+1`
		return errors.New("received incorrect batch size")
	}

	if startFramePosition < fb.tail {
		return errors.New("appending frames that should have been already sent out")
	}

	if fb.head == fb.tail && fb.head != 0 {
		return errors.New("framebuffer length is 0")
	}

	copy(fb.buffer[startFramePosition:endFramePosition], data[:])

	fb.head = max(fb.head, endFramePosition)

	frameBufferSizeCheck.trigger <- true
	return nil
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
