package protocol_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/Issaminu/distributed-donut/internal/protocol"
)

func TestEncodeRenderTask(t *testing.T) {
	const (
		taskID = uint32(0x01020304)
		start  = uint32(60)
		end    = uint32(119)
	)
	msg := protocol.EncodeRenderTask(taskID, start, end)

	if len(msg) != 13 {
		t.Fatalf("len = %d, want 13", len(msg))
	}
	if msg[0] != protocol.MessageTypeRenderTask {
		t.Errorf("message type = %#x, want %#x", msg[0], protocol.MessageTypeRenderTask)
	}
	if got := binary.BigEndian.Uint32(msg[1:5]); got != taskID {
		t.Errorf("taskID = %d, want %d", got, taskID)
	}
	if got := binary.BigEndian.Uint32(msg[5:9]); got != start {
		t.Errorf("startFrame = %d, want %d", got, start)
	}
	if got := binary.BigEndian.Uint32(msg[9:13]); got != end {
		t.Errorf("endFrame = %d, want %d", got, end)
	}
}

func TestEncodeFrameBroadcast(t *testing.T) {
	frames := []byte{0xde, 0xad, 0xbe, 0xef}
	msg := protocol.EncodeFrameBroadcast(frames)

	if len(msg) != len(frames)+1 {
		t.Fatalf("len = %d, want %d", len(msg), len(frames)+1)
	}
	if msg[0] != protocol.MessageTypeFrameBroadcast {
		t.Errorf("message type = %#x, want %#x", msg[0], protocol.MessageTypeFrameBroadcast)
	}
	if !bytes.Equal(msg[1:], frames) {
		t.Errorf("payload = %v, want %v", msg[1:], frames)
	}
}

// EncodeFrameBroadcast must copy its input, not alias it: the same frames slice
// is reused across clients and must not be mutable through the encoded message.
func TestEncodeFrameBroadcastDoesNotAliasInput(t *testing.T) {
	frames := []byte{1, 2, 3}
	msg := protocol.EncodeFrameBroadcast(frames)
	msg[1] = 99
	if frames[0] != 1 {
		t.Errorf("input slice was mutated: frames[0] = %d, want 1", frames[0])
	}
}

func TestNewRenderTask(t *testing.T) {
	rt := protocol.NewRenderTask(7, 60, 119)
	if rt.ID != 7 || rt.StartFrame != 60 || rt.EndFrame != 119 {
		t.Errorf("NewRenderTask = %+v, want {ID:7 StartFrame:60 EndFrame:119}", rt)
	}
}

func TestNewRenderResult(t *testing.T) {
	const taskID = uint32(0xCAFEBABE)
	body := make([]byte, 4+protocol.BatchSize)
	binary.BigEndian.PutUint32(body[0:4], taskID)
	for i := range body[4:] {
		body[4+i] = byte(i % 256)
	}

	res, err := protocol.NewRenderResult(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ID != taskID {
		t.Errorf("ID = %#x, want %#x", res.ID, taskID)
	}
	if res.Frames[0] != 0 || res.Frames[1] != 1 {
		t.Errorf("frames not copied correctly: got [%d %d ...]", res.Frames[0], res.Frames[1])
	}
	if want := byte((protocol.BatchSize - 1) % 256); res.Frames[protocol.BatchSize-1] != want {
		t.Errorf("last frame byte = %d, want %d", res.Frames[protocol.BatchSize-1], want)
	}
}

func TestNewRenderResultRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"too short for task ID", []byte{0x00, 0x01}},
		{"no frames", make([]byte, 4)},
		{"frames one byte short", make([]byte, 4+protocol.BatchSize-1)},
		{"frames one byte long", make([]byte, 4+protocol.BatchSize+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := protocol.NewRenderResult(tt.data); err == nil {
				t.Errorf("expected error for %q, got nil", tt.name)
			}
		})
	}
}

// A render task encoded by the server should decode back to the same fields,
// mirroring what the browser client does on receipt.
func TestRenderTaskRoundTrip(t *testing.T) {
	const (
		taskID = uint32(12345)
		start  = uint32(180)
		end    = uint32(239)
	)
	msg := protocol.EncodeRenderTask(taskID, start, end)
	if msg[0] != protocol.MessageTypeRenderTask {
		t.Fatalf("message type = %#x", msg[0])
	}
	gotID, gotStart, gotEnd, err := protocol.DecodeRenderTask(msg[1:])
	if err != nil {
		t.Fatalf("DecodeRenderTask: %v", err)
	}
	if gotID != taskID || gotStart != start || gotEnd != end {
		t.Errorf("round trip = (%d, %d, %d), want (%d, %d, %d)", gotID, gotStart, gotEnd, taskID, start, end)
	}
}

func TestDecodeRenderTaskRejectsShortInput(t *testing.T) {
	if _, _, _, err := protocol.DecodeRenderTask([]byte{0, 1, 2, 3}); err == nil {
		t.Fatal("expected error for short render task body")
	}
}

// A result encoded by a worker should be parseable by the server via
// NewRenderResult, recovering the same task ID and frame bytes.
func TestRenderResultRoundTrip(t *testing.T) {
	const taskID = uint32(7777)
	frames := make([]byte, protocol.BatchSize)
	for i := range frames {
		frames[i] = byte(i % 256)
	}

	msg := protocol.EncodeRenderResult(taskID, frames)
	if msg[0] != protocol.MessageTypeRenderResult {
		t.Fatalf("message type = %#x", msg[0])
	}

	res, err := protocol.NewRenderResult(msg[1:])
	if err != nil {
		t.Fatalf("NewRenderResult: %v", err)
	}
	if res.ID != taskID {
		t.Errorf("ID = %d, want %d", res.ID, taskID)
	}
	if !bytes.Equal(res.Frames[:], frames) {
		t.Error("frame bytes did not survive the round trip")
	}
}
