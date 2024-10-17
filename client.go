package main

import (
	"encoding/binary"
	"errors"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

type Client struct {
	id             uint32
	conn           *websocket.Conn
	mutex          sync.Mutex
	numRenderTasks uint32
}

func NewClient(ws *websocket.Conn) *Client {
	return &Client{
		id:             uint32(clientPool.GetClientCount()),
		conn:           ws,
		numRenderTasks: 0,
	}
}

const (
	MessageTypeRenderTask     = 0x0 // 0000 - Represents requesting work (compute) from workers/clients
	MessageTypeRenderResult   = 0x1 // 0001 - Represents delivering rendered frame batch(s) from a worker/client to the orchestrator
	MessageTypeFrameBroadcast = 0x2 // 0010 - Represents broadcasting frame batch(s) to all workers/clients
)

func (c *Client) HandleReceivedMessage(data []byte) error {
	if len(data) == 0 {
		return errors.New("received empty message")
	}
	messageType := data[0]
	if messageType != MessageTypeRenderResult {
		log.Println("Invalid message type received")
	}
	renderResult, err := NewRenderResult(data[1:])
	if err != nil {
		return err
	}
	return frameBatchMap.SaveRenderResult(c.id, renderResult)
}

func (c *Client) GenerateNewRenderTaskID() uint32 {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	newRenderTaskID := c.numRenderTasks
	c.numRenderTasks++
	return newRenderTaskID
}

func sendFramesToAllClients(frames []byte) {
	encodedFrames := make([]byte, len(frames)+1) // +1 to include the message type byte
	encodedFrames[0] = MessageTypeFrameBroadcast
	copy(encodedFrames[1:], frames)
	for client := range clientPool.clients {
		client.sendFrames(encodedFrames)
	}
}

func (c *Client) sendFrames(data []byte) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	err := c.conn.WriteMessage(websocket.BinaryMessage, data[:])
	if err != nil {
		log.Println(err)
	}
}

func (c *Client) RequestWork(renderTaskID uint32, startFrame uint32, endFrame uint32) {
	requestedWork := make([]byte, 13) // 8 bytes for frames + 1 byte for message type + 4 bytes for RenderTask ID
	requestedWork[0] = MessageTypeRenderTask
	binary.BigEndian.PutUint32(requestedWork[1:5], renderTaskID)
	binary.BigEndian.PutUint32(requestedWork[5:9], startFrame)
	binary.BigEndian.PutUint32(requestedWork[9:13], endFrame)
	c.mutex.Lock()
	defer c.mutex.Unlock()
	err := c.conn.WriteMessage(websocket.BinaryMessage, requestedWork[:])
	if err != nil {
		log.Println(err)
	}
}
