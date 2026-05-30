package client

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// testClient builds a Client without a live connection, which is enough for the
// pool's bookkeeping (it never touches the websocket).
func testClient(id uint32) *Client {
	return &Client{
		id:   id,
		send: make(chan *websocket.PreparedMessage, clientSendQueueSize),
		quit: make(chan struct{}),
	}
}

func TestClientPoolAddRemoveCount(t *testing.T) {
	p := NewClientPool()
	if got := p.GetClientCount(); got != 0 {
		t.Fatalf("new pool count = %d, want 0", got)
	}

	c1, c2 := testClient(1), testClient(2)
	p.AddClient(c1)
	p.AddClient(c2)
	if got := p.GetClientCount(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	if c1.pool != p {
		t.Error("AddClient did not set the client.pool back-reference")
	}

	p.RemoveClient(c1)
	if got := p.GetClientCount(); got != 1 {
		t.Fatalf("count after remove = %d, want 1", got)
	}
}

func TestClientPoolSnapshot(t *testing.T) {
	p := NewClientPool()
	c1, c2 := testClient(1), testClient(2)
	p.AddClient(c1)
	p.AddClient(c2)

	snap := p.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	seen := map[*Client]bool{}
	for _, c := range snap {
		seen[c] = true
	}
	if !seen[c1] || !seen[c2] {
		t.Error("snapshot is missing a client")
	}
}

func TestPickNewClientReturnsAMember(t *testing.T) {
	p := NewClientPool()
	members := map[*Client]bool{}
	for _, c := range []*Client{testClient(1), testClient(2), testClient(3)} {
		p.AddClient(c)
		members[c] = true
	}
	for i := 0; i < 50; i++ {
		if got := p.PickNewClient(); !members[got] {
			t.Fatalf("PickNewClient returned a non-member: %p", got)
		}
	}
}

func TestWaitForAtLeastOneUnblocksOnAdd(t *testing.T) {
	p := NewClientPool()

	done := make(chan struct{})
	go func() {
		p.WaitForAtLeastOne()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("WaitForAtLeastOne returned on an empty pool")
	case <-time.After(50 * time.Millisecond):
	}

	p.AddClient(testClient(1))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForAtLeastOne did not unblock after AddClient")
	}
}

func TestBroadcastEnqueuesToEveryClient(t *testing.T) {
	p := NewClientPool()
	c1, c2 := testClient(1), testClient(2)
	p.AddClient(c1)
	p.AddClient(c2)

	p.Broadcast([]byte{1, 2, 3})

	for _, c := range []*Client{c1, c2} {
		if got := len(c.send); got != 1 {
			t.Errorf("client %d queue len = %d, want 1", c.id, got)
		}
	}
}
