package main

import (
	"log"
	"sync"
)

type FrameBatchMap struct {
	frameBatches map[uint16]map[uint16]FrameBatchMetadata // Map<client ID, Map<render task ID, frame batch metadata>>
	mutex        sync.RWMutex
}

func NewFrameBatchMap() *FrameBatchMap {
	return &FrameBatchMap{
		frameBatches: make(map[uint16]map[uint16]FrameBatchMetadata),
	}
}

var frameBatchMap = NewFrameBatchMap()

func (fbm *FrameBatchMap) AddFrameBatch(frameBatch *FrameBatchMetadata) {
	fbm.mutex.Lock()
	defer fbm.mutex.Unlock()
	if _, ok := fbm.frameBatches[frameBatch.ClientID]; !ok {
		fbm.frameBatches[frameBatch.ClientID] = make(map[uint16]FrameBatchMetadata)
	}
	fbm.frameBatches[frameBatch.ClientID][frameBatch.renderTask.id] = *frameBatch
}

func (fbm *FrameBatchMap) GetFrameBatches(ClientID uint16) map[uint16]FrameBatchMetadata {
	fbm.mutex.RLock()
	defer fbm.mutex.RUnlock()
	return fbm.frameBatches[ClientID]
}

func (fbm *FrameBatchMap) SaveRenderResult(clientID uint16, renderResult *RenderResult) {
	fbm.mutex.Lock()
	defer fbm.mutex.Unlock()
	if _, ok := fbm.frameBatches[clientID][renderResult.id]; !ok {
		log.Printf("Warning: Client %d is trying to save a render result for a task %d that isn't assigned to them", clientID, renderResult.id)
		return
	}
	batchMetadata := fbm.frameBatches[clientID][renderResult.id]
	batchMetadata.completed = true
	fbm.frameBatches[clientID][renderResult.id] = batchMetadata
	frameBuffer.AddFramesToBuffer(batchMetadata.renderTask.startFrame, batchMetadata.renderTask.endFrame, &renderResult.frames)
}
