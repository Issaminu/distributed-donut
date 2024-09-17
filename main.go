package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

var pool = NewPool()

var messageChan = make(chan *Message)

func websocketHandler() {
	http.HandleFunc("/connect", handleNewConnections)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})
	fmt.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		return
	}
}

func handleNewConnections(w http.ResponseWriter, r *http.Request) {
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
	pool.AddClient(client)
	for {
		_, incomingMessage, err := conn.ReadMessage()
		if err != nil {
			fmt.Println(err)
			pool.RemoveClient(client)
			return
		}
		// print the length of the incoming message
		fmt.Println("Incoming message length: ", len(incomingMessage))
		var messageBuffer [MessageSize]byte
		copy(messageBuffer[:], incomingMessage)
		fmt.Println("Copied message length: ", len(messageBuffer))
		message := NewReceivedMessage(messageBuffer)
		messageChan <- message
	}
}

func main() {
	go websocketHandler()  // Start the websocket server and handles connections.
	go messageReceiver()   // Handles receiving messages from the clients.
	go frameOrchestrator() // Starts the orchestrator, which sends work to the clients, receives the work (frames) from the clients, then dispatches the frames to everyone.
	select {}
}
