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
	p := NewClientPool(0)
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

func TestClientPoolEnforcesMaxClients(t *testing.T) {
	p := NewClientPool(2)

	c1, c2, c3 := testClient(1), testClient(2), testClient(3)
	if !p.AddClient(c1) || !p.AddClient(c2) {
		t.Fatal("AddClient rejected a client while under capacity")
	}
	if p.AddClient(c3) {
		t.Fatal("AddClient admitted a client beyond the cap")
	}
	if got := p.GetClientCount(); got != 2 {
		t.Fatalf("count = %d, want 2 (rejected client must not be stored)", got)
	}
	if c3.pool != nil {
		t.Error("rejected client must not get the pool back-reference")
	}

	// Freeing a slot lets a new client in.
	p.RemoveClient(c1)
	if !p.AddClient(c3) {
		t.Fatal("AddClient rejected a client after a slot was freed")
	}
}

func TestClientPoolZeroMaxIsUnlimited(t *testing.T) {
	p := NewClientPool(0)
	for i := 0; i < 50; i++ {
		if !p.AddClient(testClient(uint32(i))) {
			t.Fatalf("AddClient rejected client %d with an uncapped pool", i)
		}
	}
}

func TestClientPoolSnapshot(t *testing.T) {
	p := NewClientPool(0)
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
	p := NewClientPool(0)
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
	p := NewClientPool(0)

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
	p := NewClientPool(0)
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
