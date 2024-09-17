package main

import (
	"encoding/binary"
	"errors"
)

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
const FrameSize = 880 // bytes
// Length of a Work is FrameSize + 4 + 4 = 888 bytes
const WorkSize = (FrameSize + 4 + 4) * FramesPerChunk // bytes

type Work struct {
	startFrame    uint32
	endFrame      uint32
	workPerformed []byte
}

func NewWork(data []byte) (*Work, error) {
	startFrame, endFrame, workPerformed, err := covertBinaryToWorkFormat(data)
	if err != nil {
		return nil, err
	}

	work := &Work{
		startFrame:    startFrame,
		endFrame:      endFrame,
		workPerformed: workPerformed,
	}
	return work, nil
}

func covertBinaryToWorkFormat(data []byte) (uint32, uint32, []byte, error) {
	if len(data) != WorkSize {
		return 0, 0, []byte{}, errors.New("invalid work size")
	}
	startFrame := binary.BigEndian.Uint32(data[0:4])
	endFrame := binary.BigEndian.Uint32(data[4:8])
	var workPerformed = data[8:]
	return startFrame, endFrame, workPerformed, nil
}
