// Package orchestrator is the central coordinator: it dispatches render tasks
// to connected clients, collects the resulting frame batches into the shared
// buffer, and periodically broadcasts buffered frames back to everyone.
package orchestrator

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/Issaminu/distributed-donut/internal/buffer"
	"github.com/Issaminu/distributed-donut/internal/client"
	"github.com/Issaminu/distributed-donut/internal/protocol"
)

const (
	firstSecondsToBroadcast = 6               // For the very first broadcast, send 6 seconds worth of frames. Given secondsToBroadcast being 4, this means we'll always have 2 seconds of additional buffer on the clients.
	secondsToBroadcast      = 4               // Number of seconds to wait before broadcasting the frames (For all broadcasts except the first). When broadcasting frames, we send 4 seconds worth of frames.
	taskTimeout             = 2 * time.Second // how long to wait for a client to return a batch before re-dispatching it
)

// Orchestrator owns the rendering pipeline. Construct it with New, then call Run to start its background loops.
type Orchestrator struct {
	buffer   *buffer.FrameBuffer
	clients  *client.ClientPool
	batchMap *FrameBatchMap
}

func New(buf *buffer.FrameBuffer, pool *client.ClientPool) *Orchestrator {
	return &Orchestrator{
		buffer:   buf,
		clients:  pool,
		batchMap: NewFrameBatchMap(buf),
	}
}

// Run starts the broadcaster and task-dispatcher loops. They stop when ctx is cancelled.
func (o *Orchestrator) Run(ctx context.Context) {
	go o.broadcaster(ctx)
	go o.dispatcher(ctx)
}

// HandleResult stores a render result a client sent back. Its signature
// satisfies client.ResultHandler, so it can be wired directly into NewClient.
func (o *Orchestrator) HandleResult(clientID uint32, result *protocol.RenderResult) error {
	return o.batchMap.SaveRenderResult(clientID, result)
}

func (o *Orchestrator) broadcaster(ctx context.Context) {
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

			o.buffer.WaitUntilBufferSizeEnoughForBroadcast(seconds)

			log.Println("Gathered enough frames, triggering a broadcast...")
			o.clients.WaitForAtLeastOne() // No point broadcasting into the void

			frames := o.buffer.GetFramesToBroadcast(seconds)
			o.clients.Broadcast(frames)
			o.buffer.RemoveSentFramesFromBuffer(seconds)
			if isFirstBroadcast {
				isFirstBroadcast = false
			}
			time.Sleep(secondsToBroadcast * time.Second) // Sleep before allowing the next broadcast, so clients consume what we just sent
		}
	}
}

func (o *Orchestrator) dispatcher(ctx context.Context) {
	isFirstRound := true // the first round prefetches more, see batchesToFetch below

	for {
		select {
		case <-ctx.Done():
			log.Println("Task dispatcher shutting down...")
			return
		default:
			batchesToFetch := secondsToBroadcast
			if isFirstRound {
				batchesToFetch = firstSecondsToBroadcast
			}

			sleep := o.buffer.WaitForRoom(batchesToFetch, secondsToBroadcast*time.Second)
			o.clients.WaitForAtLeastOne() // no point dispatching with no workers connected
			log.Printf("Triggering task dispatcher in %0.2f seconds", sleep.Seconds())
			time.Sleep(sleep)

			log.Println("Task dispatcher triggered")

			var currentFrame = uint32(o.buffer.GetNextFrameNumber())

			var wg sync.WaitGroup
			for range batchesToFetch {
				// Select a random client to do the work needed for the current batch
				startFrame := (currentFrame) % buffer.MaxFrames
				endFrame := (currentFrame + protocol.FramesPerBatch - 1) % buffer.MaxFrames
				currentFrame += protocol.FramesPerBatch
				wg.Add(1)
				go func(start, end uint32) {
					defer wg.Done()
					o.dispatchRenderTask(start, end)
				}(startFrame, endFrame)
			}
			wg.Wait() // Wait for every task batch to finish in this round before broadcasting

			o.buffer.AdvanceHead(batchesToFetch)
			isFirstRound = false
		}
	}
}

// dispatchRenderTask assigns one batch to a client and blocks until that batch has been rendered and stored.
// It returns as soon as the client responds (via the task's done channel).
// If no response arrives within taskTimeout it re-dispatches to a different client when one is available.
// dispatchRenderTask never gives up since abandoning a batch would let AdvanceHead advance over an unwritten slot.
func (o *Orchestrator) dispatchRenderTask(startFrame uint32, endFrame uint32) {
	worker := o.clients.PickNewClient()
	renderTaskID := worker.GenerateNewRenderTaskID()
	frameBatch := NewFrameBatchMetadata(renderTaskID, startFrame, endFrame)
	o.batchMap.AddFrameBatch(worker.ID(), frameBatch)
	done := frameBatch.done // same channel travels with the task across executor switches

	log.Println("Sending RenderTask for frames", startFrame, "to", endFrame, "to client", worker.ID())
	worker.RequestWork(renderTaskID, startFrame, endFrame)

	for attempt := 1; ; attempt++ {
		select {
		case <-done:
			o.batchMap.DeleteRenderTask(worker.ID(), renderTaskID)
			return
		case <-time.After(taskTimeout): // timeout exceeded, picking new client
			next := o.clients.PickNewClient()
			if next.ID() == worker.ID() {
				log.Println("Attempt #", attempt, "timed out, retrying with the same client", worker.ID())
			} else {
				log.Println("Attempt #", attempt, "timed out, switching executor to client", next.ID())
				renderTaskID = o.batchMap.SwitchRenderTaskExecutor(renderTaskID, worker.ID(), next)
				worker = next
			}
			worker.RequestWork(renderTaskID, startFrame, endFrame)
		}
	}
}
