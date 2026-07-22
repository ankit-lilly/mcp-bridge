package app

import (
	"context"
	"fmt"

	"github.com/ankit-lilly/mcp-bridge/internal/bridge"
	"github.com/ankit-lilly/mcp-bridge/internal/cli"
)

func RunBridge(ctx context.Context, cfg *cli.Config, ioStreams *IO) error {
	ctx, sess, err := bootstrap(ctx, cfg, ioStreams)
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
		bridge.NewStdioConn(sess.io.Stdin, sess.io.Stdout),
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
func RunInspect(ctx context.Context, cfg *cli.Config, ioStreams *IO) error {
	ctx, sess, err := bootstrap(ctx, cfg, ioStreams)
	if err != nil {
		return err
	}
	defer sess.cancel()

	conn, err := sess.connector.Connect(ctx)
	if err != nil {
		return fmt.Errorf("connecting to remote: %w", err)
	}
	defer conn.Close()

	if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"mcp-bridge-inspect","version":"1.0"}}}`)); err != nil {
		return fmt.Errorf("sending initialize: %w", err)
	}

	resp, err := conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("reading initialize response: %w", err)
	}
	fmt.Fprintf(sess.io.Stderr, "Connected to %s using streamable HTTP\n", cfg.ServerURL)
	fmt.Fprintf(sess.io.Stderr, "Server capabilities: %s\n", string(resp))

	if err := conn.Write(ctx, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); err != nil {
		return fmt.Errorf("sending initialized: %w", err)
	}

	for _, query := range []struct {
		method string
		id     int
		label  string
	}{
		{"tools/list", 2, "Tools"},
		{"resources/list", 3, "Resources"},
		{"prompts/list", 4, "Prompts"},
	} {
		req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"%s","id":%d}`, query.method, query.id)
		if err := conn.Write(ctx, []byte(req)); err != nil {
			return fmt.Errorf("sending %s: %w", query.method, err)
		}
		resp, err = conn.Read(ctx)
		if err != nil {
			fmt.Fprintf(sess.io.Stderr, "No %s response (may not be supported)\n", query.method)
		} else {
			fmt.Fprintf(sess.io.Stderr, "%s: %s\n", query.label, string(resp))
		}
	}

	return nil
}
