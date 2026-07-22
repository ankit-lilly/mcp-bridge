package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamableHTTP_NoPreInitGET(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	connector := NewHTTPConnector(HTTPConnectorConfig{
		Client: server.Client(),
		URL:    server.URL,
	})

	conn, err := connector.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer conn.Close()

	// No requests should have been made on connect
	if n := requestCount.Load(); n != 0 {
		t.Fatalf("expected 0 requests on connect, got %d", n)
	}
}

func TestStreamableHTTP_PostJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var msg struct {
			Method string `json:"method"`
			ID     any    `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&msg)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      msg.ID,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "test-server", "version": "1.0"},
			},
		})
	}))
	defer server.Close()

	connector := NewHTTPConnector(HTTPConnectorConfig{
		Client: server.Client(),
		URL:    server.URL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer conn.Close()

	req := `{"jsonrpc":"2.0","method":"initialize","id":1,"params":{}}`
	if err := conn.Write(ctx, []byte(req)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	resp, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(string(resp), "test-server") {
		t.Fatalf("unexpected response: %s", string(resp))
	}
}

func TestStreamableHTTP_PostSSEResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
		flusher.Flush()
		fmt.Fprintf(w, "data: %s\n\n", `{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`)
		flusher.Flush()
	}))
	defer server.Close()

	connector := NewHTTPConnector(HTTPConnectorConfig{
		Client: server.Client(),
		URL:    server.URL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer conn.Close()

	if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"tools/list","id":1}`)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Should receive both SSE messages
	msg1, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read 1 failed: %v", err)
	}
	if !strings.Contains(string(msg1), "tools") {
		t.Fatalf("unexpected msg1: %s", string(msg1))
	}

	msg2, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read 2 failed: %v", err)
	}
	if !strings.Contains(string(msg2), "notifications/progress") {
		t.Fatalf("unexpected msg2: %s", string(msg2))
	}
}

func TestStreamableHTTP_InitializeCapturesSessionID(t *testing.T) {
	var receivedSessionID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		receivedSessionID = r.Header.Get("Mcp-Session-Id")

		var msg struct {
			Method string `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&msg)

		w.Header().Set("Content-Type", "application/json")
		if msg.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "session-abc-123")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"protocolVersion": "2024-11-05"},
		})
	}))
	defer server.Close()

	connector := NewHTTPConnector(HTTPConnectorConfig{
		Client: server.Client(),
		URL:    server.URL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer conn.Close()

	// First request: initialize (no session ID expected)
	if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"initialize","id":1,"params":{}}`)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	conn.Read(ctx) // consume response

	if receivedSessionID != "" {
		t.Fatalf("first request should not have session ID, got %q", receivedSessionID)
	}

	// Second request: should include the session ID
	if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"tools/list","id":2}`)); err != nil {
		t.Fatalf("write 2 failed: %v", err)
	}
	conn.Read(ctx) // consume response

	if receivedSessionID != "session-abc-123" {
		t.Fatalf("expected session ID session-abc-123, got %q", receivedSessionID)
	}
}

func TestStreamableHTTP_ServerEventStreamStartsAfterInit(t *testing.T) {
	var getReceived atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			getReceived.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			fmt.Fprintf(w, "data: %s\n\n", `{"jsonrpc":"2.0","method":"notifications/test"}`)
			flusher.Flush()
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-1")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"protocolVersion": "2024-11-05"},
		})
	}))
	defer server.Close()

	connector := NewHTTPConnector(HTTPConnectorConfig{
		Client: server.Client(),
		URL:    server.URL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer conn.Close()

	// No GET before initialize
	if n := getReceived.Load(); n != 0 {
		t.Fatalf("expected 0 GETs before init, got %d", n)
	}

	// Send initialize
	if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"initialize","id":1,"params":{}}`)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	// Consume the JSON response from POST
	conn.Read(ctx)

	// Wait for the server event stream to start
	time.Sleep(100 * time.Millisecond)
	if n := getReceived.Load(); n == 0 {
		t.Fatal("expected GET for server event stream after initialize")
	}

	// Should receive the notification from the server event stream
	msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read event stream notification failed: %v", err)
	}
	if !strings.Contains(string(msg), "notifications/test") {
		t.Fatalf("unexpected event stream message: %s", string(msg))
	}
}

func TestStreamableHTTP_StandaloneEventStreamNotSupportedTolerated(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					w.WriteHeader(status)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Mcp-Session-Id", "sess-1")
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"result":  map[string]any{"protocolVersion": "2024-11-05"},
				})
			}))
			defer server.Close()

			connector := NewHTTPConnector(HTTPConnectorConfig{
				Client: server.Client(),
				URL:    server.URL,
			})

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			conn, err := connector.Connect(ctx)
			if err != nil {
				t.Fatalf("connect failed: %v", err)
			}
			defer conn.Close()

			if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"initialize","id":1,"params":{}}`)); err != nil {
				t.Fatalf("write failed: %v", err)
			}
			conn.Read(ctx)

			time.Sleep(100 * time.Millisecond)

			if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"tools/list","id":2}`)); err != nil {
				t.Fatalf("write after GET %d failed: %v", status, err)
			}
		})
	}
}

