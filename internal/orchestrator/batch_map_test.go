package orchestrator

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Issaminu/distributed-donut/internal/buffer"
	"github.com/Issaminu/distributed-donut/internal/client"
	"github.com/Issaminu/distributed-donut/internal/protocol"
)

func TestAddFrameBatchAndGetLength(t *testing.T) {
	m := NewFrameBatchMap(buffer.NewFrameBuffer())
	const clientID = uint32(1)

	if got := m.GetLength(clientID); got != 0 {
		t.Fatalf("new map length = %d, want 0", got)
	}

	m.AddFrameBatch(clientID, NewFrameBatchMetadata(0, 0, protocol.FramesPerBatch-1))
	m.AddFrameBatch(clientID, NewFrameBatchMetadata(1, protocol.FramesPerBatch, 2*protocol.FramesPerBatch-1))
	if got := m.GetLength(clientID); got != 2 {
		t.Fatalf("length = %d, want 2", got)
	}
}

func TestDeleteRenderTaskCleansUpClientEntry(t *testing.T) {
	m := NewFrameBatchMap(buffer.NewFrameBuffer())
	const clientID = uint32(5)

	m.AddFrameBatch(clientID, NewFrameBatchMetadata(0, 0, protocol.FramesPerBatch-1))
	m.DeleteRenderTask(clientID, 0)

	if got := m.GetLength(clientID); got != 0 {
		t.Fatalf("length after delete = %d, want 0", got)
	}
	if _, ok := m.frameBatches[clientID]; ok {
		t.Error("client entry was not cleaned up after its last task was deleted")
	}
}

func TestSaveRenderResultMarksCompletedAndClosesDone(t *testing.T) {
	m := NewFrameBatchMap(buffer.NewFrameBuffer())
	const clientID = uint32(3)

	meta := NewFrameBatchMetadata(0, 0, protocol.FramesPerBatch-1)
	done := meta.done
	m.AddFrameBatch(clientID, meta)

	if err := m.SaveRenderResult(clientID, &protocol.RenderResult{ID: 0}); err != nil {
		t.Fatalf("SaveRenderResult: %v", err)
	}

	select {
	case <-done:
	default:
		t.Fatal("done channel was not closed after the result was saved")
	}
	if !m.frameBatches[clientID][0].completed {
		t.Error("batch was not marked completed")
	}
}

func TestSaveRenderResultForUnknownTaskIsNoop(t *testing.T) {
	m := NewFrameBatchMap(buffer.NewFrameBuffer())
	// Nothing registered for client 1, so this should quietly do nothing.
	if err := m.SaveRenderResult(1, &protocol.RenderResult{ID: 42}); err != nil {
		t.Fatalf("expected nil for unknown task, got %v", err)
	}
}

func TestSaveRenderResultDuplicateIsIgnored(t *testing.T) {
	m := NewFrameBatchMap(buffer.NewFrameBuffer())
	const clientID = uint32(2)

	m.AddFrameBatch(clientID, NewFrameBatchMetadata(0, 0, protocol.FramesPerBatch-1))
	res := &protocol.RenderResult{ID: 0}

	if err := m.SaveRenderResult(clientID, res); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// A duplicate result for an already-completed task must be ignored without
	// double-closing the done channel (which would panic).
	if err := m.SaveRenderResult(clientID, res); err != nil {
		t.Fatalf("duplicate save returned error: %v", err)
	}
}

func TestSwitchRenderTaskExecutorMovesTaskToNewClient(t *testing.T) {
	m := NewFrameBatchMap(buffer.NewFrameBuffer())

	c1, cleanup1 := newConnectedClient(t)
	defer cleanup1()
	c2, cleanup2 := newConnectedClient(t)
	defer cleanup2()

	taskID := c1.GenerateNewRenderTaskID()
	m.AddFrameBatch(c1.ID(), NewFrameBatchMetadata(taskID, 0, protocol.FramesPerBatch-1))

	newTaskID := m.SwitchRenderTaskExecutor(taskID, c1.ID(), c2)

	if got := m.GetLength(c1.ID()); got != 0 {
		t.Errorf("original client length after switch = %d, want 0", got)
	}
	if got := m.GetLength(c2.ID()); got != 1 {
		t.Errorf("new client length after switch = %d, want 1", got)
	}
	if _, ok := m.frameBatches[c2.ID()][newTaskID]; !ok {
		t.Errorf("task not found under the new client with task ID %d", newTaskID)
	}
}

// newConnectedClient stands up a throwaway websocket endpoint and returns a
// live, server-side *client.Client plus a cleanup func.
func newConnectedClient(t *testing.T) (*client.Client, func()) {
	t.Helper()

	ch := make(chan *client.Client, 1)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		ch <- client.NewClient(conn, nil)
	}))

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	peer, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}

	var c *client.Client
	select {
	case c = <-ch:
	case <-time.After(2 * time.Second):
		peer.Close()
		srv.Close()
		t.Fatal("timed out waiting for the server to accept the connection")
	}

	return c, func() {
		peer.Close()
		srv.Close()
	}
}
