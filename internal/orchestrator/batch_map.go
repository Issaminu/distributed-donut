package orchestrator

import (
	"log/slog"
	"sync"

	"github.com/Issaminu/distributed-donut/internal/buffer"
	"github.com/Issaminu/distributed-donut/internal/client"
	"github.com/Issaminu/distributed-donut/internal/protocol"
)

type FrameBatchMap struct {
	frameBatches map[uint32]map[uint32]FrameBatchMetadata // Map<client ID, Map<render task ID, frame batch metadata>>
	mutex        sync.Mutex
	buffer       *buffer.FrameBuffer // where completed render results are committed
}

func NewFrameBatchMap(fb *buffer.FrameBuffer) *FrameBatchMap {
	return &FrameBatchMap{
		frameBatches: make(map[uint32]map[uint32]FrameBatchMetadata),
		buffer:       fb,
	}
}

func (fbMap *FrameBatchMap) AddFrameBatch(clientID uint32, frameBatch *FrameBatchMetadata) {
	fbMap.mutex.Lock()
	defer fbMap.mutex.Unlock()
	if _, ok := fbMap.frameBatches[clientID]; !ok {
		fbMap.frameBatches[clientID] = make(map[uint32]FrameBatchMetadata)
	}
	fbMap.frameBatches[clientID][frameBatch.renderTask.ID] = *frameBatch
}

func (fbMap *FrameBatchMap) GetLength(ClientID uint32) uint32 {
	fbMap.mutex.Lock()
	defer fbMap.mutex.Unlock()
	return uint32(len(fbMap.frameBatches[ClientID]))
}

func (fbMap *FrameBatchMap) SwitchRenderTaskExecutor(renderTaskID uint32, clientID uint32, newClient *client.Client) uint32 {
	fbMap.mutex.Lock()
	defer fbMap.mutex.Unlock()
	if clientID == newClient.ID() {
		panic("clientID and newClientID cannot be the same")
	}
	fbMeta := fbMap.frameBatches[clientID][renderTaskID]
	newRenderTaskID := newClient.GenerateNewRenderTaskID()
	if _, ok := fbMap.frameBatches[newClient.ID()]; !ok {
		fbMap.frameBatches[newClient.ID()] = make(map[uint32]FrameBatchMetadata)
	}
	fbMap.frameBatches[newClient.ID()][newRenderTaskID] = fbMeta
	delete(fbMap.frameBatches[clientID], renderTaskID)
	if len(fbMap.frameBatches[clientID]) == 0 {
		delete(fbMap.frameBatches, clientID)
	}
	return newRenderTaskID
}

func (fbMap *FrameBatchMap) DeleteRenderTask(clientID uint32, renderTaskID uint32) {
	fbMap.mutex.Lock()
	defer fbMap.mutex.Unlock()
	delete(fbMap.frameBatches[clientID], renderTaskID)
	if len(fbMap.frameBatches[clientID]) == 0 {
		delete(fbMap.frameBatches, clientID)
	}
}

func (fbMap *FrameBatchMap) SaveRenderResult(clientID uint32, renderResult *protocol.RenderResult) error {
	fbMap.mutex.Lock()
	defer fbMap.mutex.Unlock()
	if _, ok := fbMap.frameBatches[clientID][renderResult.ID]; !ok {
		slog.Warn("client returned a render result for a task not assigned to it", "client", clientID, "task", renderResult.ID)
		return nil
	}

	batchMetadata := fbMap.frameBatches[clientID][renderResult.ID]
	if batchMetadata.completed {
		return nil // duplicate result for an already-completed task; ignore so we don't double-close done
	}

	// TODO: validate render task results. Clients are untrusted, yet we commit their raw frame bytes straight into the shared buffer that everyone plays back. A buggy or malicious client can corrupt the animation for all viewers.
	if err := fbMap.buffer.AddFramesToBuffer(batchMetadata.renderTask.StartFrame, batchMetadata.renderTask.EndFrame, &renderResult.Frames); err != nil {
		return err
	}
	batchMetadata.completed = true
	fbMap.frameBatches[clientID][renderResult.ID] = batchMetadata
	close(batchMetadata.done) // wake the dispatcher goroutine waiting on this task
	return nil
}
