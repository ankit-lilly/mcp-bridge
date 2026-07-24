package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ankit-lilly/mcp-bridge/internal/config"
	"github.com/ankit-lilly/mcp-bridge/internal/fsutil"
)

func TestConfigHash_Stable(t *testing.T) {
	cfg := &config.BridgeConfig{
		ServerURL: "https://example.com/mcp",
		Resource:  "https://api.example.com",
		Headers:   []config.Header{{Key: "X-A", Value: "1"}, {Key: "X-B", Value: "2"}},
	}
	h1 := cfg.Hash()
	h2 := cfg.Hash()
	if h1 != h2 {
		t.Fatal("hash must be stable across calls")
	}
	if len(h1) != 32 {
		t.Fatalf("expected 32 char hex, got %d chars", len(h1))
	}
}

func TestConfigHash_OrderIndependent(t *testing.T) {
	cfg1 := &config.BridgeConfig{
		ServerURL: "https://example.com/mcp",
		Headers:   []config.Header{{Key: "X-A", Value: "1"}, {Key: "X-B", Value: "2"}},
	}
	cfg2 := &config.BridgeConfig{
		ServerURL: "https://example.com/mcp",
		Headers:   []config.Header{{Key: "X-B", Value: "2"}, {Key: "X-A", Value: "1"}},
	}
	if cfg1.Hash() != cfg2.Hash() {
		t.Fatal("hash must be independent of header order")
	}
}

func TestConfigHash_ChangeSensitive(t *testing.T) {
	cfg1 := &config.BridgeConfig{ServerURL: "https://example.com/mcp"}
	cfg2 := &config.BridgeConfig{ServerURL: "https://example.com/mcp", Resource: "urn:api"}
	if cfg1.Hash() == cfg2.Hash() {
		t.Fatal("different resource must yield different hash")
	}
}

func TestStore_TokensRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("store creation failed: %v", err)
	}

	ctx := context.Background()
	key := "testkey"

	// Not found initially
	_, err = s.LoadTokens(ctx, key)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Save and reload
	tok := &TokenSet{
		AccessToken:  "access123",
		RefreshToken: "refresh456",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour).Truncate(time.Second),
	}
	if err := s.SaveTokens(ctx, key, tok); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := s.LoadTokens(ctx, key)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.AccessToken != tok.AccessToken {
		t.Fatalf("access token mismatch: %s != %s", loaded.AccessToken, tok.AccessToken)
	}
	if loaded.RefreshToken != tok.RefreshToken {
		t.Fatalf("refresh token mismatch")
	}

	// Check file permissions
	path := filepath.Join(dir, key+".tokens.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected 0600 perms, got %o", info.Mode().Perm())
	}
}

func TestStore_ClientRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir)
	ctx := context.Background()

	info := &ClientInfo{ClientID: "my-client", ClientSecret: "secret"}
	if err := s.SaveClient(ctx, "key1", info); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	loaded, err := s.LoadClient(ctx, "key1")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.ClientID != "my-client" {
		t.Fatalf("client ID mismatch: %s", loaded.ClientID)
	}
}

func TestAtomicWrite_SecurePerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	if err := fsutil.AtomicWrite(path, []byte("data"), 0600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected 0600, got %o", info.Mode().Perm())
	}
}

func TestDefaultDir_PrefersBridgeEnvVar(t *testing.T) {
	t.Setenv("MCP_BRIDGE_CONFIG_DIR", "/tmp/mcp-bridge-test")

	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/tmp/mcp-bridge-test" {
		t.Fatalf("expected bridge env var to win, got %q", dir)
	}
}
