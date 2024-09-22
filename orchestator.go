package main

import (
	"math/rand"
	"sync"
)

var triggerTaskDispatcher = make(chan bool)
var triggerFrameBroadcaster = make(chan bool)

func frameOrchestrator() {
	go frameBroadcaster()
	go taskDispatcher()
	triggerTaskDispatcher <- true // Trigger the taskDispatcher for the first time
}

const (
	FramesPerBatch           = 60
	QueueLength              = 12                           // This allows for storing up to 12 batches (12 seconds of animation)
	MinQueueLengthToRetrieve = 6                            // When the queue length drops below this point, we dispatch render tasks. When we receive all render results, we broadcast them to all clients.
	NumBatchesToRetrieve     = MinQueueLengthToRetrieve * 2 // When fetching, retrieve double what is needed to trigger a fetch
)

func frameBroadcaster() {
	for {
		<-triggerFrameBroadcaster

		if len(clientPool.clients) == 0 {
			<-clientPoolIsNotEmpty // Wait until the pool is not empty, meaning we have clients to dispatch rendering tasks to
		}
		frames := frameBuffer.GetOrderedFrames()
		sendFramesToAllClients(frames)
		frameBuffer.ClearBuffer()
	}
}

func taskDispatcher() {
	for {
		for frameBuffer.GetLength() > MinQueueLengthToRetrieve {
			<-triggerTaskDispatcher
		}

		if len(clientPool.clients) == 0 {
			<-clientPoolIsNotEmpty // Wait until the pool is not empty, meaning we have clients to dispatch rendering tasks to
		}

		var wg sync.WaitGroup
		var currentFrame = frameBuffer.GetLength()

		for range NumBatchesToRetrieve {
			// Add a goroutine to the WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Select a random client to do the work needed for the current chunk
				client := _TEMP_getRandomItemFromMap(clientPool.clients)
				startFrame := currentFrame
				endFrame := currentFrame + FramesPerBatch - 1
				frameBatch := NewFrameBatchMetadata(client.id, startFrame, endFrame)
				frameBatchMap.AddFrameBatch(frameBatch)
				client.RequestWork(currentFrame, endFrame)
			}()
			currentFrame += FramesPerBatch
		}
		// Wait for all goroutines to finish before continuing or sending a signal
		wg.Wait()
		triggerFrameBroadcaster <- true
	}
}

func _TEMP_getRandomItemFromMap(m map[*Client]bool) *Client { // This will get replaced by an dispatching algorithm
	// Convert map keys to a slice
	keys := make([]*Client, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	// Select a random key
	randomKey := keys[rand.Intn(len(keys))]

	// Return the random key and the corresponding value
	return randomKey
}
