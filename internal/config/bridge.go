package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Header struct {
	Key   string
	Value string
}

type BridgeConfig struct {
	ServerURL    string
	CallbackPort int
	Headers      []Header
	Host         string
	AllowHTTP    bool
	Debug        bool
	Silent       bool
	EnableProxy  bool
	Resource     string
	AuthTimeout  time.Duration

	StaticOAuthClientMetadata json.RawMessage
	StaticOAuthClientInfo     json.RawMessage
}

// Validate checks the config for internal consistency.
func (c *BridgeConfig) Validate() error {
	if c.ServerURL == "" {
		return errors.New("server URL is required")
	}

	u, err := url.Parse(c.ServerURL)
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}

	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("server URL must have scheme and host: %s", c.ServerURL)
	}

	if !c.AllowHTTP && u.Scheme == "http" && !isLoopback(u.Hostname()) {
		return fmt.Errorf("HTTP not allowed for non-loopback host %q (use --allow-http to override)", u.Hostname())
	}

	if c.Debug && c.Silent {
		return errors.New("--debug and --silent are mutually exclusive")
	}

	return nil
}

// Hash computes a stable identity hash from the configuration inputs
// that determine which persisted state (tokens, client info, etc.) to use.
func (c *BridgeConfig) Hash() string {
	var b strings.Builder
	b.WriteString(c.ServerURL)
	b.WriteByte('|')
	b.WriteString(c.Resource)
	b.WriteByte('|')
	b.WriteString(normalizedHeadersJSON(c.Headers))

	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:16]) // 32-char hex
}

func normalizedHeadersJSON(headers []Header) string {
	if len(headers) == 0 {
		return "{}"
	}
	m := make(map[string]string, len(headers))
	for _, h := range headers {
		m[h.Key] = h.Value
	}
	data, _ := json.Marshal(m)
	return string(data)
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}
