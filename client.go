package main

import (
	"encoding/binary"
	"fmt"

	"github.com/gorilla/websocket"
)

type Client struct {
	id *websocket.Conn
}

func NewClient(ws *websocket.Conn) *Client {
	return &Client{
		id: ws,
	}
}

const (
	MessageTypeWorkRequest    = 0x0 // 0000 - Represents requesting work (compute) from a client
	MessageTypeWorkDelivery   = 0x1 // 0001 - Represents delivering frame chunk(s) to a client
	MessageTypeFrameBroadcast = 0x2 // 0010 - Represents broadcasting frame chunk(s) to all clients
)

func encodeFrames(works []Work) []byte {
	encodedWorks := make([]byte, (len(works)*WorkSize)+1) // +1 to include the message type byte
	encodedWorks[0] = MessageTypeFrameBroadcast
	// fmt.Println(works[0].workPerformed)
	for i, work := range works {
		offset := i*WorkSize + 1
		copy(encodedWorks[offset:], work.workPerformed)
	}
	return encodedWorks
}

func sendChunksToAllClients(works []Work) {
	encodedFrames := encodeFrames(works)
	for client := range pool.clients {
		go client.sendChunk(encodedFrames)
	}
}

func (c *Client) sendChunk(data []byte) {
	err := c.id.WriteMessage(websocket.BinaryMessage, data[:])
	if err != nil {
		fmt.Println(err)
	}
}

func (c *Client) RequestWork(startFrame uint32, endFrame uint32) error {
	requestedWork := make([]byte, 9) // 8 bytes for frames + 1 byte for message type
	requestedWork[0] = MessageTypeWorkRequest
	binary.BigEndian.PutUint32(requestedWork[1:5], startFrame)
	binary.BigEndian.PutUint32(requestedWork[5:9], endFrame)
	err := c.id.WriteMessage(websocket.BinaryMessage, requestedWork[:])
	return err
}
