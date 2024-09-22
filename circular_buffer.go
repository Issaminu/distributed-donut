package main

import (
	"errors"
	"sync"
)

type CircularBuffer struct {
	buffer [QueueLength]FrameBatchMetadata
	head   int
	tail   int
	size   int
	mutex  sync.RWMutex
}

func NewCircularBuffer() *CircularBuffer {
	return &CircularBuffer{}
}

func (cb *CircularBuffer) Push(work *FrameBatchMetadata) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.buffer[cb.tail] = *work
	cb.tail = (cb.tail + 1) % QueueLength

	if cb.size < QueueLength {
		cb.size++
	} else {
		cb.head = (cb.head + 1) % QueueLength
	}
}

func (cb *CircularBuffer) Get(index int) (FrameBatchMetadata, error) {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	if index < 0 || index >= cb.size {
		return FrameBatchMetadata{}, errors.New("index out of range")
	}

	actualIndex := (cb.head + index) % QueueLength
	return cb.buffer[actualIndex], nil
}

func (cb *CircularBuffer) RemoveFirstNElements(n int) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if n > cb.size {
		n = cb.size
	}

	cb.head = (cb.head + n) % QueueLength
	cb.size -= n
}

func (cb *CircularBuffer) Size() int {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.size
}

func (cb *CircularBuffer) Clear() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	cb.head = 0
	cb.tail = 0
	cb.size = 0
}
