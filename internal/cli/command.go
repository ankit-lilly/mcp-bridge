package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/ankit-lilly/mcp-bridge/internal/config"
)

// CommandHandlers holds the handler functions injected by main.
type CommandHandlers struct {
	RunBridge          func(context.Context, *config.BridgeConfig, io.Reader, io.Writer, io.Writer) error
	RunInspect         func(context.Context, *config.BridgeConfig, io.Reader, io.Writer, io.Writer) error
	RunConfigureClaude func(context.Context, *config.ClaudeInstallConfig, io.Writer, io.Writer) error
	Version            string
}

func NewRootCommand(stdin io.Reader, stdout, stderr io.Writer, handlers CommandHandlers) *cobra.Command {
	opts := NewBridgeOptions()

	rootCmd := &cobra.Command{
		Use:           RootCommandUse,
		Short:         "Bridge stdio MCP to a remote streamable HTTP server",
		Long:          "Bridge stdio MCP to a remote streamable HTTP server, inspect remote servers, and install Claude Desktop entries.",
		SilenceErrors: true,
		SilenceUsage:  true,
		// When invoked without a subcommand but with args (e.g. `mcp-bridge <url>`),
		// default to bridge behavior. This handles cobra's TraverseChildren flow.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			cfg, err := opts.BuildConfig(args)
			if err != nil {
				return err
			}
			return handlers.RunBridge(cmd.Context(), cfg, stdin, stdout, stderr)
		},
		// Allow unknown args to reach RunE instead of failing as "unknown command".
		TraverseChildren: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}
	rootCmd.Flags().SetInterspersed(false)
	opts.AddFlags(rootCmd.Flags())

	rootCmd.SetIn(stdin)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.AddCommand(
		newVersionCommand(handlers.Version),
		newBridgeConfigCommand(BridgeCommandUse, "Run the stdio bridge", stdin, stdout, stderr, handlers.RunBridge),
		newBridgeConfigCommand(InspectCommandUse, "Diagnose a remote server", stdin, stdout, stderr, handlers.RunInspect),
		newConfigureClaudeCommand(stdout, stderr, handlers.RunConfigureClaude),
	)

	return rootCmd
}

func newVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:           "version",
		Short:         "Print version information",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version)
			return nil
		},
	}
}

func newBridgeConfigCommand(
	use string,
	short string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	run func(context.Context, *config.BridgeConfig, io.Reader, io.Writer, io.Writer) error,
) *cobra.Command {
	opts := NewBridgeOptions()
	cmd := &cobra.Command{
		Use:           use,
		Short:         short,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := opts.BuildConfig(args)
			if err != nil {
				return err
			}
			return run(cmd.Context(), cfg, stdin, stdout, stderr)
		},
	}
	cmd.Flags().SetInterspersed(false)
	opts.AddFlags(cmd.Flags())
	return cmd
}

func newConfigureClaudeCommand(
	stdout, stderr io.Writer,
	run func(context.Context, *config.ClaudeInstallConfig, io.Writer, io.Writer) error,
) *cobra.Command {
	opts := NewConfigureClaudeOptions()
	cmd := &cobra.Command{
		Use:           ConfigureClaudeCommandUse,
		Short:         "Add or update a Claude Desktop mcpServers entry",
		Long:          "Add or update a Claude Desktop mcpServers entry. Pass bridge arguments after --.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && cmd.ArgsLenAtDash() != 0 {
				return errors.New("bridge arguments must be passed after --")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := opts.BuildConfig(args)
			if err != nil {
				return err
			}
			return run(cmd.Context(), cfg, stdout, stderr)
		},
	}
	cmd.Flags().SetInterspersed(false)
	opts.AddFlags(cmd.Flags())
	_ = cmd.MarkFlagRequired("name")
	return cmd
}
