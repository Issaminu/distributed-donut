package client

import (
	"log/slog"
	"math/rand"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/Issaminu/distributed-donut/internal/protocol"
)

type ClientPool struct {
	clients map[*Client]struct{}
	mu      sync.Mutex
	cond    *sync.Cond
}

func NewClientPool() *ClientPool {
	p := &ClientPool{clients: make(map[*Client]struct{})}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *ClientPool) AddClient(client *Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	client.pool = p
	p.clients[client] = struct{}{}
	p.cond.Broadcast()
}

func (p *ClientPool) RemoveClient(client *Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.clients, client)
}

func (p *ClientPool) GetClientCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.clients)
}

// WaitForAtLeastOne parks until the pool has at least one client.
func (p *ClientPool) WaitForAtLeastOne() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.clients) == 0 {
		p.cond.Wait()
	}
}

// Snapshot returns the current clients as a slice so callers can iterate
// without holding the pool's lock.
func (p *ClientPool) Snapshot() []*Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*Client, 0, len(p.clients))
	for c := range p.clients {
		out = append(out, c)
	}
	return out
}

// PickNewClient blocks until the pool has at least one client, then
// returns a randomly chosen one.
// TODO: Replace the randomness by a proper load balancing algorithm.
func (p *ClientPool) PickNewClient() *Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.clients) == 0 {
		p.cond.Wait()
	}
	keys := make([]*Client, 0, len(p.clients))
	for c := range p.clients {
		keys = append(keys, c)
	}
	return keys[rand.Intn(len(keys))]
}

// Broadcast sends one frame batch to every connected client. The message is
// prepared once (so its compressed on-wire form is computed a single time)
// rather than per-client.
func (p *ClientPool) Broadcast(frames []byte) {
	p.broadcastEncoded(protocol.EncodeFrameBroadcast(frames), "frame broadcast")
}

// BroadcastClientCount tells every client how many workers are currently in the
// fleet.
func (p *ClientPool) BroadcastClientCount(count uint32) {
	p.broadcastEncoded(protocol.EncodeClientCount(count), "client count")
}

// BroadcastBufferFullness tells every client how full the server's ring buffer
// is, as a percentage (0-100).
func (p *ClientPool) BroadcastBufferFullness(percent uint8) {
	p.broadcastEncoded(protocol.EncodeBufferFullness(percent), "buffer fullness")
}

// broadcastEncoded prepares an already-encoded message once and fans it out to a
// snapshot of the current clients. what names the message for error logs.
func (p *ClientPool) broadcastEncoded(encoded []byte, what string) {
	clients := p.Snapshot()
	if len(clients) == 0 {
		return // nothing to do, and no point preparing a message for no one
	}
	prepared, err := websocket.NewPreparedMessage(websocket.BinaryMessage, encoded)
	if err != nil {
		slog.Error("failed to prepare broadcast message", "kind", what, "err", err)
		return
	}
	for _, client := range clients {
		client.enqueue(prepared)
	}
}
