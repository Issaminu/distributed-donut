package main

type FrameBufferSizeCheck struct {
	trigger chan bool
	ready   chan bool
}

func NewFrameBufferSizeCheck() *FrameBufferSizeCheck {
	return &FrameBufferSizeCheck{
		trigger: make(chan bool, 1),
		ready:   make(chan bool, 1),
	}
}

var frameBufferSizeCheck = NewFrameBufferSizeCheck()
