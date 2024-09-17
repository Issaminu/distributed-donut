package main

var poolIsNotEmpty = make(chan bool)

type Pool struct {
	clients map[*Client]bool
}

func NewPool() *Pool {
	return &Pool{
		clients: make(map[*Client]bool),
	}
}

func (p *Pool) AddClient(client *Client) {
	p.clients[client] = true
	if len(p.clients) == 1 {
		poolIsNotEmpty <- true
	}
}

func (p *Pool) RemoveClient(client *Client) {
	delete(p.clients, client)
}
