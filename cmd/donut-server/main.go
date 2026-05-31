// Command donut-server runs the distributed-donut orchestrator: it serves the
// browser client, hands connecting workers to the client pool, and drives the
// render/broadcast pipeline.
package main

import (
	"context"
	"flag"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Issaminu/distributed-donut/internal/buffer"
	"github.com/Issaminu/distributed-donut/internal/client"
	"github.com/Issaminu/distributed-donut/internal/orchestrator"
	"github.com/Issaminu/distributed-donut/internal/server"
	"github.com/Issaminu/distributed-donut/web"
)

func main() {
	// Flags take precedence over environment variables, which take precedence
	// over the built-in defaults (sourced from orchestrator.DefaultConfig so the
	// CLI never drifts from the library defaults). See .env.example for the env vars.
	def := orchestrator.DefaultConfig()
	addr := flag.String("addr", defaultAddr(), "HTTP listen address (env: DONUT_ADDR, or PORT for port-only)")
	taskTimeout := flag.Duration("task-timeout", envDuration("DONUT_TASK_TIMEOUT", def.TaskTimeout), "how long a render batch may be outstanding before reassignment (env: DONUT_TASK_TIMEOUT)")
	broadcastInterval := flag.Duration("broadcast-interval", envDuration("DONUT_BROADCAST_INTERVAL", def.BroadcastInterval), "pacing/cooldown between broadcasts (env: DONUT_BROADCAST_INTERVAL)")
	firstBroadcastSeconds := flag.Int("first-broadcast-seconds", envInt("DONUT_FIRST_BROADCAST_SECONDS", def.FirstSecondsToBroadcast), "seconds of frames gathered before the first broadcast (env: DONUT_FIRST_BROADCAST_SECONDS)")
	broadcastSeconds := flag.Int("broadcast-seconds", envInt("DONUT_BROADCAST_SECONDS", def.SecondsToBroadcast), "seconds of frames each subsequent broadcast covers (env: DONUT_BROADCAST_SECONDS)")
	clientCountInterval := flag.Duration("client-count-interval", envDuration("DONUT_CLIENT_COUNT_INTERVAL", def.ClientCountInterval), "how often the connected-client count is broadcast to the fleet; 0 disables (env: DONUT_CLIENT_COUNT_INTERVAL)")
	bufferFullnessInterval := flag.Duration("buffer-fullness-interval", envDuration("DONUT_BUFFER_FULLNESS_INTERVAL", def.BufferFullnessInterval), "how often ring-buffer fullness is broadcast to the fleet; 0 disables (env: DONUT_BUFFER_FULLNESS_INTERVAL)")
	shutdownTimeout := flag.Duration("shutdown-timeout", envDuration("DONUT_SHUTDOWN_TIMEOUT", 10*time.Second), "max time to drain in-flight requests on shutdown (env: DONUT_SHUTDOWN_TIMEOUT)")
	logLevel := flag.String("log-level", envString("DONUT_LOG_LEVEL", "info"), "log verbosity: debug, info, warn, error (env: DONUT_LOG_LEVEL)")
	logSampleInterval := flag.Duration("log-sample-interval", envDuration("DONUT_LOG_SAMPLE_INTERVAL", 2*time.Second), "min spacing between repeats of any single debug log line, to curb hot-path spam (env: DONUT_LOG_SAMPLE_INTERVAL)")
	flag.Parse()

	slog.SetDefault(newLogger(parseLevel(*logLevel), *logSampleInterval))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT and SIGTERM signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		slog.Info("received termination signal, shutting down")
		cancel()
	}()

	// Wire the pipeline together: the buffer and client pool are shared state,
	// the orchestrator drives them, and the server feeds connecting clients in.
	frameBuffer := buffer.NewFrameBuffer()
	clientPool := client.NewClientPool()
	orch := orchestrator.New(frameBuffer, clientPool,
		orchestrator.WithTaskTimeout(*taskTimeout),
		orchestrator.WithBroadcastInterval(*broadcastInterval),
		orchestrator.WithBroadcastThresholds(*firstBroadcastSeconds, *broadcastSeconds),
		orchestrator.WithClientCountInterval(*clientCountInterval),
		orchestrator.WithBufferFullnessInterval(*bufferFullnessInterval),
	)

	staticFS, err := fs.Sub(web.Static, "static")
	if err != nil {
		slog.Error("failed to load embedded web client", "err", err)
		os.Exit(1)
	}
	srv := server.New(clientPool, orch.HandleResult, staticFS)

	orch.Run(ctx) // starts the broadcaster + task dispatcher
	go func() {   // serves the web client and accepts worker connections
		if err := srv.ListenAndServe(ctx, *addr, *shutdownTimeout); err != nil {
			slog.Error("HTTP server error", "err", err)
			cancel()
		}
	}()

	// Block until context is cancelled
	<-ctx.Done()
	slog.Info("program terminated")
}

// defaultAddr resolves the default listen address: an explicit DONUT_ADDR wins,
// otherwise PORT (the common PaaS convention) is used as ":$PORT", falling back
// to ":8080". The -addr flag still overrides whatever this returns.
func defaultAddr() string {
	if addr, ok := os.LookupEnv("DONUT_ADDR"); ok && addr != "" {
		return addr
	}
	if port, ok := os.LookupEnv("PORT"); ok && port != "" {
		return ":" + port
	}
	return ":8080"
}

// envDuration returns the duration in key (parsed via time.ParseDuration, e.g.
// "2s", "150ms") or def if the variable is unset or malformed.
func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		slog.Warn("invalid env value, using default", "key", key, "value", v, "default", def)
	}
	return def
}

// envInt returns the integer in key or def if the variable is unset or malformed.
func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		slog.Warn("invalid env value, using default", "key", key, "value", v, "default", def)
	}
	return def
}

// envString returns the value in key or def if the variable is unset or empty.
func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
