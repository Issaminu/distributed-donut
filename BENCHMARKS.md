# Benchmarks

The suite has two layers:

- **Micro-benchmarks** isolate the two CPU-bound hot paths: the wire codec
  (`internal/protocol`) and the ring-buffer copies (`internal/buffer`). They are
  fast, deterministic, and allocation-accounted.
- **End-to-end benchmarks** (`internal/harness`) drive the real server and
  orchestrator over a loopback websocket, with Go "workers" standing in for
  browsers. They measure the coordination machinery as a whole.

## Running them

```bash
# Every benchmark, with allocation stats:
go test -run='^$' -bench=. -benchmem ./...

# One package:
go test -run='^$' -bench=. -benchmem ./internal/buffer/

# One benchmark, longer run for steadier end-to-end numbers:
go test -run='^$' -bench=BenchmarkThroughput -benchtime=2s ./internal/harness/
```

`-run='^$'` matches no test names, so only benchmarks run — without it `go test`
would also run each package's unit and integration tests. The benchmark binaries
silence the server's logging (via `TestMain`), so output stays clean.

To compare two runs — before and after a change, to catch a regression — collect
several samples each and diff them with
[`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat):

```bash
go test -run='^$' -bench=. -count=10 ./internal/buffer/ > old.txt
# ...make the change...
go test -run='^$' -bench=. -count=10 ./internal/buffer/ > new.txt
benchstat old.txt new.txt
```

## What each benchmark measures

### Codec — `internal/protocol`

| Benchmark | Measures | On the server's hot path? |
| --- | --- | --- |
| `EncodeRenderTask` | Build a 13-byte task message | Yes — server → worker |
| `DecodeRenderResult` | Parse a returned frame batch (`NewRenderResult`) | Yes — worker → server |
| `EncodeFrameBroadcast` | Frame a broadcast payload | Yes — server → all workers |
| `EncodeRenderResult` | Encode a frame batch | No — worker half (the browser does this in production); included as the symmetric counterpart to decode |

`DecodeRenderTask` is deliberately omitted: it is three big-endian loads with no
allocation, and once inlined the compiler folds it away inside a loop, so any
microbenchmark of it reports a meaningless sub-nanosecond figure. (It runs in the
browser in production anyway.)

### Ring buffer — `internal/buffer`

| Benchmark | Measures |
| --- | --- |
| `AddFramesToBuffer` | Commit one rendered batch — the dispatcher's write |
| `GetFramesToBroadcast/seconds=N` | Snapshot an N-second window — the broadcaster's read — at the harness (1s), production (4s), and a catch-up (8s) window size |

### End-to-end — `internal/harness`

| Benchmark | Measures |
| --- | --- |
| `Throughput/workers=N` | Frames delivered per second through the whole pipeline, scaling workers (and dispatch width with them) 1 → 8 |
| `ThroughputUnderChurn/drop=P` | The same at 8 workers, with P% of results randomly dropped to force a task timeout and reassignment |

## How to read the numbers

This is the section that prevents misreadings — the end-to-end figures especially.

- **Throughput is a ceiling, not the playback rate.** These benchmarks turn the
  broadcast pacing right down (`BroadcastInterval = 1ms`) so the bottleneck is
  the machinery rather than the production sleeps. The server deliberately paces
  to ~60fps; the numbers say how much head-room the coordination layer has *above*
  that, not how fast frames are shown. (The server guarantees synchronized
  dispatch, not playback — see [`ARCHITECTURE.md` §1](ARCHITECTURE.md#1-context-and-goals).)
- **Synthetic frames are cheap and compressible.** Harness workers render with
  `SyntheticRenderer`, whose frames are quick to produce and compress far better
  than real donut output. That is intentional — it isolates the server's
  coordination and transport cost — but it means broadcast-compression cost is
  *under*-represented relative to production, where real frames compress ~4.8x
  rather than nearly losslessly.
- **The figures are relative, not absolute.** They depend on the machine, the Go
  toolchain, and loopback behaviour. Treat the snapshot below as a baseline to
  reproduce and compare against, not a spec.

## Snapshot

Apple M4 Max (arm64), Go 1.26.3, `-benchtime=2s`. Reproduce locally; absolute
values will differ. End-to-end and churn figures vary run to run — the trends
hold, the exact numbers do not.

**Codec**

| Benchmark | Time | Throughput | Allocations |
| --- | --- | --- | --- |
| `EncodeRenderTask` | 7.1 ns | — | 16 B, 1 alloc |
| `EncodeRenderResult` | 3.6 µs | 14.7 GB/s | 57 KB, 1 alloc |
| `DecodeRenderResult` | 3.8 µs | 13.8 GB/s | 57 KB, 1 alloc |
| `EncodeFrameBroadcast` | 3.6 µs | 14.6 GB/s | 57 KB, 1 alloc |

**Ring buffer**

| Benchmark | Time | Throughput | Allocations |
| --- | --- | --- | --- |
| `AddFramesToBuffer` | 591 ns | 89 GB/s | 0 |
| `GetFramesToBroadcast` (1s) | 1.25 µs | 42 GB/s | 1 (57 KB) |
| `GetFramesToBroadcast` (4s) | 4.3 µs | 49 GB/s | 1 (213 KB) |
| `GetFramesToBroadcast` (8s) | 8.0 µs | 53 GB/s | 1 (427 KB) |

**End-to-end throughput**

| Workers | Frames/s | Per frame |
| --- | --- | --- |
| 1 | ~56,000 | 17.9 µs |
| 2 | ~111,000 | 9.0 µs |
| 4 | ~214,000 | 4.7 µs |
| 8 | ~224,000 | 4.5 µs |

**End-to-end under churn** (8 workers)

| Results dropped | Frames/s |
| --- | --- |
| 0% | ~231,000 |
| 25% | ~12,000 |
| 50% | ~8,000 |

## What the numbers say

- **The codec and the buffer are not the bottleneck.** A batch copy runs at tens
  of GB/s, the codec at ~15 GB/s, each a few microseconds per 52 KB batch with at
  most one allocation; `AddFramesToBuffer` is allocation-free. At the server's
  real ~60fps (one batch per second per stream) these paths sit far below
  saturation — they are not where to look for performance work.
- **Coordination scales to ~4 workers, then the broadcaster saturates.**
  Throughput roughly doubles from 1 → 2 → 4 workers as the dispatch width grows
  with them, then flattens from 4 → 8. The ceiling is the single broadcaster
  goroutine, which serialises every broadcast (encode, compress, fan out).
  Parallelising or sharding that is the obvious next scaling lever.
- **Under churn, the task timeout dominates.** Dropping results collapses
  throughput far out of proportion to the drop rate — 25% dropped is roughly 20x
  slower, not 1.3x — because a dispatch round cannot finish until its slowest
  batch does, and a dropped batch costs a full `TaskTimeout` before it is
  reassigned. This is the empirical case for hedged dispatch
  ([`ARCHITECTURE.md` DR-4](ARCHITECTURE.md#dr-4-never-give-up-dispatch-with-executor-handoff)'s
  rejected alternative): the fixed timeout is the price of detecting a dead
  worker, and it sets the tail latency under churn.

## Methodology notes

A few choices that keep the measurements honest, recorded so they aren't
mistaken for bugs:

- **Defeating dead-code elimination.** The codec benchmarks assign their results
  to package-level sinks. Without that, the compiler proves the calls dead and
  removes them, and you see a nonsensical sub-nanosecond `ns/op`.
- **Silencing logs.** `TestMain` redirects the standard logger to `io.Discard`
  for the benchmark binaries. Otherwise the server's per-connection and
  per-reassignment logging serialises goroutines through the log mutex and skews
  the end-to-end results (and buries them in output).
- **`b.N` as a frame count.** The end-to-end benchmarks treat `b.N` as a target
  number of frames to deliver and let the framework scale it, so `ns/op` reads as
  time-per-frame and the reported `frames/s` follows directly.
- **Dispatch width tracks worker count.** Each end-to-end run sets the dispatch
  width equal to the worker count, so every worker can be busy at once. That is
  best-case parallelism by design — which is exactly why throughput keeps scaling
  until the broadcaster, not the workers, becomes the limit.
