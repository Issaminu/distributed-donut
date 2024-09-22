package main

type FrameBuffer struct {
	buffer           [BufferSize][FrameSize]byte
	head             int
	tail             int
	firstFrameNumber uint32
}

func NewFrameBuffer() *FrameBuffer {
	return &FrameBuffer{
		head:             0,
		tail:             0,
		firstFrameNumber: 0,
	}
}

var frameBuffer = NewFrameBuffer()

func (fb *FrameBuffer) AddFramesToBuffer(startFrame uint32, endFrame uint32, data *[BatchSize]byte) {
	if fb.GetLength() == 0 {
		fb.firstFrameNumber = startFrame
	}

	for i := startFrame; i <= endFrame; i++ {
		offset := (i - startFrame) * FrameSize
		copy(fb.buffer[fb.tail][:], data[offset:offset+FrameSize])
		fb.tail = (fb.tail + 1) % BufferSize
		if fb.tail == fb.head {
			fb.head = (fb.head + 1) % BufferSize
			fb.firstFrameNumber++
		}
	}
	frameBufferSizeCheck.trigger <- true
}

func (fb *FrameBuffer) GetOrderedFrames() []byte {
	length := fb.GetLength()
	if length == 0 {
		return nil
	}

	result := make([]byte, length*FrameSize)
	index := 0

	for i := 0; i < length; i++ {
		bufferIndex := (fb.head + i) % BufferSize
		copy(result[index:index+FrameSize], fb.buffer[bufferIndex][:])
		index += FrameSize
	}

	return result
}

func (fb *FrameBuffer) RemoveSentFramesFromBuffer() {
	framesToRemove := SecondsToBroadcast * FramesPerBatch
	if framesToRemove > fb.GetLength() {
		framesToRemove = fb.GetLength()
	}

	fb.head = (fb.head + framesToRemove) % BufferSize
	fb.firstFrameNumber += uint32(framesToRemove)
}

func (fb *FrameBuffer) GetLength() int {
	if fb.tail >= fb.head {
		return fb.tail - fb.head
	}
	return BufferSize - fb.head + fb.tail
}

func (fb *FrameBuffer) IsBufferSizeSufficientForBroadcast(isFirstBroadcast bool) bool {
	requiredFrames := SecondsToBroadcast * FramesPerBatch
	if isFirstBroadcast {
		requiredFrames = FirstSecondsToBroadcast * FramesPerBatch
	}
	return fb.GetLength() >= requiredFrames
}
