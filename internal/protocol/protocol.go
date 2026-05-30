// Package protocol defines the wire format shared by the orchestrator and its
// clients: the frame sizing constants, the message-type tags, and the
// encode/decode helpers for each message.
package protocol

import "encoding/binary"

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
