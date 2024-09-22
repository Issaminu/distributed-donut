package main

import (
	"encoding/binary"
	"errors"
)

type RenderResult struct {
	id     uint16
	frames [BatchSize]byte
}

func NewRenderResult(data []byte) (*RenderResult, error) {
	id := binary.BigEndian.Uint16(data[0:2])
	frames := data[2:]

	if len(frames) != BatchSize {
		return nil, errors.New("invalid frame batch size received in NewRenderResult()")
	}
	return &RenderResult{
		id:     id,
		frames: [BatchSize]byte(frames),
	}, nil
}
