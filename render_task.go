package main

type RenderTask struct {
	id         uint16
	startFrame uint32
	endFrame   uint32
}

func NewRenderTask(id uint16, startFrame uint32, endFrame uint32) *RenderTask {
	return &RenderTask{
		id:         id,
		startFrame: startFrame,
		endFrame:   endFrame,
	}
}
