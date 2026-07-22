package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockConn is a test ByteConn backed by channels.
type mockConn struct {
	readCh  chan []byte
	written [][]byte
	mu      sync.Mutex
	closed  bool
}

func newMockConn() *mockConn {
	return &mockConn{readCh: make(chan []byte, 100)}
}

func (m *mockConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case frame, ok := <-m.readCh:
		if !ok {
			return nil, io.EOF
		}
		return frame, nil
	}
}

func (m *mockConn) Write(_ context.Context, frame []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.written = append(m.written, append([]byte(nil), frame...))
	return nil
}

func (m *mockConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.readCh)
	}
	return nil
}

func (m *mockConn) getWritten() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]byte(nil), m.written...)
}

func TestStdioConn_ReadWrite(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"test"}` + "\n"
	var output bytes.Buffer

	conn := NewStdioConn(strings.NewReader(input), &output)
	frame, err := conn.Read(context.Background())
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(frame) != `{"jsonrpc":"2.0","method":"test"}` {
		t.Fatalf("unexpected frame: %s", string(frame))
	}

	err = conn.Write(context.Background(), []byte(`{"jsonrpc":"2.0","result":null}`))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if !strings.Contains(output.String(), `{"jsonrpc":"2.0","result":null}`) {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestStdioConn_EOF(t *testing.T) {
	conn := NewStdioConn(strings.NewReader(""), &bytes.Buffer{})
	_, err := conn.Read(context.Background())
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestRelay_PassThrough(t *testing.T) {
	local := newMockConn()
	remote := newMockConn()

	relay := NewRelay(local, remote, RelayConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()

	// Send from local -> remote
	local.readCh <- []byte(`{"jsonrpc":"2.0","method":"custom/method","id":1}`)
	// Wait for it to arrive
	for len(remote.getWritten()) == 0 {
		time.Sleep(time.Millisecond)
	}

	// Send from remote -> local
	remote.readCh <- []byte(`{"jsonrpc":"2.0","id":1,"result":"ok"}`)
	for len(local.getWritten()) == 0 {
		time.Sleep(time.Millisecond)
	}

	cancel()
	<-done

	remoteWritten := remote.getWritten()
	if len(remoteWritten) == 0 {
		t.Fatal("expected frame forwarded to remote")
	}
	if !strings.Contains(string(remoteWritten[0]), "custom/method") {
		t.Fatalf("unexpected remote frame: %s", string(remoteWritten[0]))
	}

	localWritten := local.getWritten()
	if len(localWritten) == 0 {
		t.Fatal("expected frame forwarded to local")
	}
}

func TestRelay_ToolsCallPassThrough(t *testing.T) {
	local := newMockConn()
	remote := newMockConn()

	relay := NewRelay(local, remote, RelayConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()

	req := `{"jsonrpc":"2.0","method":"tools/call","id":2,"params":{"name":"delete_user"}}`
	local.readCh <- []byte(req)

	for len(remote.getWritten()) == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	remoteWritten := remote.getWritten()
	if len(remoteWritten) == 0 {
		t.Fatal("expected tools/call to be forwarded to remote")
	}
	if string(remoteWritten[0]) != req {
		t.Fatalf("expected forwarded request unchanged, got %s", string(remoteWritten[0]))
	}
	if len(local.getWritten()) != 0 {
		t.Fatal("did not expect a local error response")
	}
}

func TestRelay_ToolsListResponsePassThrough(t *testing.T) {
	local := newMockConn()
	remote := newMockConn()

	relay := NewRelay(local, remote, RelayConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()

	resp := `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"list_users"},{"name":"delete_user"}]}}`
	remote.readCh <- []byte(resp)

	for len(local.getWritten()) == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	localWritten := local.getWritten()
	if len(localWritten) == 0 {
		t.Fatal("expected tools/list response to be forwarded to local")
	}
	if string(localWritten[0]) != resp {
		t.Fatalf("expected response unchanged, got %s", string(localWritten[0]))
	}
}

func TestRelay_InitializeTransform(t *testing.T) {
	local := newMockConn()
	remote := newMockConn()

	relay := NewRelay(local, remote, RelayConfig{ClientID: "mcp-bridge"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()

	initReq := `{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"clientInfo":{"name":"TestClient","version":"1.0"}}}`
	local.readCh <- []byte(initReq)

	for len(remote.getWritten()) == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	remoteWritten := remote.getWritten()
	if len(remoteWritten) == 0 {
		t.Fatal("expected initialize to be forwarded")
	}

	var msg struct {
		Params struct {
			ClientInfo struct {
				Name string `json:"name"`
			} `json:"clientInfo"`
		} `json:"params"`
	}
	if err := json.Unmarshal(remoteWritten[0], &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !strings.Contains(msg.Params.ClientInfo.Name, "mcp-bridge") {
		t.Fatalf("expected client name to contain bridge suffix: %s", msg.Params.ClientInfo.Name)
	}
}

func TestRelay_ServerInitiatedPassThrough(t *testing.T) {
	local := newMockConn()
	remote := newMockConn()

	relay := NewRelay(local, remote, RelayConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()

	// Server-initiated notification (no id, has method)
	remote.readCh <- []byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"token":"abc"}}`)
	// Server-initiated request (has id and method)
	remote.readCh <- []byte(`{"jsonrpc":"2.0","method":"sampling/createMessage","id":"server-1","params":{}}`)

	for len(local.getWritten()) < 2 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done

	localWritten := local.getWritten()
	if len(localWritten) < 2 {
		t.Fatalf("expected 2 messages passed through, got %d", len(localWritten))
	}
}

func TestStdioConn_WriteTrimsTrailingWhitespace(t *testing.T) {
	var output bytes.Buffer
	conn := NewStdioConn(strings.NewReader(""), &output)

	// Simulate frame with trailing \r\n (as HTTP response bodies often have)
	err := conn.Write(context.Background(), []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\r\n"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Should produce exactly one JSON line followed by one newline
	got := output.String()
	if got != "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n" {
		t.Fatalf("expected single JSON line with trailing newline, got %q", got)
	}
}

func TestStdioConn_WriteEmptyFrameDiscarded(t *testing.T) {
	var output bytes.Buffer
	conn := NewStdioConn(strings.NewReader(""), &output)

	// A frame that is all whitespace should be discarded
	err := conn.Write(context.Background(), []byte("\r\n"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("expected empty output for whitespace-only frame, got %q", output.String())
	}
}

func TestRelay_DisconnectPropagation(t *testing.T) {
	local := newMockConn()
	remote := newMockConn()

	relay := NewRelay(local, remote, RelayConfig{})

	// Close local immediately
	local.Close()

	err := relay.Run(context.Background())
	if err != nil {
		t.Fatalf("expected clean shutdown, got error: %v", err)
	}
}