func TestStreamableHTTP_DeleteOnClose(t *testing.T) {
	var deleteReceived atomic.Int32
	var deleteSessionID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deleteReceived.Add(1)
			deleteSessionID = r.Header.Get("Mcp-Session-Id")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "delete-test-session")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"protocolVersion": "2024-11-05"},
		})
	}))
	defer server.Close()

	connector := NewHTTPConnector(HTTPConnectorConfig{
		Client: server.Client(),
		URL:    server.URL,
	})

	conn, err := connector.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Initialize to establish session
	if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"initialize","id":1,"params":{}}`)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	conn.Read(ctx)

	// Close should send DELETE
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	if n := deleteReceived.Load(); n != 1 {
		t.Fatalf("expected 1 DELETE, got %d", n)
	}
	if deleteSessionID != "delete-test-session" {
		t.Fatalf("expected session ID in DELETE, got %q", deleteSessionID)
	}
}

func TestStreamableHTTP_AuthRetryOnce(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		n := callCount.Add(1)
		if n == 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  "ok",
		})
	}))
	defer server.Close()

	authCalled := false
	mockAuth := &mockAuthorizer{fn: func(ctx context.Context, c *AuthRequiredError) error {
		authCalled = true
		if c.StatusCode != 401 {
			t.Fatalf("expected 401, got %d", c.StatusCode)
		}
		return nil
	}}

	connector := NewHTTPConnector(HTTPConnectorConfig{
		Client:     server.Client(),
		URL:        server.URL,
		Authorizer: mockAuth,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer conn.Close()

	if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if !authCalled {
		t.Fatal("expected authorizer to be called")
	}

	resp, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(string(resp), `"result":"ok"`) {
		t.Fatalf("unexpected response: %s", string(resp))
	}
}

func TestStreamableHTTP_WithToken(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  nil,
		})
	}))
	defer server.Close()

	connector := NewHTTPConnector(HTTPConnectorConfig{
		Client:      server.Client(),
		URL:         server.URL,
		TokenSource: StaticToken("my-token"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer conn.Close()

	if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"test","id":1}`)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	conn.Read(ctx)

	if receivedAuth != "Bearer my-token" {
		t.Fatalf("expected Bearer my-token, got %s", receivedAuth)
	}
}

func TestStreamableHTTP_ProtocolVersionHeader(t *testing.T) {
	var versions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		versions = append(versions, r.Header.Get("Mcp-Protocol-Version"))

		var msg struct {
			Method string `json:"method"`
		}
		json.NewDecoder(r.Body).Decode(&msg)

		w.Header().Set("Content-Type", "application/json")
		if msg.Method == "initialize" {
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"serverInfo":      map[string]any{"name": "test"},
				},
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "result": nil})
		}
	}))
	defer server.Close()

	connector := NewHTTPConnector(HTTPConnectorConfig{
		Client: server.Client(),
		URL:    server.URL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := connector.Connect(ctx)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer conn.Close()

	// First request: initialize — should send default version
	conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"initialize","id":1,"params":{}}`))
	conn.Read(ctx)

	// Second request: should use negotiated version from response
	conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"tools/list","id":2}`))
	conn.Read(ctx)

	if len(versions) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(versions))
	}
	if versions[0] != "2025-03-26" {
		t.Fatalf("first request should use default version, got %q", versions[0])
	}
	if versions[1] != "2024-11-05" {
		t.Fatalf("second request should use negotiated version '2024-11-05', got %q", versions[1])
	}
}

func TestStreamableHTTP_UnsupportedEndpointErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       string
	}{
		{name: "not-found", statusCode: http.StatusNotFound, want: "streamable HTTP endpoint not found (404)"},
		{name: "method-not-allowed", statusCode: http.StatusMethodNotAllowed, want: "streamable HTTP POST not allowed (405)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Fatalf("unexpected method: %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			connector := NewHTTPConnector(HTTPConnectorConfig{
				Client: server.Client(),
				URL:    server.URL,
			})

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			conn, err := connector.Connect(ctx)
			if err != nil {
				t.Fatalf("connect failed: %v", err)
			}
			defer conn.Close()

			err = conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"initialize","id":1,"params":{}}`))
			if err == nil {
				t.Fatal("expected write to fail")
			}
			if err.Error() != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, err.Error())
			}
		})
	}
}

// mockAuthorizer for testing
type mockAuthorizer struct {
	fn func(ctx context.Context, c *AuthRequiredError) error
}

func (m *mockAuthorizer) EnsureAuthorized(ctx context.Context, challenge *AuthRequiredError) error {
	return m.fn(ctx, challenge)
}
