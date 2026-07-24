package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ankit-lilly/mcp-bridge/internal/config"
	"github.com/ankit-lilly/mcp-bridge/internal/logx"
	"github.com/ankit-lilly/mcp-bridge/internal/remote"
	"github.com/ankit-lilly/mcp-bridge/internal/store"
	"github.com/ankit-lilly/mcp-bridge/internal/version"
)

type session struct {
	connector *remote.HTTPConnector
	logger    *slog.Logger
	cancel    context.CancelFunc
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
}

func bootstrap(ctx context.Context, cfg *config.BridgeConfig, stdin io.Reader, stdout, stderr io.Writer) (context.Context, *session, error) {
	logger := logx.New(logx.Config{Debug: cfg.Debug, Silent: cfg.Silent, Output: stderr})

	storeDir, err := store.DefaultDir()
	if err != nil {
		return nil, nil, fmt.Errorf("config dir: %w", err)
	}
	s, err := store.New(storeDir)
	if err != nil {
		return nil, nil, fmt.Errorf("store init: %w", err)
	}

	configKey := cfg.Hash()
	logger.Debug("config hash computed", "key", configKey)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)

	httpClient := buildHTTPClient(cfg)

	headers := make(map[string]string, len(cfg.Headers))
	for _, h := range cfg.Headers {
		headers[h.Key] = h.Value
	}

	authMgr, authz := buildAuth(cfg, s, configKey, httpClient, logger, stderr)

	connector := remote.NewHTTPConnector(remote.HTTPConnectorConfig{
		Client:      httpClient,
		URL:         cfg.ServerURL,
		Headers:     headers,
		TokenSource: authMgr,
		Authorizer:  authz,
		Logger:      logger,
		UserAgent:   "mcp-bridge/" + version.Version,
	})

	sess := &session{
		connector: connector,
		logger:    logger,
		cancel:    cancel,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
	}
	return ctx, sess, nil
}

func buildHTTPClient(cfg *config.BridgeConfig) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	if cfg.EnableProxy {
		transport.Proxy = http.ProxyFromEnvironment
	}

	return &http.Client{Transport: transport}
}
