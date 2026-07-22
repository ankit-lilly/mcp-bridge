package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	dir string
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating config dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Dir() string { return s.dir }

func DefaultDir() (string, error) {
	if env := os.Getenv("MCP_BRIDGE_CONFIG_DIR"); env != "" {
		return env, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determining config dir: %w", err)
	}
	return filepath.Join(base, "mcp-bridge"), nil
}

// TokenSet holds persisted OAuth tokens.
type TokenSet struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Expiry       time.Time `json:"expiry,omitzero"`
	Scope        string    `json:"scope,omitempty"`
}

// ClientInfo holds persisted client registration or static client data.
type ClientInfo struct {
	ClientID     string          `json:"client_id"`
	ClientSecret string          `json:"client_secret,omitempty"`
	Raw          json.RawMessage `json:"raw,omitempty"`
}

// LoadTokens reads persisted tokens for the given key.
func (s *Store) LoadTokens(_ context.Context, key string) (*TokenSet, error) {
	return loadJSON[TokenSet](s.path(key, "tokens.json"))
}

// SaveTokens persists tokens atomically.
func (s *Store) SaveTokens(_ context.Context, key string, tok *TokenSet) error {
	return saveJSON(s.path(key, "tokens.json"), tok)
}

func (s *Store) LoadClient(_ context.Context, key string) (*ClientInfo, error) {
	return loadJSON[ClientInfo](s.path(key, "client.json"))
}

func (s *Store) SaveClient(_ context.Context, key string, info *ClientInfo) error {
	return saveJSON(s.path(key, "client.json"), info)
}

func (s *Store) path(key, suffix string) string {
	return filepath.Join(s.dir, key+"."+suffix)
}

func loadJSON[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filepath.Base(path), err)
	}
	return &v, nil
}

func saveJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return atomicWrite(path, data, 0600)
}

// atomicWrite writes data to a temp file and renames it into place.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
