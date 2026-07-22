package cli

import (
	"reflect"
	"testing"
)

func TestParseConfigureClaude_Minimal(t *testing.T) {
	cfg, err := ParseConfigureClaude([]string{"--name", "remote-server", "https://example.com/mcp"})
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

func TestParseConfigureClaude_PreservesBridgeArgs(t *testing.T) {
	cfg, err := ParseConfigureClaude([]string{
		"--name=remote-server",
		"--claude-config", "/tmp/claude.json",
		"--dry-run",
		"--force",
		"--",
		"--header", "Authorization:${API_KEY}",
		"--allow-http",
		"http://localhost:8080/mcp",
		"8080",
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
		"http://localhost:8080/mcp",
		"8080",
	}
	if !reflect.DeepEqual(cfg.BridgeArgs, wantArgs) {
		t.Fatalf("unexpected bridge args: %v", cfg.BridgeArgs)
	}
}

func TestParseConfigureClaude_RejectsInspectMode(t *testing.T) {
	_, err := ParseConfigureClaude([]string{"--name", "remote-server", "inspect", "https://example.com/mcp"})
	if err == nil {
		t.Fatal("expected inspect mode to be rejected")
	}
}

func TestParseConfigureClaude_RequiresName(t *testing.T) {
	_, err := ParseConfigureClaude([]string{"https://example.com/mcp"})
	if err == nil {
		t.Fatal("expected missing name to fail")
	}
}

func TestParseConfigureClaude_SupportsPassthroughSeparator(t *testing.T) {
	cfg, err := ParseConfigureClaude([]string{
		"--name", "remote-server",
		"--",
		"--header", "X-Api-Key:${API_KEY}",
		"https://example.com/mcp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := []string{"--header", "X-Api-Key:${API_KEY}", "https://example.com/mcp"}
	if !reflect.DeepEqual(cfg.BridgeArgs, wantArgs) {
		t.Fatalf("unexpected bridge args: %v", cfg.BridgeArgs)
	}
}

func TestParseConfigureClaude_RequiresSeparatorForBridgeFlags(t *testing.T) {
	_, err := ParseConfigureClaude([]string{
		"--name", "remote-server",
		"--header", "X-Api-Key:${API_KEY}",
		"https://example.com/mcp",
	})
	if err == nil {
		t.Fatal("expected bridge flags before -- to fail")
	}
}
