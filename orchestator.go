package main

import (
	"fmt"
	"math/rand"
)

var chunkChan = make(chan *Work) // For now, 1 Chunk = 1 Work, Which is 60 frames = 1 second
var chunkQueue = make([]Work, 0)

var triggerWorkDispatcher = make(chan bool)

func frameOrchestrator() {
	go chunkBroadcaster()
	go workDispatcher()
	triggerWorkDispatcher <- true // Trigger the workDispatcher for the first time
}

const (
	FramesPerChunk      = 60
	QueueLength         = 12                // This allows for storing up to 12 chunks (12 seconds of animation)
	SendThreshold       = 6                 // This is how many chunks we'll send at a time
	NumChunksToRetreive = SendThreshold * 2 // When fetching, retrieve double what is needed to trigger a fetch
)

func chunkBroadcaster() {
	for {
		work := <-chunkChan
		chunkBuffer.Push(*work)

		if chunkBuffer.Size() < SendThreshold { // We haven't gathered enough chunks to send
			fmt.Println("Not enough chunks to send. Waiting...")
			continue
		}

		fmt.Println("Received enough chunks to send. Sending...")
		var chunksToSend []Work
		for i := 0; i < SendThreshold; i++ {
			if chunk, err := chunkBuffer.Get(i); err == nil {
				chunksToSend = append(chunksToSend, chunk)
			}
		}

		// Broadcast the chunks to all clients
		sendChunksToAllClients(chunksToSend)

		// Dequeuing the sent chunks
		chunkBuffer.RemoveFirstNElements(SendThreshold)

		// Check if we have enough chunks to trigger a fetch
		if chunkBuffer.Size() > NumChunksToRetreive {
			triggerWorkDispatcher <- true
		}
	}
}

func workDispatcher() {
	for {
		<-triggerWorkDispatcher
		err := dispatchWork()
		if err != nil {
			fmt.Println(err)
		}
	}
}

func dispatchWork() error {
	if len(pool.clients) == 0 {
		<-poolIsNotEmpty // Wait until the pool is not empty, meaning we have clients to dispatch work to
	}
	var currentFrame uint32
	if len(chunkQueue) == 0 {
		currentFrame = 0
	} else {
		currentFrame = chunkQueue[len(chunkQueue)-1].endFrame + 1
	}
	for range NumChunksToRetreive {
		// Select a random client to do the work needed for the current chunk
		client := _TEMP_getRandomItemFromMap(pool.clients)
		err := client.RequestWork(currentFrame, currentFrame+60)
		if err != nil {
			return err
		}
		currentFrame += 60
	}
	return nil
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
