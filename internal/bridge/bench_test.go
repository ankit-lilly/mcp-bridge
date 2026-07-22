package bridge

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func BenchmarkRelay_PassThrough(b *testing.B) {
	frame := []byte(`{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"safe_tool","arguments":{"key":"value"}}}`)

	local := newMockConn()
	remote := newMockConn()

	relay := NewRelay(local, remote, RelayConfig{})
	ctx := b.Context()

	go relay.Run(ctx)

	for b.Loop() {
		local.readCh <- frame
	}
}

func BenchmarkRelay_InitializeTransform(b *testing.B) {
	request := []byte(`{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"clientInfo":{"name":"TestClient","version":"1.0"}}}`)
	for b.Loop() {
		local := newMockConn()
		remote := newMockConn()
		relay := NewRelay(local, remote, RelayConfig{ClientID: "mcp-bridge"})
		ctx, cancel := context.WithCancel(context.Background())

		go relay.Run(ctx)
		local.readCh <- request

		for len(remote.getWritten()) == 0 {
			time.Sleep(time.Microsecond)
		}

		var msg struct {
			Params struct {
				ClientInfo struct {
					Name string `json:"name"`
				} `json:"clientInfo"`
			} `json:"params"`
		}
		_ = json.Unmarshal(remote.getWritten()[0], &msg)
		cancel()
	}
}
