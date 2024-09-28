package main

import (
	"encoding/binary"
	"errors"
)

type RenderResult struct {
	id     uint32
	frames [BatchSize]byte
}

func NewRenderResult(data []byte) (*RenderResult, error) {
	id := binary.BigEndian.Uint32(data[0:4])
	frames := data[4:]

	if len(frames) != BatchSize {
		return nil, errors.New("invalid frame batch size received in NewRenderResult()")
	}
	return &RenderResult{
		id:     id,
		frames: [BatchSize]byte(frames),
	}, nil
}
