package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

var clientPool = NewClientPool()

func websocketHandler(ctx context.Context) {
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleNewConnection(ctx, w, r)
	})
	http.Handle("/", http.FileServer(http.Dir("./static")))
	server := &http.Server{Addr: ":8080"}

	go func() {
		<-ctx.Done()
		log.Println("Shutting down WebSocket server...")
		if err := server.Shutdown(context.Background()); err != nil {
			log.Println("WebSocket server shutdown error:", err)
		}
	}()

	log.Println("Server started on :8080")
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Println("HTTP server error:", err)
	}
}

func handleNewConnection(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer func(conn *websocket.Conn) {
		err := conn.Close()
		if err != nil {
			log.Println(err)
		}
	}(conn)
	log.Println("Client connected")
	client := NewClient(conn)
	clientPool.AddClient(client)

	for {
		select {
		case <-ctx.Done():
			log.Println("Connection handler shutting down...")
			clientPool.RemoveClient(client)
			return
		default:
			_, incomingMessage, err := conn.ReadMessage()
			if err != nil {
				log.Println(err)
				clientPool.RemoveClient(client)
				return
			}
			err = client.HandleReceivedMessage(incomingMessage)
			if err != nil {
				log.Println(err)
				return
			}

		}
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Handle SIGINT and SIGTERM signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received termination signal, shutting down...")
		cancel()
	}()
	go websocketHandler(ctx)  // Start the websocket server and handles connections and receiving messages.
	go frameOrchestrator(ctx) // Starts the orchestrator, which sends work to the clients, receives the work (frames) from the clients, then dispatches the frames to everyone.

	// Block until context is cancelled
	<-ctx.Done()
	log.Println("Program terminated")
}
