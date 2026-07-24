package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestConfigureClaudeOptionsBuildConfig_Minimal(t *testing.T) {
	opts := NewConfigureClaudeOptions()
	opts.Name = "remote-server"

	cfg, err := opts.BuildConfig([]string{"https://example.com/mcp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "remote-server" {
		t.Fatalf("unexpected name: %s", cfg.Name)
	}
	if cfg.DryRun {
		t.Fatal("did not expect dry-run")
	}
	if cfg.Force {
		t.Fatal("did not expect force")
	}
	wantArgs := []string{"https://example.com/mcp"}
	if !reflect.DeepEqual(cfg.BridgeArgs, wantArgs) {
		t.Fatalf("unexpected bridge args: %v", cfg.BridgeArgs)
	}
}

func TestConfigureClaudeOptionsBuildConfig_PreservesBridgeArgs(t *testing.T) {
	opts := NewConfigureClaudeOptions()
	opts.Name = "remote-server"
	opts.ClaudeConfigPath = "/tmp/claude.json"
	opts.DryRun = true
	opts.Force = true

	cfg, err := opts.BuildConfig([]string{
		"--header", "Authorization:${API_KEY}",
		"--allow-http",
		"--callback-port", "8080",
		"http://localhost:8080/mcp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClaudeConfigPath != "/tmp/claude.json" {
		t.Fatalf("unexpected config path: %s", cfg.ClaudeConfigPath)
	}
	if !cfg.DryRun {
		t.Fatal("expected dry-run to be enabled")
	}
	if !cfg.Force {
		t.Fatal("expected force to be enabled")
	}
	wantArgs := []string{
		"--header", "Authorization:${API_KEY}",
		"--allow-http",
		"--callback-port", "8080",
		"http://localhost:8080/mcp",
	}
	if !reflect.DeepEqual(cfg.BridgeArgs, wantArgs) {
		t.Fatalf("unexpected bridge args: %v", cfg.BridgeArgs)
	}
}

func TestConfigureClaudeOptionsBuildConfig_RequiresName(t *testing.T) {
	opts := NewConfigureClaudeOptions()

	_, err := opts.BuildConfig([]string{"https://example.com/mcp"})
	if err == nil {
		t.Fatal("expected missing name to fail")
	}
}

func TestConfigureClaudeOptionsBuildConfig_RequiresBridgeArgs(t *testing.T) {
	opts := NewConfigureClaudeOptions()
	opts.Name = "remote-server"

	_, err := opts.BuildConfig(nil)
	if err == nil {
		t.Fatal("expected missing bridge args to fail")
	}
}

func TestConfigureClaudeOptionsBuildConfig_RejectsSubcommands(t *testing.T) {
	opts := NewConfigureClaudeOptions()
	opts.Name = "remote-server"

	_, err := opts.BuildConfig([]string{"inspect", "https://example.com/mcp"})
	if err == nil {
		t.Fatal("expected bridge subcommand to be rejected")
	}
	if !strings.Contains(err.Error(), `pass only bridge arguments after --`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigureClaudeOptionsBuildConfig_RejectsInvalidBridgeArgs(t *testing.T) {
	opts := NewConfigureClaudeOptions()
	opts.Name = "remote-server"

	_, err := opts.BuildConfig([]string{"https://example.com/mcp", "8080"})
	if err == nil {
		t.Fatal("expected invalid bridge args to fail")
	}
}
