package claudeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingReturnsEmptyDocument(t *testing.T) {
	doc, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := doc.MarshalIndent()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !strings.Contains(string(data), `"mcpServers": {}`) {
		t.Fatalf("expected empty mcpServers object, got %s", string(data))
	}
}

func TestSetServerLifecycle(t *testing.T) {
	doc := New()
	entry := ServerConfig{
		Command: "/usr/local/bin/mcp-bridge",
		Args:    []string{"https://example.com/mcp"},
	}

	result, err := doc.SetServer("remote-server", entry, false)
	if err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if result != MergeCreated {
		t.Fatalf("expected create result, got %s", result)
	}

	result, err = doc.SetServer("remote-server", entry, false)
	if err != nil {
		t.Fatalf("set same entry failed: %v", err)
	}
	if result != MergeUnchanged {
		t.Fatalf("expected unchanged result, got %s", result)
	}

	_, err = doc.SetServer("remote-server", ServerConfig{
		Command: "/usr/local/bin/mcp-bridge",
		Args:    []string{"https://example.com/other"},
	}, false)
	if err == nil {
		t.Fatal("expected conflict without force")
	}

	result, err = doc.SetServer("remote-server", ServerConfig{
		Command: "/usr/local/bin/mcp-bridge",
		Args:    []string{"https://example.com/other"},
	}, true)
	if err != nil {
		t.Fatalf("forced replace failed: %v", err)
	}
	if result != MergeUpdated {
		t.Fatalf("expected update result, got %s", result)
	}
}

func TestLoadPreservesUnrelatedKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	err := os.WriteFile(path, []byte(`{"theme":"dark","mcpServers":{"existing":{"command":"old","args":["a"]}}}`), 0600)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	doc, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	_, err = doc.SetServer("new", ServerConfig{
		Command: "/usr/local/bin/mcp-bridge",
		Args:    []string{"https://example.com/mcp"},
	}, false)
	if err != nil {
		t.Fatalf("set failed: %v", err)
	}

	data, err := doc.MarshalIndent()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("unmarshal root failed: %v", err)
	}
	if _, ok := root["theme"]; !ok {
		t.Fatalf("expected theme to be preserved: %s", string(data))
	}

	var servers map[string]json.RawMessage
	if err := json.Unmarshal(root["mcpServers"], &servers); err != nil {
		t.Fatalf("unmarshal servers failed: %v", err)
	}
	if _, ok := servers["existing"]; !ok {
		t.Fatalf("expected existing server to be preserved: %s", string(data))
	}
	if _, ok := servers["new"]; !ok {
		t.Fatalf("expected new server to be added: %s", string(data))
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected invalid JSON to fail")
	}
}

func TestLoadRejectsNonObjectRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	if err := os.WriteFile(path, []byte("null"), 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected non-object root to fail")
	}
}
