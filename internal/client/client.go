// Package client models a connected worker (a browser running the donut
// renderer): the websocket plumbing to talk to it, and the pool that tracks all of them.
// Received results are surfaced through an injected ResultHandler so the
// orchestrator can wire in its own storage without creating an import cycle.
package client

import (
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Issaminu/distributed-donut/internal/protocol"
)

// clientIDCounter hands out monotonically increasing, never-reused client IDs.
var clientIDCounter atomic.Uint32

const (
	writeTimeout        = 30 * time.Second // a write that can't complete in this long means a dead client
	clientSendQueueSize = 15               // outbound messages we'll buffer before deciding a client is too slow

	// maxIncomingMessageBytes bounds the size of a message we'll read from a client. The only thing a client ever sends is a RenderResult:
	// 1 byte message type + 4 bytes render task ID + BatchSize bytes of frames.
	// This is done so that a misbehaving client can't stream us unbounded data or a decompression bomb.
	maxIncomingMessageBytes = 1 + 4 + protocol.BatchSize
)

// ResultHandler is called with each RenderResult a client sends back. The
// orchestrator supplies the implementation when constructing a client.
type ResultHandler func(clientID uint32, result *protocol.RenderResult) error

type Client struct {
	id             uint32
	conn           *websocket.Conn
	mutex          sync.Mutex
	numRenderTasks uint32
	send           chan *websocket.PreparedMessage // outbound messages, drained by the writer goroutine. it is a PreparedMessage so its on-wire (compressed) form is computed once
	quit           chan struct{}                   // closed to stop the writer goroutine
	closeOnce      sync.Once
	onResult       ResultHandler // invoked when the client sends back a RenderResult
	pool           *ClientPool   // set when the client is added to a pool, so close() can remove it
}

func NewClient(ws *websocket.Conn, onResult ResultHandler) *Client {
	ws.SetReadLimit(maxIncomingMessageBytes)
	return &Client{
		id:       clientIDCounter.Add(1),
		conn:     ws,
		send:     make(chan *websocket.PreparedMessage, clientSendQueueSize),
		quit:     make(chan struct{}),
		onResult: onResult,
	}
}

// ID returns the client's stable, unique identifier.
func (c *Client) ID() uint32 { return c.id }

// WritePump is the goroutine that handles sending messages to a client.
// Each connected client has its own independent WritePump().
func (c *Client) WritePump() {
	for {
		select {
		case <-c.quit:
			return
		case msg := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				log.Println(err)
				c.Close()
				return
			}
			if err := c.conn.WritePreparedMessage(msg); err != nil {
				log.Println(err)
				c.Close() // dead connection; tear down so the read loop unblocks too
				return
			}
		}
	}
}

// enqueue hands a message to the client's writer without blocking. A full queue
// means the client can't keep up, so we drop the message rather than stall
// everyone else.
func (c *Client) enqueue(msg *websocket.PreparedMessage) {
	select {
	case c.send <- msg:
	default:
		log.Printf("Client %d send queue full, dropping message", c.id)
	}
}

// Close tears the client down exactly once: stops the writer, closes the
// connection (which unblocks the reader), and removes it from the pool.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.quit)
		c.conn.Close()
		if c.pool != nil {
			c.pool.RemoveClient(c)
		}
	})
}

func (c *Client) HandleReceivedMessage(data []byte) error {
	if len(data) == 0 {
		return errors.New("received empty message")
	}
	messageType := data[0]
	if messageType != protocol.MessageTypeRenderResult {
		log.Println("Invalid message type received")
		return nil
	}
	renderResult, err := protocol.NewRenderResult(data[1:])
	if err != nil {
		return err
	}
	if c.onResult == nil {
		return nil
	}
	return c.onResult(c.id, renderResult)
}

func (c *Client) GenerateNewRenderTaskID() uint32 {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	newRenderTaskID := c.numRenderTasks
	c.numRenderTasks++
	return newRenderTaskID
}

func (c *Client) RequestWork(renderTaskID uint32, startFrame uint32, endFrame uint32) {
	requestedWork := protocol.EncodeRenderTask(renderTaskID, startFrame, endFrame)

	// Prepared like the broadcast so the writer goroutine has a single path; this
	// message has one recipient, so there's no compression sharing to gain, just
	// uniformity.
	prepared, err := websocket.NewPreparedMessage(websocket.BinaryMessage, requestedWork)
	if err != nil {
		log.Println("Failed to prepare render task message:", err)
		return
	}
	c.enqueue(prepared)
}
