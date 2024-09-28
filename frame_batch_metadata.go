package main

// === Calculation for the frame size ===
// The frame is represented as a 2D grid of characters.
// The grid dimensions are 80 characters wide and 22 characters tall.
// Each row ends with a newline character.

// We can calculate the length as follows:
// 80 characters per row
// 22 rows
// Total length = 80 * 22 = 1760 characters
// Possible values for each character are ".,-~:;=!*#$@ \n".
// These are 13 possible values. 13 in binary is 1101. So, we can represent each character as a 4-bit binary number.
// This means that the total length of the frame is 1760 * 4 = 7040 bits = 880 bytes.
const FrameSize = 880                        // bytes
const BatchSize = FrameSize * FramesPerBatch // bytes, FrameBatch also has some metadata that is stripped when broadcasting the frames

type FrameBatchMetadata struct {
	ClientID   uint32
	renderTask RenderTask
	completed  bool
}

func NewFrameBatchMetadata(ClientID uint32, startFrame uint32, endFrame uint32) *FrameBatchMetadata {
	id := frameBatchMap.GetLength(ClientID)
	renderTask := NewRenderTask(id, startFrame, endFrame)
	return &FrameBatchMetadata{
		ClientID:   ClientID,
		renderTask: *renderTask,
		completed:  false,
	}
}
