package cli

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ankit-lilly/mcp-bridge/internal/config"
)

func TestRootCommandShowsHelpWithoutSubcommand(t *testing.T) {
	cmd, stdout, _ := newTestCommand(CommandHandlers{})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	for _, want := range []string{
		RootCommandUse,
		"bridge",
		"inspect",
		"configure-claude",
		"version",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected help output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestBridgeCommandRuns(t *testing.T) {
	var got *config.BridgeConfig
	cmd, _, _ := newTestCommand(CommandHandlers{
		RunBridge: func(_ context.Context, cfg *config.BridgeConfig, _ io.Reader, _, _ io.Writer) error {
			got = cfg
			return nil
		},
	}, "bridge", "https://example.com/mcp")

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected bridge handler to be called")
	}
	if got.ServerURL != "https://example.com/mcp" {
		t.Fatalf("unexpected server URL: %s", got.ServerURL)
	}
}

func TestInspectCommandRuns(t *testing.T) {
	var got *config.BridgeConfig
	cmd, _, _ := newTestCommand(CommandHandlers{
		RunInspect: func(_ context.Context, cfg *config.BridgeConfig, _ io.Reader, _, _ io.Writer) error {
			got = cfg
			return nil
		},
	}, "inspect", "https://example.com/mcp")

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected inspect handler to be called")
	}
	if got.ServerURL != "https://example.com/mcp" {
		t.Fatalf("unexpected server URL: %s", got.ServerURL)
	}
}

func TestConfigureClaudeCommandRuns(t *testing.T) {
	var got *config.ClaudeInstallConfig
	cmd, _, _ := newTestCommand(CommandHandlers{
		RunConfigureClaude: func(_ context.Context, cfg *config.ClaudeInstallConfig, _, _ io.Writer) error {
			got = cfg
			return nil
		},
	}, "configure-claude", "--name", "remote-server", "--", "--allow-http", "http://localhost/mcp")

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected configure-claude handler to be called")
	}
	if got.Name != "remote-server" {
		t.Fatalf("unexpected name: %s", got.Name)
	}
	wantArgs := []string{"--allow-http", "http://localhost/mcp"}
	if !reflect.DeepEqual(got.BridgeArgs, wantArgs) {
		t.Fatalf("unexpected bridge args: %v", got.BridgeArgs)
	}
}

func TestVersionCommandPrintsVersion(t *testing.T) {
	cmd, stdout, _ := newTestCommand(CommandHandlers{}, "version")

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "mcp-bridge test-version" {
		t.Fatalf("unexpected version output: %q", stdout.String())
	}
}

func TestRootCommandRunsBridgeWithBareURL(t *testing.T) {
	var got *config.BridgeConfig
	cmd, _, _ := newTestCommand(CommandHandlers{
		RunBridge: func(_ context.Context, cfg *config.BridgeConfig, _ io.Reader, _, _ io.Writer) error {
			got = cfg
			return nil
		},
	}, "https://example.com/mcp")

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected bridge handler to be called for bare URL")
	}
	if got.ServerURL != "https://example.com/mcp" {
		t.Fatalf("unexpected server URL: %s", got.ServerURL)
	}
}

func TestConfigureClaudeRequiresSeparator(t *testing.T) {
	cmd, _, _ := newTestCommand(CommandHandlers{}, "configure-claude", "--name", "remote-server", "https://example.com/mcp")

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected missing -- separator to fail")
	}
	if !strings.Contains(err.Error(), "bridge arguments must be passed after --") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func newTestCommand(handlers CommandHandlers, args ...string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	if handlers.RunBridge == nil {
		handlers.RunBridge = func(context.Context, *config.BridgeConfig, io.Reader, io.Writer, io.Writer) error { return nil }
	}
	if handlers.RunInspect == nil {
		handlers.RunInspect = func(context.Context, *config.BridgeConfig, io.Reader, io.Writer, io.Writer) error { return nil }
	}
	if handlers.RunConfigureClaude == nil {
		handlers.RunConfigureClaude = func(context.Context, *config.ClaudeInstallConfig, io.Writer, io.Writer) error { return nil }
	}
	if handlers.Version == "" {
		handlers.Version = "mcp-bridge test-version"
	}

	cmd := NewRootCommand(strings.NewReader(""), stdout, stderr, handlers)
	cmd.SetArgs(args)
	return cmd, stdout, stderr
}
