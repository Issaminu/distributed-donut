package main

import "errors"

type RenderResult struct {
	id     uint16
	frames [BatchSize]byte
}

func NewRenderResult(id uint16, frames []byte) (*RenderResult, error) {
	if len(frames) != BatchSize {
		return nil, errors.New("invalid frame batch size received in NewRenderResult()")
	}
	return &RenderResult{
		id:     id,
		frames: [BatchSize]byte(frames),
	}, nil
}
