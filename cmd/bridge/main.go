package main

import (
	"context"
	"errors"
	"flag"
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
	rawArgs := os.Args[1:]
	if len(rawArgs) > 0 && isHelpArg(rawArgs[0]) {
		cli.WriteRootHelp(os.Stdout)
		os.Exit(0)
	}

	runMode, args, err := splitMode(rawArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if len(args) > 0 && isVersionArg(args[0]) {
		fmt.Fprintln(os.Stderr, version.String())
		os.Exit(0)
	}

	if err := run(runMode, context.Background(), args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
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
	case "bridge":
		return bridgeMode, args[1:], nil
	default:
		return bridgeMode, args, nil
	}
}

func isVersionArg(arg string) bool {
	return arg == "--version" || arg == "-v"
}

func isHelpArg(arg string) bool {
	return arg == "--help" || arg == "-h"
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
		cfg, err := cli.ParseInspect(args)
		if err != nil {
			return err
		}
		return app.RunInspect(ctx, cfg, ioStreams)
	default:
		cfg, err := cli.ParseBridge(args)
		if err != nil {
			return err
		}
		return app.RunBridge(ctx, cfg, ioStreams)
	}
}
