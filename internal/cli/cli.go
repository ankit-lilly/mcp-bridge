package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ankit-lilly/mcp-bridge/internal/config"
	"github.com/spf13/pflag"
)

const unsupportedTransportMessage = "--transport is not supported; mcp-bridge uses streamable HTTP"

func parseHeader(s string) (config.Header, error) {
	key, value, ok := strings.Cut(s, ":")
	if !ok {
		return config.Header{}, fmt.Errorf("header must be key:value, got %q", s)
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		return config.Header{}, fmt.Errorf("header key must not be empty in %q", s)
	}
	return config.Header{Key: key, Value: expandEnv(value)}, nil
}

func expandEnv(s string) string {
	return os.Expand(s, os.Getenv)
}

type BridgeOptions struct {
	Headers          []string
	Host             string
	CallbackPort     int
	LegacyTransport  string
	AllowHTTP        bool
	Debug            bool
	Silent           bool
	EnableProxy      bool
	Resource         string
	AuthTimeout      time.Duration
	StaticMetadata   string
	StaticClientInfo string
}

func NewBridgeOptions() *BridgeOptions {
	return &BridgeOptions{
		AuthTimeout: 2 * time.Minute,
	}
}

func (o *BridgeOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&o.Headers, "header", nil, "Add outbound header (key:value); repeatable")
	fs.StringVar(&o.Host, "host", "", "Hostname for OAuth callback URL")
	fs.IntVar(&o.CallbackPort, "callback-port", 0, "Preferred local callback port")
	fs.StringVar(&o.LegacyTransport, "transport", "", "Deprecated transport selector")
	if transportFlag := fs.Lookup("transport"); transportFlag != nil {
		transportFlag.NoOptDefVal = "legacy"
		_ = fs.MarkHidden("transport")
	}
	fs.BoolVar(&o.AllowHTTP, "allow-http", false, "Allow non-HTTPS remote URLs")
	fs.BoolVar(&o.Debug, "debug", false, "Enable verbose sanitized debug logging")
	fs.BoolVar(&o.Silent, "silent", false, "Suppress normal stderr logs")
	fs.BoolVar(&o.EnableProxy, "enable-proxy", false, "Honor HTTP_PROXY/HTTPS_PROXY/NO_PROXY")
	fs.StringVar(&o.Resource, "resource", "", "Resource parameter for authorization")
	fs.DurationVar(&o.AuthTimeout, "auth-timeout", 2*time.Minute, "Timeout for browser callback (for example 30s or 2m)")
	fs.StringVar(&o.StaticMetadata, "static-oauth-client-metadata", "", "Static client metadata (JSON or @file)")
	fs.StringVar(&o.StaticClientInfo, "static-oauth-client-info", "", "Pre-registered client info (JSON or @file)")
}

func (o *BridgeOptions) BuildConfig(positionals []string) (*config.BridgeConfig, error) {
	if err := validateBridgePositionals(positionals); err != nil {
		return nil, err
	}
	if o.LegacyTransport != "" {
		return nil, errors.New(unsupportedTransportMessage)
	}

	cfg := &config.BridgeConfig{
		ServerURL:    positionals[0],
		CallbackPort: o.CallbackPort,
		Host:         o.Host,
		AllowHTTP:    o.AllowHTTP,
		Debug:        o.Debug,
		Silent:       o.Silent,
		EnableProxy:  o.EnableProxy,
		Resource:     o.Resource,
		AuthTimeout:  o.AuthTimeout,
	}

	for _, header := range o.Headers {
		hdr, err := parseHeader(header)
		if err != nil {
			return nil, err
		}
		cfg.Headers = append(cfg.Headers, hdr)
	}

	if o.StaticMetadata != "" {
		data, err := loadJSONOrFile(o.StaticMetadata)
		if err != nil {
			return nil, fmt.Errorf("--static-oauth-client-metadata: %w", err)
		}
		cfg.StaticOAuthClientMetadata = data
	}

	if o.StaticClientInfo != "" {
		data, err := loadJSONOrFile(o.StaticClientInfo)
		if err != nil {
			return nil, fmt.Errorf("--static-oauth-client-info: %w", err)
		}
		cfg.StaticOAuthClientInfo = data
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseBridgeArgs(args []string) (*config.BridgeConfig, error) {
	opts := NewBridgeOptions()
	fs := pflag.NewFlagSet("bridge", pflag.ContinueOnError)
	fs.SetInterspersed(false)
	opts.AddFlags(fs)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return opts.BuildConfig(fs.Args())
}

func validateBridgePositionals(positionals []string) error {
	switch len(positionals) {
	case 0:
		return errors.New("server URL is required")
	case 1:
		return nil
	default:
		return fmt.Errorf("bridge accepts exactly one server URL, got %d arguments", len(positionals))
	}
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
