package app

import (
	"context"
	"fmt"
	"io"

	"github.com/ankit-lilly/mcp-bridge/internal/bridge"
	"github.com/ankit-lilly/mcp-bridge/internal/config"
)

const (
	inspectInitializeRPC = `{
		"jsonrpc": "2.0",
		"method": "initialize",
		"id": 1,
		"params": {
			"protocolVersion": "2024-11-05",
			"capabilities": {},
			"clientInfo": {
				"name": "mcp-bridge-inspect",
				"version": "1.0"
			}
		}
	}`
	inspectInitializedRPC = `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	inspectToolsListRPC   = `{"jsonrpc":"2.0","method":"tools/list","id":2}`
	inspectResourcesRPC   = `{"jsonrpc":"2.0","method":"resources/list","id":3}`
	inspectPromptsListRPC = `{"jsonrpc":"2.0","method":"prompts/list","id":4}`
)

type inspectQuery struct {
	method  string
	label   string
	request string
}

var inspectQueries = []inspectQuery{
	{method: "tools/list", label: "Tools", request: inspectToolsListRPC},
	{method: "resources/list", label: "Resources", request: inspectResourcesRPC},
	{method: "prompts/list", label: "Prompts", request: inspectPromptsListRPC},
}

func RunBridge(ctx context.Context, cfg *config.BridgeConfig, stdin io.Reader, stdout, stderr io.Writer) error {
	ctx, sess, err := bootstrap(ctx, cfg, stdin, stdout, stderr)
	if err != nil {
		return err
	}
	defer sess.cancel()

	remoteConn, err := sess.connector.Connect(ctx)
	if err != nil {
		return fmt.Errorf("connecting to remote: %w", err)
	}
	defer remoteConn.Close()

	relay := bridge.NewRelay(
		bridge.NewStdioConn(sess.stdin, sess.stdout),
		remoteConn,
		bridge.RelayConfig{
			Logger:   sess.logger,
			ClientID: "mcp-bridge",
		},
	)

	sess.logger.Info("relay started")
	return relay.Run(ctx)
}

// RunInspect is the main entry point for the diagnostic inspect mode.
func RunInspect(ctx context.Context, cfg *config.BridgeConfig, stdin io.Reader, stdout, stderr io.Writer) error {
	ctx, sess, err := bootstrap(ctx, cfg, stdin, stdout, stderr)
	if err != nil {
		return err
	}
	defer sess.cancel()

	conn, err := sess.connector.Connect(ctx)
	if err != nil {
		return fmt.Errorf("connecting to remote: %w", err)
	}
	defer conn.Close()

	if err := conn.Write(ctx, []byte(inspectInitializeRPC)); err != nil {
		return fmt.Errorf("sending initialize: %w", err)
	}

	resp, err := conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("reading initialize response: %w", err)
	}
	fmt.Fprintf(sess.stderr, "Connected to %s using streamable HTTP\n", cfg.ServerURL)
	fmt.Fprintf(sess.stderr, "Server capabilities: %s\n", string(resp))

	if err := conn.Write(ctx, []byte(inspectInitializedRPC)); err != nil {
		return fmt.Errorf("sending initialized: %w", err)
	}

	for _, query := range inspectQueries {
		if err := conn.Write(ctx, []byte(query.request)); err != nil {
			return fmt.Errorf("sending %s: %w", query.method, err)
		}
		resp, err = conn.Read(ctx)
		if err != nil {
			fmt.Fprintf(sess.stderr, "No %s response (may not be supported)\n", query.method)
		} else {
			fmt.Fprintf(sess.stderr, "%s: %s\n", query.label, string(resp))
		}
	}

	return nil
}
