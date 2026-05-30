package client

import (
	"log"
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
	encodedFrames := protocol.EncodeFrameBroadcast(frames)

	prepared, err := websocket.NewPreparedMessage(websocket.BinaryMessage, encodedFrames)
	if err != nil {
		log.Println("Failed to prepare broadcast message:", err)
		return
	}
	for _, client := range p.Snapshot() {
		client.enqueue(prepared)
	}
}
