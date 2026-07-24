package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ankit-lilly/mcp-bridge/internal/app"
	"github.com/ankit-lilly/mcp-bridge/internal/cli"
	"github.com/ankit-lilly/mcp-bridge/internal/version"
)

func main() {
	cmd := cli.NewRootCommand(os.Stdin, os.Stdout, os.Stderr, cli.CommandHandlers{
		RunBridge:          app.RunBridge,
		RunInspect:         app.RunInspect,
		RunConfigureClaude: app.RunConfigureClaude,
		Version:            version.String(),
	})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
