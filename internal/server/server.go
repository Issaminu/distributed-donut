// Package server exposes the HTTP surface: it serves the static web client and
// upgrades /ws requests into websocket clients, handing each one to the client
// pool and wiring its results back to the orchestrator.
package server

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Issaminu/distributed-donut/internal/client"
)

type Server struct {
	pool     *client.ClientPool
	onResult client.ResultHandler
	upgrader websocket.Upgrader
	assets   fs.FS
}

// New builds a Server. assets is the filesystem served at "/"
// onResult is invoked for every render result a client returns.
func New(pool *client.ClientPool, onResult client.ResultHandler, assets fs.FS) *Server {

	// Note on compression:
	// Measured on the donut frames:
	// BestSpeed ~4.0x, DefaultCompression ~4.8x, BestCompression ~5.0x.
	// So level 6 keeps almost all the ratio for far less CPU.
	// (~2x is already banked by our custom nibble packing, so end-to-end vs raw ASCII this is ~9-10x.)
	// TODO: look into implementing our own compression layer on both ends
	upgrader := websocket.Upgrader{EnableCompression: true}

	return &Server{
		pool:     pool,
		onResult: onResult,
		upgrader: upgrader,
		assets:   assets,
	}
}

// ListenAndServe starts the HTTP server on addr and blocks until it stops. It
// shuts down gracefully when ctx is cancelled, waiting up to shutdownTimeout for
// in-flight requests to drain before giving up.
func (s *Server) ListenAndServe(ctx context.Context, addr string, shutdownTimeout time.Duration) error {
	server := &http.Server{Addr: addr, Handler: s.Handler(ctx)}

	go func() {
		<-ctx.Done()
		log.Println("Shutting down WebSocket server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Println("WebSocket server shutdown error:", err)
		}
	}()

	log.Println("Server started on", addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Handler(ctx context.Context) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		s.handleNewConnection(ctx, w, r)
	})
	mux.Handle("/", http.FileServer(http.FS(s.assets)))
	return mux
}

func (s *Server) handleNewConnection(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("Client connected")
	c := client.NewClient(conn, s.onResult)
	s.pool.AddClient(c)
	defer c.Close()
	go c.WritePump()

	for {
		select {
		case <-ctx.Done():
			log.Println("Connection handler shutting down...")
			return
		default:
			_, incomingMessage, err := conn.ReadMessage()
			if err != nil {
				log.Println(err)
				return
			}
			if err := c.HandleReceivedMessage(incomingMessage); err != nil {
				log.Println(err)
				return
			}
		}
	}
}
