package protocol

type RenderTask struct {
	ID         uint32
	StartFrame uint32
	EndFrame   uint32
}

func NewRenderTask(id uint32, startFrame uint32, endFrame uint32) *RenderTask {
	return &RenderTask{
		ID:         id,
		StartFrame: startFrame,
		EndFrame:   endFrame,
	}
}
