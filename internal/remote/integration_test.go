package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestIntegration_FullStreamableHTTPFlow simulates a realistic MCP server that:
// 1. Returns 401 on first POST (triggers auth)
// 2. After auth, accepts initialize and returns session ID + protocol version
// 3. Serves the optional server event stream with server-initiated notifications
// 4. Accepts subsequent POSTs with session ID
// 5. Responds to DELETE on close
func TestIntegration_FullStreamableHTTPFlow(t *testing.T) {
	var (
		mu          sync.Mutex
		authed      bool
		sessionID   = "integration-session-abc"
		protoVer    = "2024-11-05"
		methods     []string
		deleteCalls atomic.Int32
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		isAuthed := authed
		mu.Unlock()

		switch r.Method {
		case "POST":
			if !isAuthed {
				w.Header().Set("WWW-Authenticate", `Bearer scope="mcp:read mcp:write", resource_metadata="https://example.com/.well-known/oauth-protected-resource"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			// Verify session ID on non-initial requests
			if sid := r.Header.Get("Mcp-Session-Id"); sid != "" && sid != sessionID {
				http.Error(w, "invalid session", http.StatusBadRequest)
				return
			}

			var msg struct {
				Method string `json:"method"`
				ID     any    `json:"id"`
			}
			json.NewDecoder(r.Body).Decode(&msg)
			mu.Lock()
			methods = append(methods, msg.Method)
			mu.Unlock()

			w.Header().Set("Mcp-Session-Id", sessionID)

			switch msg.Method {
			case "initialize":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      msg.ID,
					"result": map[string]any{
						"protocolVersion": protoVer,
						"serverInfo":      map[string]any{"name": "integration-server", "version": "1.0"},
						"capabilities":    map[string]any{"tools": map[string]any{}},
					},
				})
			case "notifications/initialized":
				w.WriteHeader(http.StatusAccepted)
			case "tools/list":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      msg.ID,
					"result": map[string]any{
						"tools": []map[string]any{
							{"name": "read_file", "description": "Reads a file"},
							{"name": "write_file", "description": "Writes a file"},
						},
					},
				})
			default:
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      msg.ID,
					"result":  nil,
				})
			}

		case "GET":
			// Optional server event stream
			if !isAuthed {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Mcp-Session-Id") != sessionID {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			flusher := w.(http.Flusher)
			// Send a server notification
			fmt.Fprintf(w, "data: %s\n\n", `{"jsonrpc":"2.0","method":"notifications/resources/updated","params":{"uri":"file:///changed"}}`)
			flusher.Flush()
			<-r.Context().Done()

		case "DELETE":
			deleteCalls.Add(1)
			if r.Header.Get("Mcp-Session-Id") != sessionID {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	// Mock authorizer that "succeeds" by flipping the authed flag
	mockAuth := &mockAuthorizer{fn: func(ctx context.Context, c *AuthRequiredError) error {
		if c.StatusCode != 401 {
			t.Errorf("expected 401, got %d", c.StatusCode)
		}
		// Verify challenge headers were captured
		wwwAuth := c.Headers.Get("WWW-Authenticate")
		if !strings.Contains(wwwAuth, "mcp:read") {
			t.Errorf("expected scope in WWW-Authenticate, got %q", wwwAuth)
		}
		mu.Lock()
		authed = true
		mu.Unlock()
		return nil
	}}

	connector := NewHTTPConnector(HTTPConnectorConfig{
		Client:      server.Client(),
		URL:         server.URL,
		Authorizer:  mockAuth,
		TokenSource: StaticToken("valid-token"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	// 1. Initialize (first POST triggers 401 -> auth -> retry)
	initReq := `{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	if err := conn.Write(ctx, []byte(initReq)); err != nil {
		t.Fatalf("initialize write failed: %v", err)
	}

	initResp, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("initialize read failed: %v", err)
	}
	if !strings.Contains(string(initResp), "integration-server") {
		t.Fatalf("unexpected init response: %s", string(initResp))
	}
	if !strings.Contains(string(initResp), protoVer) {
		t.Fatalf("expected protocol version in response: %s", string(initResp))
	}

	// 2. Send initialized notification
	if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); err != nil {
		t.Fatalf("initialized write failed: %v", err)
	}

	// 3. Wait for the server event stream to deliver a notification
	time.Sleep(200 * time.Millisecond)
	streamMsg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("event stream notification read failed: %v", err)
	}
	if !strings.Contains(string(streamMsg), "resources/updated") {
		t.Fatalf("unexpected event stream message: %s", string(streamMsg))
	}

	// 4. Send tools/list with session
	if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"tools/list","id":2}`)); err != nil {
		t.Fatalf("tools/list write failed: %v", err)
	}
	toolsResp, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("tools/list read failed: %v", err)
	}
	if !strings.Contains(string(toolsResp), "read_file") {
		t.Fatalf("unexpected tools response: %s", string(toolsResp))
	}

	// 5. Close — should DELETE
	conn.Close()
	time.Sleep(200 * time.Millisecond)

	if n := deleteCalls.Load(); n != 1 {
		t.Fatalf("expected 1 DELETE call, got %d", n)
	}

	// Verify method sequence
	mu.Lock()
	defer mu.Unlock()
	expected := []string{"initialize", "notifications/initialized", "tools/list"}
	if len(methods) != len(expected) {
		t.Fatalf("expected methods %v, got %v", expected, methods)
	}
	for i, m := range expected {
		if methods[i] != m {
			t.Fatalf("method[%d]: expected %q, got %q", i, m, methods[i])
		}
	}
}
