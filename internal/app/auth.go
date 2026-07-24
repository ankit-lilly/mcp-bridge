package app

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/ankit-lilly/mcp-bridge/internal/config"
	"github.com/ankit-lilly/mcp-bridge/internal/oauth"
	"github.com/ankit-lilly/mcp-bridge/internal/remote"
	"github.com/ankit-lilly/mcp-bridge/internal/store"
)

const (
	derivedCallbackPortBase = 10000
	derivedCallbackPortSpan = 50000
)

func buildAuth(cfg *config.BridgeConfig, s *store.Store, configKey string, httpClient *http.Client, logger *slog.Logger, stderr io.Writer) (*oauth.Manager, remote.Authorizer) {
	authMgr := oauth.NewManager(oauth.ManagerConfig{
		ServerURL:        cfg.ServerURL,
		Resource:         cfg.Resource,
		Host:             callbackHost(cfg.Host),
		CallbackPort:     callbackPort(cfg, configKey),
		AuthTimeout:      cfg.AuthTimeout,
		Logger:           logger,
		Client:           httpClient,
		Stderr:           stderr,
		ClientMetadata:   cfg.StaticOAuthClientMetadata,
		ClientInfo:       loadPersistedClientInfo(cfg, s, configKey, logger),
		Token:            loadPersistedToken(s, configKey, logger),
		StaticClientInfo: cfg.StaticOAuthClientInfo != nil,
		OnTokenChange: func(ctx context.Context, tok *oauth.Token) error {
			return s.SaveTokens(ctx, configKey, &store.TokenSet{
				AccessToken:  tok.AccessToken,
				RefreshToken: tok.RefreshToken,
				TokenType:    tok.TokenType,
				Expiry:       tok.ExpiresAt,
				Scope:        tok.Scope,
			})
		},
	})

	return authMgr, &authorizer{
		mgr:       authMgr,
		store:     s,
		configKey: configKey,
		logger:    logger,
	}
}

type authorizer struct {
	mgr       *oauth.Manager
	store     *store.Store
	configKey string
	logger    *slog.Logger
	mu        sync.Mutex
}

func (a *authorizer) EnsureAuthorized(ctx context.Context, challenge *remote.AuthRequiredError) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	statusCode := 0
	phase := ""
	var challengeInfo *oauth.ChallengeInfo

	if challenge != nil {
		statusCode = challenge.StatusCode
		phase = challenge.Phase
		if challenge.Headers != nil {
			challengeInfo = oauth.ParseWWWAuthenticate(challenge.Headers)
		}
	}

	a.logger.Info("authorization required", "status", statusCode, "phase", phase)

	_, clientInfo, err := a.mgr.Authorize(ctx, challengeInfo)
	if err != nil {
		return err
	}
	if err := a.persistClientInfo(ctx, clientInfo); err != nil {
		return err
	}

	a.logger.Info("authorization completed successfully")
	return nil
}

func (a *authorizer) persistClientInfo(ctx context.Context, clientInfo *oauth.ClientRegistration) error {
	if clientInfo == nil {
		return nil
	}
	if err := a.store.SaveClient(ctx, a.configKey, &store.ClientInfo{
		ClientID:     clientInfo.ClientID,
		ClientSecret: clientInfo.ClientSecret,
	}); err != nil {
		a.logger.Warn("client persistence failed", "err", err)
		return fmt.Errorf("persisting client info: %w", err)
	}
	return nil
}

func callbackHost(host string) string {
	if host != "" {
		return host
	}
	return "127.0.0.1"
}

func callbackPort(cfg *config.BridgeConfig, configKey string) int {
	if cfg.CallbackPort != 0 {
		return cfg.CallbackPort
	}
	return deriveCallbackPort(configKey)
}

func deriveCallbackPort(configKey string) int {
	h := sha256.Sum256([]byte(configKey))
	n := binary.BigEndian.Uint32(h[:4])
	return int(n%derivedCallbackPortSpan) + derivedCallbackPortBase
}

func loadPersistedToken(s *store.Store, configKey string, logger *slog.Logger) *oauth.Token {
	tok, err := s.LoadTokens(context.Background(), configKey)
	if err != nil {
		return nil
	}
	logger.Debug("loaded persisted tokens")
	return &oauth.Token{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenType:    tok.TokenType,
		ExpiresAt:    tok.Expiry,
	}
}

func loadPersistedClientInfo(cfg *config.BridgeConfig, s *store.Store, configKey string, logger *slog.Logger) *oauth.ClientRegistration {
	if cfg.StaticOAuthClientInfo != nil {
		var clientInfo oauth.ClientRegistration
		if err := json.Unmarshal(cfg.StaticOAuthClientInfo, &clientInfo); err == nil {
			return &clientInfo
		}
		return nil
	}

	clientInfo, err := s.LoadClient(context.Background(), configKey)
	if err != nil {
		return nil
	}
	logger.Debug("loaded persisted client info")
	return &oauth.ClientRegistration{
		ClientID:     clientInfo.ClientID,
		ClientSecret: clientInfo.ClientSecret,
	}
}
