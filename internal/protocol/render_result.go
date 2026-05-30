package protocol

import (
	"encoding/binary"
	"errors"
)

type RenderResult struct {
	ID     uint32
	Frames [BatchSize]byte
}

// NewRenderResult parses the body of a RenderResult message (the bytes after
// the message-type tag): 4 bytes render task ID followed by exactly BatchSize
// bytes of frames.
func NewRenderResult(data []byte) (*RenderResult, error) {
	if len(data) < 4 {
		return nil, errors.New("render result too short to contain a task ID")
	}
	id := binary.BigEndian.Uint32(data[0:4])
	frames := data[4:]

	if len(frames) != BatchSize {
		return nil, errors.New("invalid frame batch size received in NewRenderResult()")
	}
	return &RenderResult{
		ID:     id,
		Frames: [BatchSize]byte(frames),
	}, nil
}
