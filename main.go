package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

var clientPool = NewClientPool()

func websocketHandler() {
	http.HandleFunc("/connect", handleNewConnection)
	http.Handle("/", http.FileServer(http.Dir("./static")))
	fmt.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		return
	}
}

func handleNewConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func(conn *websocket.Conn) {
		err := conn.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(conn)
	fmt.Println("Client connected")
	client := NewClient(conn)
	clientPool.AddClient(client)
	for {
		_, incomingMessage, err := conn.ReadMessage()
		if err != nil {
			fmt.Println(err)
			clientPool.RemoveClient(client)
			return
		}
		client.handleReceivedMessage(incomingMessage)
	}
}

func main() {
	go websocketHandler()  // Start the websocket server and handles connections and receiving messages.
	go frameOrchestrator() // Starts the orchestrator, which sends work to the clients, receives the work (frames) from the clients, then dispatches the frames to everyone.
	select {}
}
