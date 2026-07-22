package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParse_MinimalValid(t *testing.T) {
	cfg, err := Parse([]string{"https://example.com/mcp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ServerURL != "https://example.com/mcp" {
		t.Fatalf("unexpected server URL: %s", cfg.ServerURL)
	}
	if cfg.AuthTimeout != 120*time.Second {
		t.Fatalf("expected default auth timeout 120s, got %v", cfg.AuthTimeout)
	}
}

func TestParse_AllFlags(t *testing.T) {
	args := []string{
		"--header", "X-Api-Key:my-key",
		"--header", "X-Custom:val",
		"--host", "myhost.local",
		"--callback-port", "9999",
		"--allow-http",
		"--debug",
		"--enable-proxy",
		"--resource", "https://api.example.com",
		"--auth-timeout", "60",
		"https://example.com/mcp",
	}
	cfg, err := Parse(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Headers) != 2 {
		t.Fatalf("expected 2 headers, got %d", len(cfg.Headers))
	}
	if cfg.Host != "myhost.local" {
		t.Fatalf("unexpected host: %s", cfg.Host)
	}
	if cfg.CallbackPort != 9999 {
		t.Fatalf("expected port 9999, got %d", cfg.CallbackPort)
	}
	if !cfg.AllowHTTP {
		t.Fatal("expected allow-http to be set")
	}
	if !cfg.Debug {
		t.Fatal("expected debug to be set")
	}
	if cfg.Resource != "https://api.example.com" {
		t.Fatalf("unexpected resource: %s", cfg.Resource)
	}
	if cfg.AuthTimeout != 60*time.Second {
		t.Fatalf("expected 60s timeout, got %v", cfg.AuthTimeout)
	}
}

func TestParse_PositionalCallbackPort(t *testing.T) {
	cfg, err := Parse([]string{"https://example.com/mcp", "8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CallbackPort != 8080 {
		t.Fatalf("expected port 8080, got %d", cfg.CallbackPort)
	}
}

func TestParse_MissingURL(t *testing.T) {
	_, err := Parse([]string{})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestParse_RemovedTransport(t *testing.T) {
	for _, args := range [][]string{
		{"--transport", "http-first", "https://example.com/mcp"},
		{"--transport=http-first", "https://example.com/mcp"},
	} {
		_, err := Parse(args)
		if err == nil {
			t.Fatalf("expected error for removed transport flag: %v", args)
		}
	}
}

func TestParse_HTTPNotAllowed(t *testing.T) {
	_, err := Parse([]string{"http://remote.example.com/mcp"})
	if err == nil {
		t.Fatal("expected error for HTTP on non-loopback host")
	}
}

func TestParse_HTTPAllowedForLoopback(t *testing.T) {
	_, err := Parse([]string{"http://localhost/mcp"})
	if err != nil {
		t.Fatalf("loopback HTTP should be allowed: %v", err)
	}
}

func TestParse_HTTPAllowedWithFlag(t *testing.T) {
	_, err := Parse([]string{"--allow-http", "http://remote.example.com/mcp"})
	if err != nil {
		t.Fatalf("--allow-http should permit HTTP: %v", err)
	}
}

func TestParse_DebugAndSilentConflict(t *testing.T) {
	_, err := Parse([]string{"--debug", "--silent", "https://example.com/mcp"})
	if err == nil {
		t.Fatal("expected error for debug+silent conflict")
	}
}

func TestParseHeader(t *testing.T) {
	h, err := ParseHeader("Authorization:Bearer token123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Key != "Authorization" {
		t.Fatalf("unexpected key: %s", h.Key)
	}
	if h.Value != "Bearer token123" {
		t.Fatalf("unexpected value: %s", h.Value)
	}
}

func TestParseHeader_Invalid(t *testing.T) {
	_, err := ParseHeader("no-colon")
	if err == nil {
		t.Fatal("expected error for missing colon")
	}
}

func TestExpandEnv(t *testing.T) {
	os.Setenv("TEST_MCP_VAR", "expanded")
	defer os.Unsetenv("TEST_MCP_VAR")
	result := ExpandEnv("prefix-${TEST_MCP_VAR}-suffix")
	if result != "prefix-expanded-suffix" {
		t.Fatalf("unexpected expansion: %s", result)
	}
}

func TestLoadJSONOrFile(t *testing.T) {
	data, err := loadJSONOrFile(`{"client_id":"abc"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"client_id":"abc"}` {
		t.Fatalf("unexpected data: %s", string(data))
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "client.json")
	os.WriteFile(path, []byte(`{"id":"from-file"}`), 0644)
	data, err = loadJSONOrFile("@" + path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"id":"from-file"}` {
		t.Fatalf("unexpected data: %s", string(data))
	}
}

func TestLoadJSONOrFile_InvalidJSON(t *testing.T) {
	_, err := loadJSONOrFile("not-json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
