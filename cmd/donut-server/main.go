// Command donut-server runs the distributed-donut orchestrator: it serves the
// browser client, hands connecting workers to the client pool, and drives the
// render/broadcast pipeline.
package main

import (
	"context"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Issaminu/distributed-donut/internal/buffer"
	"github.com/Issaminu/distributed-donut/internal/client"
	"github.com/Issaminu/distributed-donut/internal/orchestrator"
	"github.com/Issaminu/distributed-donut/internal/server"
	"github.com/Issaminu/distributed-donut/web"
)

const listenAddr = ":8080"

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

	// Wire the pipeline together: the buffer and client pool are shared state,
	// the orchestrator drives them, and the server feeds connecting clients in.
	frameBuffer := buffer.NewFrameBuffer()
	clientPool := client.NewClientPool()
	orch := orchestrator.New(frameBuffer, clientPool)

	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		log.Fatalln("Failed to load embedded web client:", err)
	}
	srv := server.New(clientPool, orch.HandleResult, staticFS)

	orch.Run(ctx) // starts the broadcaster + task dispatcher
	go func() {   // serves the web client and accepts worker connections
		if err := srv.ListenAndServe(ctx, listenAddr); err != nil {
			log.Println("HTTP server error:", err)
			cancel()
		}
	}()

	// Block until context is cancelled
	<-ctx.Done()
	log.Println("Program terminated")
}
