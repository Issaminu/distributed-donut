package main

import (
	"encoding/binary"
	"fmt"

	"github.com/gorilla/websocket"
)

type Client struct {
	id   uint16
	conn *websocket.Conn
}

func NewClient(ws *websocket.Conn) *Client {
	return &Client{
		id:   uint16(clientPool.GetClientCount()),
		conn: ws,
	}
}

const (
	MessageTypeRenderTask     = 0x0 // 0000 - Represents requesting work (compute) from workers/clients
	MessageTypeRenderResult   = 0x1 // 0001 - Represents delivering rendered frame chunk(s) from a worker/client to the orchestrator
	MessageTypeFrameBroadcast = 0x2 // 0010 - Represents broadcasting frame chunk(s) to all workers/clients
)

func (c *Client) handleReceivedMessage(data []byte) {
	messageType := data[0]
	if messageType != MessageTypeRenderResult {
		fmt.Println("Invalid message type received")
	}
	fmt.Println("Received a rendering result")
	renderResult, err := NewRenderResult(binary.BigEndian.Uint16(data[1:3]), data[3:])
	frameBatchMap.AddRenderResultToFrameBatch(c.id, renderResult)
	if err != nil {
		fmt.Println(err)
		return
	}

}

func sendFramesToAllClients(frames []byte) {
	encodedFrames := make([]byte, len(frames)+1) // +1 to include the message type byte
	encodedFrames[0] = MessageTypeFrameBroadcast
	copy(encodedFrames[1:], frames)
	for client := range clientPool.clients {
		go client.sendChunk(encodedFrames)
	}
}

func (c *Client) sendChunk(data []byte) {
	err := c.conn.WriteMessage(websocket.BinaryMessage, data[:])
	if err != nil {
		fmt.Println(err)
	}
}

func (c *Client) RequestWork(startFrame uint32, endFrame uint32) {
	requestedWork := make([]byte, 11) // 8 bytes for frames + 1 byte for message type + 2 bytes for RenderTask ID
	requestedWork[0] = MessageTypeRenderTask
	binary.BigEndian.PutUint16(requestedWork[1:3], c.id)
	binary.BigEndian.PutUint32(requestedWork[3:7], startFrame)
	binary.BigEndian.PutUint32(requestedWork[7:11], endFrame)
	err := c.conn.WriteMessage(websocket.BinaryMessage, requestedWork[:])
	if err != nil {
		fmt.Println(err)
	}
}
