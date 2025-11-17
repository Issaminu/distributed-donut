# Distributed Donut

A Go-based distributed rendering system that creates an ASCII art animation of a rotating 3D donut by leveraging the collective computing power of connected web browser clients.

## What is Distributed Donut?

Distributed Donut demonstrates **browser-based distributed computing** by offloading CPU-intensive rendering calculations to multiple connected clients, then coordinating and broadcasting the results to create a seamless animation displayed on all connected browsers.

## Architecture Overview

### Server-Side Components (Go)

- **Main Server** (`main.go`): Handles WebSocket connections and HTTP routing
- **Frame Orchestrator** (`orchestator.go`): Central controller coordinating the rendering pipeline
- **Frame Buffer**: Circular buffer storing rendered frames (~30 minutes capacity)
- **Client Pool**: Tracks all connected clients for task distribution
- **Frame Batch Map**: Manages rendering tasks and handles failures

### Client-Side Components (JavaScript)

- **Client JavaScript**: Establishes WebSocket connection and displays animation
- **Web Worker** (`donut-worker.js`): Performs rendering calculations in separate thread

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
- Calculates 3D torus coordinates and projects to 2D
- Implements lighting based on surface normals
- Uses Z-buffer for depth sorting
- Maps to ASCII character set: `.,-~:;=!*#$@ \n`

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

1. **Prerequisites**: Go 1.x installed
2. **Run Server**: `go run *.go`
3. **Open Browser**: Navigate to `http://localhost:8080`
4. **Watch Magic**: See the distributed ASCII donut animation!

## Use Cases

This project serves as an excellent demonstration of:
- Distributed systems concepts
- WebSocket binary protocols
- Go concurrency patterns (goroutines, channels)
- Browser-based parallel computing
- Real-time synchronization techniques

---

*Distributed Donut showcases how Go's concurrency primitives can orchestrate a distributed system where web browsers collaborate to perform computational work, demonstrating efficient network protocols and creative use of browser capabilities for parallel computing.*