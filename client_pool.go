package main

var clientPoolIsNotEmpty = make(chan bool)

type ClientPool struct {
	clients map[*Client]bool
}

func NewClientPool() *ClientPool {
	return &ClientPool{
		clients: make(map[*Client]bool),
	}
}

func (p *ClientPool) AddClient(client *Client) {
	p.clients[client] = true
	if len(p.clients) == 1 {
		clientPoolIsNotEmpty <- true
	}
}

func (p *ClientPool) RemoveClient(client *Client) {
	delete(p.clients, client)
}
