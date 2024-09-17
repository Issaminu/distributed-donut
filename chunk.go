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
// Length of a Chunk is FrameSize + 4 + 4 = 888 bytes
const ChunkSize = (FrameSize + 4 + 4) * FramesPerChunk // bytes

type Chunk struct {
	startFrame     uint32
	endFrame       uint32
	chunkPerformed []byte
}

func NewChunk(data []byte) (*Chunk, error) {
	startFrame, endFrame, chunkPerformed, err := covertBinaryToChunkFormat(data)
	if err != nil {
		return nil, err
	}

	chunk := &Chunk{
		startFrame:     startFrame,
		endFrame:       endFrame,
		chunkPerformed: chunkPerformed,
	}
	return chunk, nil
}

func covertBinaryToChunkFormat(data []byte) (uint32, uint32, []byte, error) {
	if len(data) != ChunkSize {
		return 0, 0, []byte{}, errors.New("invalid chunk size")
	}
	startFrame := binary.BigEndian.Uint32(data[0:4])
	endFrame := binary.BigEndian.Uint32(data[4:8])
	var chunkPerformed = data[8:]
	return startFrame, endFrame, chunkPerformed, nil
}
