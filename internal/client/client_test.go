package client

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Issaminu/distributed-donut/internal/protocol"
)

func TestClientID(t *testing.T) {
	c := &Client{id: 42}
	if c.ID() != 42 {
		t.Errorf("ID() = %d, want 42", c.ID())
	}
}

func TestGenerateNewRenderTaskIDIsMonotonic(t *testing.T) {
	c := &Client{}
	for want := uint32(0); want < 5; want++ {
		if got := c.GenerateNewRenderTaskID(); got != want {
			t.Fatalf("GenerateNewRenderTaskID() = %d, want %d", got, want)
		}
	}
}

func TestGenerateNewRenderTaskIDIsConcurrencySafe(t *testing.T) {
	c := &Client{}
	const n = 1000

	var wg sync.WaitGroup
	ids := make([]uint32, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i] = c.GenerateNewRenderTaskID()
		}(i)
	}
	wg.Wait()

	seen := make(map[uint32]bool, n)
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate render task ID %d handed out", id)
		}
		seen[id] = true
	}
}

// validResultMessage builds a full RenderResult message (type byte + body) for
// the given task ID, with a zeroed frame batch of the correct size.
func validResultMessage(taskID uint32) []byte {
	msg := make([]byte, 1+4+protocol.BatchSize)
	msg[0] = protocol.MessageTypeRenderResult
	binary.BigEndian.PutUint32(msg[1:5], taskID)
	return msg
}

func TestHandleReceivedMessageInvokesHandler(t *testing.T) {
	var (
		calls       int
		gotClientID uint32
		gotTaskID   uint32
	)
	c := &Client{
		id: 7,
		onResult: func(clientID uint32, result *protocol.RenderResult) error {
			calls++
			gotClientID = clientID
			gotTaskID = result.ID
			return nil
		},
	}

	if err := c.HandleReceivedMessage(validResultMessage(99)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("handler called %d times, want 1", calls)
	}
	if gotClientID != 7 {
		t.Errorf("handler clientID = %d, want 7", gotClientID)
	}
	if gotTaskID != 99 {
		t.Errorf("handler result ID = %d, want 99", gotTaskID)
	}
}

func TestHandleReceivedMessageRejectsEmpty(t *testing.T) {
	c := &Client{}
	if err := c.HandleReceivedMessage(nil); err == nil {
		t.Fatal("expected error for an empty message")
	}
}

func TestHandleReceivedMessageIgnoresNonResultTypes(t *testing.T) {
	called := false
	c := &Client{onResult: func(uint32, *protocol.RenderResult) error { called = true; return nil }}

	// A RenderTask-typed message should be ignored by a client (only the server
	// receives results).
	msg := []byte{protocol.MessageTypeRenderTask, 0, 0, 0, 0}
	if err := c.HandleReceivedMessage(msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("handler should not be invoked for non-result messages")
	}
}

func TestHandleReceivedMessageWithNilHandler(t *testing.T) {
	c := &Client{} // onResult is nil
	if err := c.HandleReceivedMessage(validResultMessage(1)); err != nil {
		t.Fatalf("unexpected error with nil handler: %v", err)
	}
}

func TestHandleReceivedMessageRejectsMalformedResult(t *testing.T) {
	c := &Client{onResult: func(uint32, *protocol.RenderResult) error { return nil }}
	// Correct type byte, but the body is far too short to be a frame batch.
	msg := []byte{protocol.MessageTypeRenderResult, 0, 0, 0, 1, 0xff}
	if err := c.HandleReceivedMessage(msg); err == nil {
		t.Fatal("expected error for a malformed render result")
	}
}

// A full send queue must drop new messages rather than block the caller (which
// would stall the broadcaster for every other client).
func TestEnqueueDropsWhenQueueIsFull(t *testing.T) {
	c := &Client{id: 1, send: make(chan *websocket.PreparedMessage, 2)}
	prepared, err := websocket.NewPreparedMessage(websocket.BinaryMessage, []byte{1})
	if err != nil {
		t.Fatalf("NewPreparedMessage: %v", err)
	}

	c.enqueue(prepared)
	c.enqueue(prepared)
	c.enqueue(prepared) // must not block; this one is dropped

	if got := len(c.send); got != 2 {
		t.Fatalf("queue len = %d, want 2 (third message should be dropped)", got)
	}
}

func TestRequestWorkEnqueuesAMessage(t *testing.T) {
	c := &Client{id: 1, send: make(chan *websocket.PreparedMessage, clientSendQueueSize)}
	c.RequestWork(3, 60, 119)
	if got := len(c.send); got != 1 {
		t.Fatalf("queue len = %d, want 1", got)
	}
}

// newConnectedClient stands up a throwaway websocket endpoint and returns the
// server-side Client, the client-side peer connection (to observe what the
// Client writes), and a cleanup func.
func newConnectedClient(t *testing.T, onResult ResultHandler) (*Client, *websocket.Conn, func()) {
	t.Helper()

	ch := make(chan *Client, 1)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		ch <- NewClient(conn, onResult)
	}))

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	peer, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}

	var c *Client
	select {
	case c = <-ch:
	case <-time.After(2 * time.Second):
		peer.Close()
		srv.Close()
		t.Fatal("timed out waiting for the server to accept the connection")
	}

	return c, peer, func() {
		peer.Close()
		srv.Close()
	}
}

func TestWritePumpDeliversEnqueuedMessages(t *testing.T) {
	c, peer, cleanup := newConnectedClient(t, nil)
	defer cleanup()

	go c.WritePump()

	want := []byte{0x9, 0x8, 0x7}
	prepared, err := websocket.NewPreparedMessage(websocket.BinaryMessage, want)
	if err != nil {
		t.Fatalf("NewPreparedMessage: %v", err)
	}
	c.enqueue(prepared)

	if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	mt, got, err := peer.ReadMessage()
	if err != nil {
		t.Fatalf("peer read: %v", err)
	}
	if mt != websocket.BinaryMessage {
		t.Errorf("message type = %d, want binary", mt)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	c.Close()
}

func TestCloseRemovesFromPoolAndIsIdempotent(t *testing.T) {
	pool := NewClientPool(0)
	c, _, cleanup := newConnectedClient(t, nil)
	defer cleanup()

	pool.AddClient(c)
	if got := pool.GetClientCount(); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}

	c.Close()
	c.Close() // second call must be a no-op, not a panic (closeOnce)

	if got := pool.GetClientCount(); got != 0 {
		t.Fatalf("count after Close = %d, want 0", got)
	}
}
