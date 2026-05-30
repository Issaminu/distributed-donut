# Distributed Donut

A Go-based distributed rendering system that creates an ASCII art animation of a rotating 3D donut by leveraging the collective computing power of connected web browser clients.

## What is Distributed Donut?

Distributed Donut demonstrates **browser-based distributed computing** by offloading CPU-intensive rendering calculations to multiple connected clients, then coordinating and broadcasting the results to create a seamless animation displayed on all connected browsers.
## Going deeper

[`ARCHITECTURE.md`](ARCHITECTURE.md) is the technical deep dive: the end-to-end
data flow, the binary wire protocol, the ring buffer's invariants, the
concurrency model, how the pipeline survives a churning fleet, and the design
decisions behind each of those — with the alternatives that were weighed and the
trade-offs accepted.

[`BENCHMARKS.md`](BENCHMARKS.md) covers the benchmark suite: the micro-benchmarks
on the CPU-bound hot paths, the end-to-end benchmarks that drive the real server,
and how to run and compare them.


## Quick start

Requires Go 1.22+.

```bash
go run ./cmd/donut-server
```

Open <http://localhost:8080>, then open it in a few more tabs — each one pitches
in as a worker.

## Commands

| Command | Description |
| --- | --- |
| `go run ./cmd/donut-server` | Run the server on `:8080` |
| `go build ./cmd/donut-server` | Build the `donut-server` binary |
| `go test ./...` | Run the tests |

The browser client in `web/static` is embedded into the binary with `go:embed`,
so the build runs from anywhere with no extra files alongside it.

## How it works

1. A browser connects over WebSocket and spawns a Web Worker.
2. The server assigns it a contiguous range of frames to render.
3. The worker runs the donut math off the UI thread, packs the frames, and sends
   them back.
4. The server stores completed frames in a ring buffer and broadcasts them to
   every connected browser.
5. Each browser plays the stream back at 60fps.

If a worker goes quiet, its batch is reassigned to another. Frames are a pure
function of their frame number, so the work is safe to retry and hand off.

## Layout

```
cmd/donut-server/   the runnable binary
internal/
  protocol/         wire format: constants, message tags, encode/decode
  buffer/           the ring buffer of rendered frames
  client/           one worker connection, and the pool of all of them
  orchestrator/     the dispatch and broadcast loops
  server/           HTTP and WebSocket handlers
web/                embedded browser client (HTML/CSS/JS)
```

## Credits

The donut is [a1k0n's `donut.c`](https://www.a1k0n.net/2011/07/20/donut-math.html),
ported to JavaScript.

## License

[MIT](LICENSE) © Issam Boubcher
