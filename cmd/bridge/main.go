package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ankit-lilly/mcp-bridge/internal/app"
	"github.com/ankit-lilly/mcp-bridge/internal/cli"
	"github.com/ankit-lilly/mcp-bridge/internal/version"
)

type mode uint8

const (
	bridgeMode mode = iota
	inspectMode
	configureClaudeMode
)

func main() {
	runMode, args, err := splitMode(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if len(args) > 0 && isVersionArg(args[0]) {
		fmt.Fprintln(os.Stderr, version.String())
		os.Exit(0)
	}

	if err := run(runMode, context.Background(), args); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func splitMode(args []string) (mode, []string, error) {
	if len(args) == 0 {
		return bridgeMode, args, nil
	}

	switch args[0] {
	case "inspect":
		return inspectMode, args[1:], nil
	case "configure-claude":
		return configureClaudeMode, args[1:], nil
	case "client":
		return bridgeMode, nil, fmt.Errorf(`the "client" subcommand has been removed; use "inspect" or the default bridge mode`)
	case "bridge":
		return bridgeMode, args[1:], nil
	default:
		return bridgeMode, args, nil
	}
}

func isVersionArg(arg string) bool {
	return arg == "--version" || arg == "-v"
}

func run(m mode, ctx context.Context, args []string) error {
	ioStreams := app.DefaultIO()
	switch m {
	case configureClaudeMode:
		cfg, err := cli.ParseConfigureClaude(args)
		if err != nil {
			return err
		}
		return app.RunConfigureClaude(ctx, cfg, ioStreams)
	case inspectMode:
		cfg, err := cli.Parse(args)
		if err != nil {
			return err
		}
		return app.RunInspect(ctx, cfg, ioStreams)
	default:
		cfg, err := cli.Parse(args)
		if err != nil {
			return err
		}
		return app.RunBridge(ctx, cfg, ioStreams)
	}
}
