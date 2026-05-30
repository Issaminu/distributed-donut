# Distributed Donut

A Go-based distributed rendering system that creates an ASCII art animation of a rotating 3D donut by leveraging the collective computing power of connected web browser clients.

## What is Distributed Donut?

Distributed Donut demonstrates **browser-based distributed computing** by offloading CPU-intensive rendering calculations to multiple connected clients, then coordinating and broadcasting the results to create a seamless animation displayed on all connected browsers.

## Architecture Overview

### Server-Side Components (Go)

- **Entrypoint** (`cmd/donut-server`): Wires the pipeline together and handles startup/shutdown
- **HTTP/WebSocket Server** (`internal/server`): Handles WebSocket connections and serves the web client
- **Frame Orchestrator** (`internal/orchestrator`): Central controller coordinating the rendering pipeline; also owns the **Frame Batch Map** that manages rendering tasks and handles failures
- **Frame Buffer** (`internal/buffer`): Circular buffer storing rendered frames (~30 minutes capacity)
- **Client Pool** (`internal/client`): Tracks all connected clients for task distribution
- **Wire Protocol** (`internal/protocol`): Frame sizing constants, message types, and encode/decode helpers

### Client-Side Components (JavaScript)

- **Client JavaScript** (`web/static/script.js`): Establishes WebSocket connection and displays animation
- **Web Worker** (`web/static/donut-worker.js`): Performs rendering calculations in separate thread

The browser client in `web/static` is embedded into the binary via `go:embed`, so the server runs from anywhere without needing the source tree.

### Project Layout

```
cmd/donut-server/      # main package — the runnable binary
internal/
  protocol/            # wire format: constants, message types, encode/decode (no deps)
  buffer/              # FrameBuffer ring
  client/              # Client + ClientPool
  orchestrator/        # dispatch/broadcast loops + FrameBatchMap
  server/              # HTTP + WebSocket handlers
  debug/               # optional console renderer (build with -tags debug)
web/                   # embedded static browser client
```

## How It Works

1. **Client Connection**: Browsers connect via WebSocket
2. **Task Dispatch**: Server assigns frame ranges (e.g., frames 0-59) to clients
3. **Distributed Rendering**: Clients render ASCII frames using 3D mathematics
4. **Result Collection**: Clients encode and send completed frames back to server
5. **Frame Storage**: Server stores frames in circular buffer
6. **Broadcasting**: Server broadcasts frames to ALL clients for synchronized playback
7. **Animation Display**: Clients decode frames and display the rotating donut

## Technical Features

### Communication Protocol
Uses binary WebSocket messages with three message types:
- **RenderTask** (0x0): Server → Client task assignment
- **RenderResult** (0x1): Client → Server frame submission  
- **FrameBroadcast** (0x2): Server → All Clients frame distribution

### ASCII Donut Algorithm
Uses a1k0n's incredible [donut.c](https://www.a1k0n.net/2011/07/20/donut-math.html).

### Frame Encoding/Decoding
- Efficient compression: Each character mapped to 4-bit index
- Two characters packed per byte (50% compression)
- Example: 'A' (index 5) + 'B' (index 7) → byte `0x57`

### Configuration
- **FramesPerBatch**: 60 frames (1 second at 60fps)
- **Buffer Capacity**: 108,000 frames (~30 minutes)
- **Task Timeout**: 2 seconds with automatic reassignment
- **Playback Rate**: 60fps for smooth animation

## Key Benefits

✅ **Distributed Computing**: Harnesses multiple browser clients for parallel processing  
✅ **Real-time Coordination**: Synchronizes rendering across all connected clients  
✅ **Fault Tolerance**: Automatic task reassignment on client failures  
✅ **Efficient Protocol**: Binary WebSocket with 50% compression  
✅ **Non-blocking**: Web Workers prevent UI freezing  
✅ **Scalable**: Dynamic client management with graceful handling  

## Getting Started

1. **Prerequisites**: Go 1.22+ installed
2. **Run Server**: `go run ./cmd/donut-server` (or `go build ./cmd/donut-server` then `./donut-server`)
3. **Open Browser**: Navigate to `http://localhost:8080`
4. **Watch Magic**: See the distributed ASCII donut animation!
