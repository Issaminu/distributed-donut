// Package protocol defines the wire format shared by the orchestrator and its
// clients: the frame sizing constants, the message-type tags, and the
// encode/decode helpers for each message.
package protocol

import (
	"encoding/binary"
	"errors"
)

// === Calculation for the frame size ===
// The frame is represented as a 2D grid of characters.
// The grid dimensions are 80 characters wide and 22 characters tall.
// Each row ends with a newline character.

// We can calculate the length as follows:
// 80 characters per row
// 22 rows
// Total length = 80 * 22 = 1760 characters
// Possible values for each character are ".,-~:;=!*#$@ \n".
// These are 14 possible values. 14 in binary is 1110. So, we can represent each character as a 4-bit binary number.
// This means that the total length of the frame is 1760 * 4 = 7040 bits = 880 bytes.
const (
	FrameSize      = 880                        // bytes per frame
	FramesPerBatch = 60                         // frames per batch (1 second at 60fps)
	BatchSize      = FrameSize * FramesPerBatch // bytes per batch of frames
)

// Message types. Each message on the wire begins with one of these tag bytes.
const (
	MessageTypeRenderTask     = 0x0 // 0000 - requesting work (compute) from workers/clients
	MessageTypeRenderResult   = 0x1 // 0001 - delivering a rendered frame batch from a worker/client to the orchestrator
	MessageTypeFrameBroadcast = 0x2 // 0010 - broadcasting frame batch(s) to all workers/clients
	MessageTypeClientCount    = 0x3 // 0011 - telemetry: live count of connected clients, broadcast to everyone
	MessageTypeBufferFullness = 0x4 // 0100 - telemetry: server ring-buffer fullness as a percentage, broadcast to everyone
)

// EncodeRenderTask builds a RenderTask message: 1 byte message type + 4 bytes
// render task ID + 4 bytes start frame + 4 bytes end frame.
func EncodeRenderTask(renderTaskID, startFrame, endFrame uint32) []byte {
	msg := make([]byte, 13)
	msg[0] = MessageTypeRenderTask
	binary.BigEndian.PutUint32(msg[1:5], renderTaskID)
	binary.BigEndian.PutUint32(msg[5:9], startFrame)
	binary.BigEndian.PutUint32(msg[9:13], endFrame)
	return msg
}

// EncodeFrameBroadcast prepends the broadcast message-type byte to a slice of
// already-encoded frames.
func EncodeFrameBroadcast(frames []byte) []byte {
	msg := make([]byte, len(frames)+1)
	msg[0] = MessageTypeFrameBroadcast
	copy(msg[1:], frames)
	return msg
}

// EncodeRenderResult builds a RenderResult message: 1 byte message type +
// 4 bytes render task ID + the rendered frame bytes. It is the symmetric
// counterpart to NewRenderResult, used by a worker returning completed work.
func EncodeRenderResult(renderTaskID uint32, frames []byte) []byte {
	msg := make([]byte, 5+len(frames))
	msg[0] = MessageTypeRenderResult
	binary.BigEndian.PutUint32(msg[1:5], renderTaskID)
	copy(msg[5:], frames)
	return msg
}

// EncodeClientCount builds a ClientCount telemetry message: 1 byte message type
// + 4 bytes client count. It is broadcast to every client so each viewer can
// show how many browsers are currently in the fleet.
func EncodeClientCount(count uint32) []byte {
	msg := make([]byte, 5)
	msg[0] = MessageTypeClientCount
	binary.BigEndian.PutUint32(msg[1:5], count)
	return msg
}

// DecodeClientCount parses the body of a ClientCount message (the 4 bytes after
// the message-type tag). It is the symmetric counterpart to EncodeClientCount.
func DecodeClientCount(data []byte) (count uint32, err error) {
	if len(data) < 4 {
		return 0, errors.New("client count message too short")
	}
	return binary.BigEndian.Uint32(data[0:4]), nil
}

// EncodeBufferFullness builds a BufferFullness telemetry message: 1 byte message
// type + 1 byte percentage (0-100). It is broadcast to every client so each
// viewer can show how full the server's ring buffer is.
func EncodeBufferFullness(percent uint8) []byte {
	return []byte{MessageTypeBufferFullness, percent}
}

// DecodeBufferFullness parses the body of a BufferFullness message (the 1 byte
// after the message-type tag). It is the symmetric counterpart to
// EncodeBufferFullness.
func DecodeBufferFullness(data []byte) (percent uint8, err error) {
	if len(data) < 1 {
		return 0, errors.New("buffer fullness message too short")
	}
	return data[0], nil
}

// DecodeRenderTask parses the body of a RenderTask message (the 12 bytes after
// the message-type tag): render task ID, start frame, end frame. It is the
// symmetric counterpart to EncodeRenderTask, used by a worker receiving work.
func DecodeRenderTask(data []byte) (id uint32, startFrame uint32, endFrame uint32, err error) {
	if len(data) < 12 {
		return 0, 0, 0, errors.New("render task message too short")
	}
	id = binary.BigEndian.Uint32(data[0:4])
	startFrame = binary.BigEndian.Uint32(data[4:8])
	endFrame = binary.BigEndian.Uint32(data[8:12])
	return id, startFrame, endFrame, nil
}
