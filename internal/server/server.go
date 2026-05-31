// Package server exposes the HTTP surface: it serves the static web client and
// upgrades /ws requests into websocket clients, handing each one to the client
// pool and wiring its results back to the orchestrator.
package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
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
		slog.Info("shutting down HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown error", "err", err)
		}
	}()

	slog.Info("server started", "addr", addr)
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
		slog.Warn("websocket upgrade failed", "err", err)
		return
	}
	c := client.NewClient(conn, s.onResult)
	if !s.pool.AddClient(c) {
		// At capacity: tell the client to retry later and drop the connection
		// before spawning its writer, so a flood can't exhaust our goroutines.
		slog.Warn("rejecting connection: client pool at capacity", "client", c.ID())
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "server at capacity"),
			time.Now().Add(time.Second),
		)
		conn.Close()
		return
	}
	slog.Debug("client connected", "client", c.ID())
	defer c.Close()
	go c.WritePump()

	for {
		select {
		case <-ctx.Done():
			slog.Debug("connection handler shutting down", "client", c.ID())
			return
		default:
			_, incomingMessage, err := conn.ReadMessage()
			if err != nil {
				// A browser tab closing is the normal case, not an error worth
				// shouting about; only unexpected closes get a warning.
				if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
					slog.Warn("client read error", "client", c.ID(), "err", err)
				} else {
					slog.Debug("client disconnected", "client", c.ID(), "err", err)
				}
				return
			}
			if err := c.HandleReceivedMessage(incomingMessage); err != nil {
				slog.Warn("dropping connection after invalid message", "client", c.ID(), "err", err)
				return
			}
		}
	}
}
