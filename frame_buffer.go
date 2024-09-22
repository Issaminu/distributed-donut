package main

import (
	"cmp"
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

func (fb *FrameBuffer) AddFrameBatch(startFrame uint32, endFrame uint32, data *[BatchSize]byte) {
	for i := startFrame; i <= endFrame; i++ {
		offset := (i - startFrame) * FrameSize
		fb.frames[i] = data[offset : offset+FrameSize]

	}
}

func (fb *FrameBuffer) GetOrderedFrames() []byte {
	var keys []uint32
	for k := range fb.frames {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(i, j uint32) int {
		return cmp.Compare(keys[i], keys[j])
	})

	orderedFrames := make([]byte, len(keys))
	for _, k := range keys {
		orderedFrames = append(orderedFrames, fb.frames[k]...)
	}
	return orderedFrames
}

func (fb *FrameBuffer) ClearBuffer() {
	fb.frames = make(map[uint32][]byte)
}

func (fb *FrameBuffer) GetLength() uint32 {
	return uint32(len(fb.frames))
}
