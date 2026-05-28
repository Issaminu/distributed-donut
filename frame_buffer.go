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
	batchEndIndex := (endFrame*FrameSize + FrameSize) % BufferSize // `+FrameSize` because `endFrame*FrameSize` defines where the last frame starts. But what we actually meed os where it ends. i.e. the position of the last byte of the frameBatch

	if batchEndIndex-batchStartIndex != BatchSize {
		return errors.New("received incorrect batch size")
	}

	if batchStartIndex < fb.tail {
		return errors.New("appending frames that should have been already sent out")
	}

	copy(fb.buffer[batchStartIndex:batchEndIndex], data[:])

	fb.head = max(fb.head, batchEndIndex)
	log.Println("startFramePosition", batchStartIndex)
	log.Println("endFramePosition", batchEndIndex)

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
	fb.tail = fb.head
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
