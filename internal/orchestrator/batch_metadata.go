package orchestrator

import "github.com/Issaminu/distributed-donut/internal/protocol"

type FrameBatchMetadata struct {
	renderTask protocol.RenderTask
	completed  bool          // synchronous "is it done" state, read/written under the FrameBatchMap mutex
	done       chan struct{} // closed once when the batch is stored, to wake the dispatcher goroutine waiting on it
}

func NewFrameBatchMetadata(renderTaskID uint32, startFrame uint32, endFrame uint32) *FrameBatchMetadata {
	renderTask := protocol.NewRenderTask(renderTaskID, startFrame, endFrame)
	return &FrameBatchMetadata{
		renderTask: *renderTask,
		completed:  false,
		done:       make(chan struct{}),
	}
}
