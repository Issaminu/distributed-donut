package protocol_test

import (
	"testing"

	"github.com/Issaminu/distributed-donut/internal/protocol"
)

// Package-level sinks. Without consuming the results, the compiler proves these
// calls dead and eliminates them (you'd see sub-nanosecond ns/op). Assigning to
// an escaping sink keeps the work honest.
var (
	sinkBytes  []byte
	sinkResult *protocol.RenderResult
)

// The codec runs on every message in and out of the server. The two that carry
// a whole frame batch (encode-result, decode-result) move real bytes, so they
// set b.SetBytes to report throughput in MB/s; the task message just reports
// ns/op.
//
// Note on what runs where: the server's hot path is EncodeRenderTask (out),
// NewRenderResult (in), and EncodeFrameBroadcast (out). EncodeRenderResult is
// the worker half — exercised by the Go harness, but done in the browser in
// production; it's included here as the symmetric counterpart to decode.
// DecodeRenderTask (the other worker half) is deliberately omitted: it is three
// big-endian loads with no allocation, and once inlined the compiler folds it
// away in a loop, so any microbenchmark of it reports a meaningless ~0.5ns.

func BenchmarkEncodeRenderTask(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		sinkBytes = protocol.EncodeRenderTask(1, 60, 119)
	}
}

func BenchmarkEncodeRenderResult(b *testing.B) {
	frames := make([]byte, protocol.BatchSize)
	b.SetBytes(protocol.BatchSize)
	b.ReportAllocs()
	for range b.N {
		sinkBytes = protocol.EncodeRenderResult(1, frames)
	}
}

func BenchmarkDecodeRenderResult(b *testing.B) {
	// The body is what the server parses after the message-type tag: a 4-byte
	// task ID followed by exactly one batch of frame bytes.
	body := make([]byte, 4+protocol.BatchSize)
	b.SetBytes(protocol.BatchSize)
	b.ReportAllocs()
	for range b.N {
		res, err := protocol.NewRenderResult(body)
		if err != nil {
			b.Fatal(err)
		}
		sinkResult = res
	}
}

func BenchmarkEncodeFrameBroadcast(b *testing.B) {
	frames := make([]byte, protocol.BatchSize)
	b.SetBytes(protocol.BatchSize)
	b.ReportAllocs()
	for range b.N {
		sinkBytes = protocol.EncodeFrameBroadcast(frames)
	}
}
