package main

import "fmt"

type Message struct {
	ok   bool
	work *Work
}

const MessageSize = WorkSize + 1 // bytes

func NewReceivedMessage(data [MessageSize]byte) *Message {
	// fmt.Println("Received message")
	// fmt.Println(data)
	newWork, err := NewWork(data[1:])
	if err != nil {
		return &Message{
			ok: false,
		}
	}
	return &Message{
		ok:   true,
		work: newWork,
	}
}

func messageReceiver() {
	for {
		message := <-messageChan
		if !message.ok {
			fmt.Println("Invalid message received")
			continue
		}
		fmt.Println("Received message")
		chunkChan <- message.work

	}
}
