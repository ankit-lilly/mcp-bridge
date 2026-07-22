package claudeconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// ServerConfig is a Claude Desktop mcpServers entry.
type ServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// MergeResult describes the result of inserting or replacing an mcpServers entry.
type MergeResult string

const (
	MergeCreated   MergeResult = "created"
	MergeUpdated   MergeResult = "updated"
	MergeUnchanged MergeResult = "unchanged"
)

// Document represents a Claude Desktop config file while preserving unknown top-level keys.
type Document struct {
	topLevel map[string]json.RawMessage
	servers  map[string]json.RawMessage
}

// New returns an empty Claude Desktop config document.
func New() *Document {
	return &Document{
		topLevel: make(map[string]json.RawMessage),
		servers:  make(map[string]json.RawMessage),
	}
}

// Load reads an existing Claude Desktop config file, or returns an empty document if it does not exist.
func Load(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return New(), nil
		}
		return nil, err
	}

	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(data, &topLevel); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filepath.Base(path), err)
	}
	if topLevel == nil {
		return nil, fmt.Errorf("%s must contain a JSON object", filepath.Base(path))
	}

	doc := &Document{
		topLevel: topLevel,
		servers:  make(map[string]json.RawMessage),
	}

	rawServers, ok := topLevel["mcpServers"]
	if !ok || isJSONNull(rawServers) {
		return doc, nil
	}

	if err := json.Unmarshal(rawServers, &doc.servers); err != nil {
		return nil, fmt.Errorf(`parsing %s field "mcpServers": %w`, filepath.Base(path), err)
	}
	if doc.servers == nil {
		doc.servers = make(map[string]json.RawMessage)
	}

	return doc, nil
}

// SetServer merges an mcpServers entry into the document.
func (d *Document) SetServer(name string, server ServerConfig, force bool) (MergeResult, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("Claude Desktop server name is required")
	}
	if strings.TrimSpace(server.Command) == "" {
		return "", errors.New("Claude Desktop server command is required")
	}
	if d.servers == nil {
		d.servers = make(map[string]json.RawMessage)
	}

	newEntry, err := json.Marshal(server)
	if err != nil {
		return "", err
	}

	if existing, ok := d.servers[name]; ok {
		equal, err := jsonSemanticallyEqual(existing, newEntry)
		if err != nil {
			return "", fmt.Errorf("comparing existing server %q: %w", name, err)
		}
		if equal {
			return MergeUnchanged, nil
		}
		if !force {
			return "", fmt.Errorf("Claude Desktop config already contains server %q (use --force to replace it)", name)
		}
		d.servers[name] = newEntry
		return MergeUpdated, nil
	}

	d.servers[name] = newEntry
	return MergeCreated, nil
}

// MarshalIndent encodes the document with stable formatting.
func (d *Document) MarshalIndent() ([]byte, error) {
	topLevel := make(map[string]json.RawMessage, len(d.topLevel)+1)
	for key, value := range d.topLevel {
		if key == "mcpServers" {
			continue
		}
		topLevel[key] = value
	}

	servers := d.servers
	if servers == nil {
		servers = make(map[string]json.RawMessage)
	}

	serverJSON, err := json.Marshal(servers)
	if err != nil {
		return nil, err
	}
	topLevel["mcpServers"] = json.RawMessage(serverJSON)

	data, err := json.MarshalIndent(topLevel, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Write persists the document atomically.
func (d *Document) Write(path string) error {
	data, err := d.MarshalIndent()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating Claude Desktop config directory: %w", err)
	}
	return atomicWrite(path, data, 0600)
}

func isJSONNull(raw []byte) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func jsonSemanticallyEqual(a, b []byte) (bool, error) {
	var left any
	if err := json.Unmarshal(a, &left); err != nil {
		return false, err
	}
	var right any
	if err := json.Unmarshal(b, &right); err != nil {
		return false, err
	}
	return reflect.DeepEqual(left, right), nil
}

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
