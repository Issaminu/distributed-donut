package main

import (
	"encoding/binary"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// clientIDCounter hands out monotonically increasing, never-reused client IDs.
var clientIDCounter atomic.Uint32

const (
	writeTimeout        = 30 * time.Second // a write that can't complete in this long means a dead client
	clientSendQueueSize = 15               // outbound messages we'll buffer before deciding a client is too slow
)

type Client struct {
	id             uint32
	conn           *websocket.Conn
	mutex          sync.Mutex
	numRenderTasks uint32
	send           chan []byte   // outbound messages, drained by the writer goroutine
	quit           chan struct{} // closed to stop the writer goroutine
	closeOnce      sync.Once
}

func NewClient(ws *websocket.Conn) *Client {
	return &Client{
		id:   clientIDCounter.Add(1),
		conn: ws,
		send: make(chan []byte, clientSendQueueSize),
		quit: make(chan struct{}),
	}
}

// Goroutine that handles sending messages to a client.
// Each connected client has it's own independant writePump().
func (c *Client) writePump() {
	for {
		select {
		case <-c.quit:
			return
		case msg := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				log.Println(err)
				c.close()
				return
			}
			if err := c.conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
				log.Println(err)
				c.close() // dead connection; tear down so the read loop unblocks too
				return
			}
		}
	}
}

// enqueue hands a message to the client's writer without blocking. A full queue
// means the client can't keep up, so we drop the message rather than stall
// everyone else.
func (c *Client) enqueue(msg []byte) {
	select {
	case c.send <- msg:
	default:
		log.Printf("Client %d send queue full, dropping message", c.id)
	}
}

// close tears the client down exactly once: stops the writer, closes the
// connection (which unblocks the reader), and removes it from the pool.
func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.quit)
		c.conn.Close()
		clientPool.RemoveClient(c)
	})
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
	for _, client := range clientPool.Snapshot() {
		client.enqueue(encodedFrames)
	}
}

func (c *Client) RequestWork(renderTaskID uint32, startFrame uint32, endFrame uint32) {
	requestedWork := make([]byte, 13) //  1 byte for message type + 4 bytes for RenderTask ID + 4 bytes for startFrame + 4 bytes for endFrame
	requestedWork[0] = MessageTypeRenderTask
	binary.BigEndian.PutUint32(requestedWork[1:5], renderTaskID)
	binary.BigEndian.PutUint32(requestedWork[5:9], startFrame)
	binary.BigEndian.PutUint32(requestedWork[9:13], endFrame)
	c.enqueue(requestedWork)
}
