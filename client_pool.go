package main

import (
	"math/rand"
	"sync"
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
