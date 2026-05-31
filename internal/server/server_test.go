package server

import (
	"context"
	"encoding/binary"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Issaminu/distributed-donut/internal/client"
	"github.com/Issaminu/distributed-donut/internal/protocol"
)

func newTestServer(t *testing.T, onResult client.ResultHandler, assets fs.FS) (*httptest.Server, *client.ClientPool) {
	t.Helper()
	pool := client.NewClientPool(0)
	s := New(pool, onResult, assets)
	ts := httptest.NewServer(s.Handler(context.Background()))
	t.Cleanup(ts.Close)
	return ts, pool
}

func TestServesStaticAssets(t *testing.T) {
	assets := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<h1>donut</h1>")},
		"script.js":  &fstest.MapFile{Data: []byte("console.log('hi')")},
	}
	ts, _ := newTestServer(t, nil, assets)

	for path, want := range map[string]string{
		"/":          "<h1>donut</h1>",
		"/script.js": "console.log('hi')",
	} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, resp.StatusCode)
		}
		if string(body) != want {
			t.Errorf("GET %s body = %q, want %q", path, body, want)
		}
	}
}

// A connection that arrives when the pool is at capacity must be rejected with
// a "try again later" close rather than admitted, so a flood can't pile up
// connections without bound.
func TestRejectsConnectionsBeyondMaxClients(t *testing.T) {
	pool := client.NewClientPool(1)
	s := New(pool, nil, fstest.MapFS{})
	ts := httptest.NewServer(s.Handler(context.Background()))
	t.Cleanup(ts.Close)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	// First connection fills the single slot and stays open.
	first, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer first.Close()

	// Second connection upgrades but must be closed immediately with 1013.
	second, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer second.Close()

	if err := second.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, _, err = second.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseTryAgainLater) {
		t.Fatalf("second connection error = %v, want close 1013 (try again later)", err)
	}
	if got := pool.GetClientCount(); got != 1 {
		t.Errorf("pool count = %d, want 1 (rejected connection must not be admitted)", got)
	}
}

// End-to-end: a websocket client that sends a RenderResult should drive the
// injected result handler and be registered in the pool.
func TestWebSocketDeliversRenderResultToHandler(t *testing.T) {
	type received struct {
		clientID uint32
		taskID   uint32
	}
	got := make(chan received, 1)
	onResult := func(clientID uint32, r *protocol.RenderResult) error {
		got <- received{clientID: clientID, taskID: r.ID}
		return nil
	}
	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}}
	ts, pool := newTestServer(t, onResult, assets)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	const taskID = uint32(123)
	msg := make([]byte, 1+4+protocol.BatchSize)
	msg[0] = protocol.MessageTypeRenderResult
	binary.BigEndian.PutUint32(msg[1:5], taskID)
	if err := conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case r := <-got:
		if r.taskID != taskID {
			t.Errorf("handler taskID = %d, want %d", r.taskID, taskID)
		}
		if r.clientID == 0 {
			t.Error("handler clientID = 0, want a real assigned ID")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("result handler was not called after sending a RenderResult")
	}

	if got := pool.GetClientCount(); got != 1 {
		t.Errorf("pool count = %d, want 1", got)
	}
}
