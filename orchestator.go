package main

import (
	"context"
	"log"
	"math/rand"
	"time"
)

var triggerTaskDispatcher = make(chan bool)
var isFirstBroadcast = true
var triggerFrameBroadcaster = make(chan bool)

type FrameBufferSizeCheck struct {
	trigger chan bool
	locked  bool
}

var frameBufferSizeCheck = FrameBufferSizeCheck{
	trigger: make(chan bool),
	locked:  false,
}

func frameOrchestrator(ctx context.Context) {
	go frameBroadcaster(ctx)
	go taskDispatcher(ctx)
	go frameBufferSizeChecker(ctx)
	triggerTaskDispatcher <- true // Trigger the taskDispatcher for the first time
}

const (
	FramesPerBatch          = 60                              // Frames per batch/second
	BufferSize              = FrameSize * FramesPerBatch * 20 // Storing 20 seconds worth of frames in the buffer (WIP)
	NumBatchesToRetrieve    = 12                              // Send 12 rendering tasks to clients.
	FirstSecondsToBroadcast = 6                               // For the very first broadcast, send 6 seconds worth of frames. Given SecondsToBroadcast being 4, this means we'll always have 2 seconds of additional buffer on the clients.
	SecondsToBroadcast      = 4                               // Number of seconds to wait before broadcasting the frames (For all broadcasts except the first). When broadcasting frames, we send 4 seconds worth of frames.
)

func frameBroadcaster(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("Frame broadcaster shutting down...")
			return
		case <-triggerFrameBroadcaster:

			if len(clientPool.clients) == 0 {
				<-clientPoolIsNotEmpty // Wait until the pool is not empty, meaning we have clients to dispatch rendering tasks to
			}
			frames := frameBuffer.GetOrderedFrames()
			sendFramesToAllClients(frames)
			time.Sleep(SecondsToBroadcast * time.Second) // Sleep for the specified number of seconds before sending again
			frameBuffer.RemoveSentFramesFromBuffer()
			frameBufferSizeCheck.locked = false
			triggerTaskDispatcher <- true
		}
	}
}

func taskDispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("Task dispatcher shutting down...")
			return
		case <-triggerTaskDispatcher:

			if len(clientPool.clients) == 0 {
				<-clientPoolIsNotEmpty // Wait until the pool is not empty, meaning we have clients to dispatch rendering tasks to
			}
			log.Println("Task dispatcher triggered")

			var currentFrame = uint32(frameBuffer.GetLength())

			for range NumBatchesToRetrieve {
				// Select a random client to do the work needed for the current batch
				client := _TEMP_getRandomItemFromMap(clientPool.clients)
				startFrame := currentFrame
				endFrame := currentFrame + FramesPerBatch - 1
				log.Println("Sending work for frames", startFrame, "to", endFrame, "to client", client.id)
				frameBatch := NewFrameBatchMetadata(client.id, startFrame, endFrame)
				frameBatchMap.AddFrameBatch(frameBatch)
				client.RequestWork(frameBatch.renderTask.id, currentFrame, endFrame)
				currentFrame += FramesPerBatch
			}
		}
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

func frameBufferSizeChecker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-frameBufferSizeCheck.trigger:
			if frameBufferSizeCheck.locked {
				continue
			}
			if frameBuffer.IsBufferSizeSufficientForBroadcast(isFirstBroadcast) {
				frameBufferSizeCheck.locked = true
				log.Println("Gathered enough frames, triggering a broadcast...")
				if isFirstBroadcast {
					isFirstBroadcast = false
				}
				triggerFrameBroadcaster <- true
			}
		}
	}
}
