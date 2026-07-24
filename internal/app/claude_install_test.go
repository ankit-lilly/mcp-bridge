package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ankit-lilly/mcp-bridge/internal/config"
)

func TestRunConfigureClaude_DryRun(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "Claude", "claude_desktop_config.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := RunConfigureClaude(context.Background(), &config.ClaudeInstallConfig{
		Name:             "remote-server",
		ClaudeConfigPath: configPath,
		DryRun:           true,
		BridgeArgs:       []string{"https://example.com/mcp"},
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout.Len() == 0 {
		t.Fatal("expected dry-run output")
	}
	if stderr.Len() != 0 {
		t.Fatalf("did not expect stderr output: %s", stderr.String())
	}
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no config file to be written, got err=%v", err)
	}
}

func TestRunConfigureClaude_WritesMergedConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "Claude", "claude_desktop_config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"theme":"dark"}`), 0600); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := RunConfigureClaude(context.Background(), &config.ClaudeInstallConfig{
		Name:             "remote-server",
		ClaudeConfigPath: configPath,
		BridgeArgs:       []string{"--header", "X-Test:${TOKEN}", "https://example.com/mcp"},
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected status output")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	var root struct {
		Theme      string `json:"theme"`
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if root.Theme != "dark" {
		t.Fatalf("expected theme to be preserved, got %q", root.Theme)
	}
	entry, ok := root.MCPServers["remote-server"]
	if !ok {
		t.Fatalf("expected remote-server entry: %s", string(data))
	}
	if entry.Command == "" {
		t.Fatalf("expected executable command path: %s", string(data))
	}
	wantArgs := []string{"bridge", "--header", "X-Test:${TOKEN}", "https://example.com/mcp"}
	if !reflect.DeepEqual(entry.Args, wantArgs) {
		t.Fatalf("unexpected args: %v", entry.Args)
	}
}
