package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
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

type Header struct {
	Key   string
	Value string
}

type repeatedFlag []string

func (r *repeatedFlag) String() string { return strings.Join(*r, ", ") }
func (r *repeatedFlag) Set(s string) error {
	*r = append(*r, s)
	return nil
}

var bridgeFlagNames = newBridgeFlagNames()

func Parse(args []string) (*Config, error) {
	return ParseBridge(args)
}

func ParseBridge(args []string) (*Config, error) {
	return parseBridgeCommand("mcp-bridge", bridgeUsageLine, args)
}

func ParseInspect(args []string) (*Config, error) {
	return parseBridgeCommand("mcp-bridge inspect", inspectUsageLine, args)
}

// Validate checks the config for internal consistency.
func (c *Config) Validate() error {
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
func (c *Config) Hash() string {
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

func ParseHeader(s string) (Header, error) {
	key, value, ok := strings.Cut(s, ":")
	if !ok {
		return Header{}, fmt.Errorf("header must be key:value, got %q", s)
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		return Header{}, fmt.Errorf("header key must not be empty in %q", s)
	}
	return Header{Key: key, Value: ExpandEnv(value)}, nil
}

func ExpandEnv(s string) string {
	return os.Expand(s, os.Getenv)
}

type bridgeFlagValues struct {
	headers          repeatedFlag
	host             string
	callbackPort     int
	allowHTTP        bool
	debug            bool
	silent           bool
	enableProxy      bool
	resource         string
	authTimeout      int
	staticMetadata   string
	staticClientInfo string
}

func parseBridgeCommand(name, usageLine string, args []string) (*Config, error) {
	if err := rejectRemovedFlags(args); err != nil {
		return nil, err
	}

	fs, values := newBridgeFlagSet(name, usageLine)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg, err := values.buildConfig(fs.Args())
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func newBridgeFlagSet(name, usageLine string) (*flag.FlagSet, *bridgeFlagValues) {
	values := &bridgeFlagValues{}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	setFlagSetUsage(fs, usageLine)
	registerBridgeFlags(fs, values)
	return fs, values
}

func newBridgeFlagNames() map[string]struct{} {
	fs, _ := newBridgeFlagSet("bridge-flags", bridgeUsageLine)
	names := make(map[string]struct{})
	fs.VisitAll(func(f *flag.Flag) {
		names[f.Name] = struct{}{}
	})
	return names
}

func registerBridgeFlags(fs *flag.FlagSet, values *bridgeFlagValues) {
	fs.Var(&values.headers, "header", "Add outbound header (key:value); repeatable")
	fs.StringVar(&values.host, "host", "", "Hostname for OAuth callback URL")
	fs.IntVar(&values.callbackPort, "callback-port", 0, "Preferred local callback port")
	fs.BoolVar(&values.allowHTTP, "allow-http", false, "Allow non-HTTPS remote URLs")
	fs.BoolVar(&values.debug, "debug", false, "Enable verbose sanitized debug logging")
	fs.BoolVar(&values.silent, "silent", false, "Suppress normal stderr logs")
	fs.BoolVar(&values.enableProxy, "enable-proxy", false, "Honor HTTP_PROXY/HTTPS_PROXY/NO_PROXY")
	fs.StringVar(&values.resource, "resource", "", "Resource parameter for authorization")
	fs.IntVar(&values.authTimeout, "auth-timeout", 120, "Timeout in seconds for browser callback")
	fs.StringVar(&values.staticMetadata, "static-oauth-client-metadata", "", "Static client metadata (JSON or @file)")
	fs.StringVar(&values.staticClientInfo, "static-oauth-client-info", "", "Pre-registered client info (JSON or @file)")
}

func (v *bridgeFlagValues) buildConfig(positional []string) (*Config, error) {
	if len(positional) < 1 {
		return nil, errors.New("server URL is required")
	}

	callbackPort := v.callbackPort
	if len(positional) > 1 {
		port, err := strconv.Atoi(positional[1])
		if err != nil {
			return nil, fmt.Errorf("invalid callback port %q: %w", positional[1], err)
		}
		if callbackPort == 0 {
			callbackPort = port
		}
	}

	cfg := &Config{
		ServerURL:    positional[0],
		CallbackPort: callbackPort,
		Host:         v.host,
		AllowHTTP:    v.allowHTTP,
		Debug:        v.debug,
		Silent:       v.silent,
		EnableProxy:  v.enableProxy,
		Resource:     v.resource,
		AuthTimeout:  time.Duration(v.authTimeout) * time.Second,
	}

	for _, header := range v.headers {
		hdr, err := ParseHeader(header)
		if err != nil {
			return nil, err
		}
		cfg.Headers = append(cfg.Headers, hdr)
	}

	if v.staticMetadata != "" {
		data, err := loadJSONOrFile(v.staticMetadata)
		if err != nil {
			return nil, fmt.Errorf("--static-oauth-client-metadata: %w", err)
		}
		cfg.StaticOAuthClientMetadata = data
	}

	if v.staticClientInfo != "" {
		data, err := loadJSONOrFile(v.staticClientInfo)
		if err != nil {
			return nil, fmt.Errorf("--static-oauth-client-info: %w", err)
		}
		cfg.StaticOAuthClientInfo = data
	}

	return cfg, nil
}

func rejectRemovedFlags(args []string) error {
	for _, arg := range args {
		if arg == "--transport" || strings.HasPrefix(arg, "--transport=") {
			return errors.New("--transport is not supported; mcp-bridge uses streamable HTTP")
		}
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

func loadJSONOrFile(s string) (json.RawMessage, error) {
	if after, ok := strings.CutPrefix(s, "@"); ok {
		data, err := os.ReadFile(after)
		if err != nil {
			return nil, fmt.Errorf("reading file %s: %w", after, err)
		}
		if !json.Valid(data) {
			return nil, fmt.Errorf("file %s does not contain valid JSON", after)
		}
		return json.RawMessage(data), nil
	}
	if !json.Valid([]byte(s)) {
		return nil, fmt.Errorf("invalid JSON: %s", s)
	}
	return json.RawMessage(s), nil
}
