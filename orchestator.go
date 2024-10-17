package main

import (
	"context"
	"log"
	"math/rand"
	"time"
)

var triggerTaskDispatcher = make(chan bool, 1)
var isFirstBroadcast = true
var triggerFrameBroadcaster = make(chan bool, 1)
var logChan = make(chan []byte)

func frameOrchestrator(ctx context.Context) {
	frameBufferSizeCheck.ready <- true // Set frameBufferSizeCheck to be ready for the first time
	go frameBufferSizeChecker(ctx)
	go frameBroadcaster(ctx)
	go taskDispatcher(ctx)
	go consoleDrawer(ctx)
	triggerTaskDispatcher <- true // Trigger the taskDispatcher for the first time
}

const (
	FramesPerBatch          = 60      // Frames per batch/second
	MaxBatches              = 30 * 60 // 1800 batches/seconds ~= 30 Minutes (Could go longer but it would consume much more memory for no benefit)
	MaxFrames               = MaxBatches * FramesPerBatch
	BufferSize              = MaxFrames * FrameSize // Buffer size in bytes
	FirstSecondsToBroadcast = 6                     // For the very first broadcast, send 6 seconds worth of frames. Given SecondsToBroadcast being 4, this means we'll always have 2 seconds of additional buffer on the clients.
	SecondsToBroadcast      = 4                     // Number of seconds to wait before broadcasting the frames (For all broadcasts except the first). When broadcasting frames, we send 4 seconds worth of frames.
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
			frames := frameBuffer.GetFrames()
			// logChan <- frames
			sendFramesToAllClients(frames)
			time.Sleep(SecondsToBroadcast * time.Second) // Sleep for the specified number of seconds before sending again
			frameBuffer.RemoveSentFramesFromBuffer()
			if isFirstBroadcast {
				isFirstBroadcast = false
			}
			frameBufferSizeCheck.ready <- true
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

			var currentFrame = uint32(frameBuffer.GetNextFrameNumber())

			batchesToFetch := SecondsToBroadcast
			if isFirstBroadcast {
				batchesToFetch = FirstSecondsToBroadcast
			}
			for range batchesToFetch {
				// Select a random client to do the work needed for the current batch
				startFrame := (currentFrame) % MaxFrames
				endFrame := (currentFrame + FramesPerBatch - 1) % MaxFrames
				currentFrame += FramesPerBatch
				go dispatchRenderTask(startFrame, endFrame, 1, 0, nil)
			}
		}
	}
}

func dispatchRenderTask(startFrame uint32, endFrame uint32, attempt int, previousRenderTaskID uint32, previousClient *Client) { // For the first attempt, attempt is 1, previousRenderTaskID is 0 and previousClient is nil
	renderTaskID := previousRenderTaskID
	newClient := _TEMP_getRandomItemFromMap(clientPool.clients)

	if attempt == 1 { // First attempt
		renderTaskID = newClient.GenerateNewRenderTaskID()
		log.Println("Sending RenderTask for frames", startFrame, "to", endFrame, "to client", newClient.id)
		frameBatch := NewFrameBatchMetadata(renderTaskID, startFrame, endFrame)
		frameBatchMap.AddFrameBatch(newClient.id, frameBatch)
	} else { // Subsequent attempts
		if newClient.id == previousClient.id {
			log.Println("Attempt #", attempt-1, "failed, retrying with the same render task executor")
		} else {
			log.Println("Attempt #", attempt-1, "failed, switching render task executor to", newClient.id)
			renderTaskID = frameBatchMap.SwitchRenderTaskExecutor(previousRenderTaskID, previousClient.id, newClient) // Switch the render task executor to the new client, and receive the new RenderTask ID
		}
	}
	newClient.RequestWork(renderTaskID, startFrame, endFrame)
	time.Sleep(time.Second * 2)
	if !frameBatchMap.isRenderTaskCompleted(newClient.id, renderTaskID) {
		dispatchRenderTask(startFrame, endFrame, attempt+1, renderTaskID, newClient)
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
		case <-frameBufferSizeCheck.trigger: // No `default` on this `select`, this is blocking execution on this thread while waiting for a message on the channel
			if frameBuffer.IsBufferSizeSufficientForBroadcast() {
				select {
				case <-frameBufferSizeCheck.ready:
					log.Println("Gathered enough frames, triggering a broadcast...")
					triggerFrameBroadcaster <- true
				default: // Default is important, as in Go, it makes the `case` above not wait until it's ready. Otherwise, if we didn't have a `default`, we'd block execution while the channel is empty
					continue
				}
			}
		}
	}
}
