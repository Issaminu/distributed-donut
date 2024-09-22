package main

import (
	"log"
	"slices"
)

type FrameBuffer struct {
	frames map[uint32][]byte // Map<frame number, frame data>
}

func NewFrameBuffer() *FrameBuffer {
	return &FrameBuffer{
		frames: make(map[uint32][]byte),
	}
}

var frameBuffer = NewFrameBuffer()

func (fb *FrameBuffer) AddFramesToBuffer(startFrame uint32, endFrame uint32, data *[BatchSize]byte) {
	for i := startFrame; i <= endFrame; i++ {
		offset := (i - startFrame) * FrameSize
		fb.frames[i] = data[offset : offset+FrameSize]
	}
	triggerFrameBufferSizeCheck <- true
}

func (fb *FrameBuffer) GetOrderedFrames() []byte {
	var keys []uint32
	for k := range fb.frames {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	totalFrames := len(keys)
	if totalFrames == 0 {
		log.Println("Returning empty frames")
		return nil
	}

	encodedFrames := make([]byte, totalFrames*FrameSize)
	for i, key := range keys {
		copy(encodedFrames[i*FrameSize:], fb.frames[key])
	}
	return encodedFrames
}

func (fb *FrameBuffer) RemoveSentFramesFromBuffer() {
	startFrame := uint32(0)
	endFrame := uint32(SecondsToBroadcast*FramesPerBatch - 1)
	for i := startFrame; i <= endFrame; i++ {
		delete(fb.frames, i)
	}
	triggerTaskDispatcher <- true // Trigger the taskDispatcher to send more work
}

func (fb *FrameBuffer) GetLength() uint32 {
	return uint32(len(fb.frames))
}

func (fb *FrameBuffer) IsBufferSizeSufficientForBroadcast(isFirstBroadcast bool) bool {
	if isFirstBroadcast {
		return fb.GetLength() > FirstSecondsToBroadcast*FramesPerBatch
	}
	return fb.GetLength() > SecondsToBroadcast*FramesPerBatch
}
