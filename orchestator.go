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
	taskTimeout             = 2 * time.Second             // how long to wait for a client to return a batch before re-dispatching it

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
					dispatchRenderTask(start, end)
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

// dispatchRenderTask assigns one batch to a client and blocks until that batch has been rendered and stored.
// It returns as soon as the client responds (via the task's done channel).
// If no response arrives within taskTimeout it re-dispatches to a different client when one is available.
// `dispatchRenderTask()` never gives up since abandoning a batch would let AdvanceHead advance over an unwritten slot.
func dispatchRenderTask(startFrame uint32, endFrame uint32) {
	client := clientPool.PickNewClient()
	renderTaskID := client.GenerateNewRenderTaskID()
	frameBatch := NewFrameBatchMetadata(renderTaskID, startFrame, endFrame)
	frameBatchMap.AddFrameBatch(client.id, frameBatch)
	done := frameBatch.done // same channel travels with the task across executor switches

	log.Println("Sending RenderTask for frames", startFrame, "to", endFrame, "to client", client.id)
	client.RequestWork(renderTaskID, startFrame, endFrame)

	for attempt := 1; ; attempt++ {
		select {
		case <-done:
			frameBatchMap.DeleteRenderTask(client.id, renderTaskID)
			return
		case <-time.After(taskTimeout): // timeout exceeded, picking new client
			next := clientPool.PickNewClient()
			if next.id == client.id {
				log.Println("Attempt #", attempt, "timed out, retrying with the same client", client.id)
			} else {
				log.Println("Attempt #", attempt, "timed out, switching executor to client", next.id)
				renderTaskID = frameBatchMap.SwitchRenderTaskExecutor(renderTaskID, client.id, next)
				client = next
			}
			client.RequestWork(renderTaskID, startFrame, endFrame)
		}
	}
}
