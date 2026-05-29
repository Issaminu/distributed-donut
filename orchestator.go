package main

import (
	"context"
	"log"
	"sync"
	"time"
)

var logChan = make(chan []byte)

func frameOrchestrator(ctx context.Context) {
	go frameBroadcaster(ctx)
	go taskDispatcher(ctx)
}

const (
	FramesPerBatch          = 60                          // Frames per batch/second
	MaxBatches              = 30 * 60                     // 1800 batches/seconds ~= 30 Minutes (Could go longer but it would consume much more memory for no benefit)
	MaxFrames               = MaxBatches * FramesPerBatch // Maximum amount of frames we can hold in the buffer
	BufferSize              = MaxFrames * FrameSize       // Buffer size in bytes
	firstSecondsToBroadcast = 6                           // For the very first broadcast, send 6 seconds worth of frames. Given secondsToBroadcast being 4, this means we'll always have 2 seconds of additional buffer on the clients.
	secondsToBroadcast      = 4                           // Number of seconds to wait before broadcasting the frames (For all broadcasts except the first). When broadcasting frames, we send 4 seconds worth of frames.
)

func frameBroadcaster(ctx context.Context) {
	var isFirstBroadcast = true
	for {
		select {
		case <-ctx.Done():
			log.Println("Frame broadcaster shutting down...")
			return
		default:
			seconds := secondsToBroadcast
			if isFirstBroadcast {
				seconds = firstSecondsToBroadcast
			}

			frameBuffer.WaitUntilBufferSizeEnoughForBroadcast(seconds)

			log.Println("Gathered enough frames, triggering a broadcast...")
			clientPool.WaitForAtLeastOne() // No point broadcasting into the void

			frames := frameBuffer.GetFramesToBroadcast(seconds)
			sendFramesToAllClients(frames)
			frameBuffer.RemoveSentFramesFromBuffer(seconds)
			if isFirstBroadcast {
				isFirstBroadcast = false
			}
			time.Sleep(secondsToBroadcast * time.Second) // Sleep before allowing the next broadcast, so clients consume what we just sent
		}
	}
}

func taskDispatcher(ctx context.Context) {
	var isFirstBroadcast = true

	for {
		select {
		case <-ctx.Done():
			log.Println("Task dispatcher shutting down...")
			return
		default:
			batchesToFetch := secondsToBroadcast
			if isFirstBroadcast {
				batchesToFetch = firstSecondsToBroadcast
			}

			frameBuffer.WaitForRoom(batchesToFetch)

			log.Println("Task dispatcher triggered")

			var currentFrame = uint32(frameBuffer.GetNextFrameNumber())

			var wg sync.WaitGroup
			for range batchesToFetch {
				// Select a random client to do the work needed for the current batch
				startFrame := (currentFrame) % MaxFrames
				endFrame := (currentFrame + FramesPerBatch - 1) % MaxFrames
				currentFrame += FramesPerBatch
				wg.Add(1)
				go func(start, end uint32) {
					defer wg.Done()
					dispatchRenderTask(start, end, 1, 0, nil)
				}(startFrame, endFrame)
			}
			wg.Wait() // Wait for every task batch to finish in this round before broadcasting

			if isFirstBroadcast {
				isFirstBroadcast = false
			}

			frameBuffer.AdvanceHead(batchesToFetch)
		}
	}
}

func dispatchRenderTask(startFrame uint32, endFrame uint32, attempt int, previousRenderTaskID uint32, previousClient *Client) { // For the first attempt, attempt is 1, previousRenderTaskID is 0 and previousClient is nil
	renderTaskID := previousRenderTaskID
	newClient := clientPool.PickNewClient()

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
