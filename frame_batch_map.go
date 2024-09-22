package main

type FrameBatchMap struct {
	frameBatches map[uint16]map[uint16]FrameBatchMetadata // Map<client ID, Map<render task ID, frame batch metadata>>
}

func NewFrameBatchMap() *FrameBatchMap {
	return &FrameBatchMap{
		frameBatches: make(map[uint16]map[uint16]FrameBatchMetadata),
	}
}

var frameBatchMap = NewFrameBatchMap()

func (fbm *FrameBatchMap) AddFrameBatch(frameBatch *FrameBatchMetadata) {
	if _, ok := fbm.frameBatches[frameBatch.ClientID]; !ok {
		fbm.frameBatches[frameBatch.ClientID] = make(map[uint16]FrameBatchMetadata)
	}
	fbm.frameBatches[frameBatch.ClientID][frameBatch.renderTask.id] = *frameBatch
}

func (fbm *FrameBatchMap) GetFrameBatches(ClientID uint16) map[uint16]FrameBatchMetadata {
	return fbm.frameBatches[ClientID]
}

func (fbm *FrameBatchMap) AddRenderResultToFrameBatch(clientID uint16, renderResult *RenderResult) {
	batchMetadata := fbm.frameBatches[clientID][renderResult.id]
	batchMetadata.completed = true
	fbm.frameBatches[clientID][renderResult.id] = batchMetadata
	frameBuffer.AddFrameBatch(batchMetadata.renderTask.startFrame, batchMetadata.renderTask.endFrame, &renderResult.frames)
}
