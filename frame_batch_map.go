package main

import (
	"log"
	"sync"
)

type FrameBatchMap struct {
	frameBatches map[uint32]map[uint32]FrameBatchMetadata // Map<client ID, Map<render task ID, frame batch metadata>>
	mutex        sync.Mutex
}

func NewFrameBatchMap() *FrameBatchMap {
	return &FrameBatchMap{
		frameBatches: make(map[uint32]map[uint32]FrameBatchMetadata),
	}
}

var frameBatchMap = NewFrameBatchMap()

func (fbMap *FrameBatchMap) AddFrameBatch(clientID uint32, frameBatch *FrameBatchMetadata) {
	fbMap.mutex.Lock()
	defer fbMap.mutex.Unlock()
	if _, ok := fbMap.frameBatches[clientID]; !ok {
		fbMap.frameBatches[clientID] = make(map[uint32]FrameBatchMetadata)
	}
	fbMap.frameBatches[clientID][frameBatch.renderTask.id] = *frameBatch
}

func (fbMap *FrameBatchMap) GetLength(ClientID uint32) uint32 {
	fbMap.mutex.Lock()
	defer fbMap.mutex.Unlock()
	return uint32(len(fbMap.frameBatches[ClientID]))
}

func (fbMap *FrameBatchMap) SwitchRenderTaskExecutor(renderTaskID uint32, clientID uint32, newClient *Client) uint32 {
	fbMap.mutex.Lock()
	defer fbMap.mutex.Unlock()
	if clientID == newClient.id {
		panic("clientID and newClientID cannot be the same")
	}
	log.Println("Assigning render task to client", newClient.id, "instead of client", clientID)
	fbMeta := fbMap.frameBatches[clientID][renderTaskID]
	newRenderTaskID := newClient.GenerateNewRenderTaskID()
	fbMap.frameBatches[newClient.id][newRenderTaskID] = fbMeta
	delete(fbMap.frameBatches[clientID], renderTaskID)
	return newRenderTaskID
}

func (fbMap *FrameBatchMap) isRenderTaskCompleted(clientID uint32, renderTaskID uint32) bool {
	fbMap.mutex.Lock()
	defer fbMap.mutex.Unlock()
	return fbMap.frameBatches[clientID][renderTaskID].completed
}

func (fbMap *FrameBatchMap) SaveRenderResult(clientID uint32, renderResult *RenderResult) error {
	fbMap.mutex.Lock()
	defer fbMap.mutex.Unlock()
	if _, ok := fbMap.frameBatches[clientID][renderResult.id]; !ok {
		log.Printf("Warning: Client %d is trying to save a render result for a task %d that isn't assigned to them", clientID, renderResult.id)
		return nil
	}

	batchMetadata := fbMap.frameBatches[clientID][renderResult.id]
	batchMetadata.completed = true
	fbMap.frameBatches[clientID][renderResult.id] = batchMetadata
	return frameBuffer.AddFramesToBuffer(batchMetadata.renderTask.startFrame, batchMetadata.renderTask.endFrame, &renderResult.frames)
}
