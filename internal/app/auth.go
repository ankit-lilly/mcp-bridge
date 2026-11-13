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
	"time"

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
	mgr            *oauth.Manager
	store          *store.Store
	configKey      string
	logger         *slog.Logger
	authorize      func(context.Context, *oauth.ChallengeInfo) (*oauth.Token, *oauth.ClientRegistration, error)
	mu             sync.Mutex
	flight         *authorizationFlight
	lastAuthorized [32]byte
	authorizedAt   time.Time
	lastFailed     [32]byte
	lastError      error
	failedAt       time.Time
}

type authorizationFlight struct {
	done       chan struct{}
	credential [32]byte
	err        error
}

func (a *authorizer) EnsureAuthorized(ctx context.Context, challenge *remote.AuthRequiredError) error {
	if challenge == nil {
		return fmt.Errorf("missing authorization challenge")
	}

	challengeInfo := oauth.ParseWWWAuthenticate(challenge.Headers)
	if challenge.StatusCode == http.StatusForbidden && (challengeInfo == nil || challengeInfo.Error != "insufficient_scope") {
		return remote.ErrForbidden
	}

	a.mu.Lock()
	if flight := a.flight; flight != nil {
		a.mu.Unlock()
		select {
		case <-flight.done:
			if flight.err != nil && flight.credential == challenge.SentCredential {
				return flight.err
			}
			return a.EnsureAuthorized(ctx, challenge)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		a.mu.Unlock()
		return err
	}

	current := a.mgr.CurrentAccessToken()
	currentCredential := ""
	if current != "" {
		currentCredential = "Bearer " + current
	}
	fingerprint := remote.CredentialFingerprint(currentCredential)
	// A previously issued token changed underneath this request; retry rather
	// than launching another browser. For requests that carried no credential,
	// require evidence that this process completed authorization first.
	if current != "" && fingerprint != challenge.SentCredential && challenge.SentCredential != ([32]byte{}) && challenge.SentCredential != remote.CredentialFingerprint("") {
		a.mu.Unlock()
		return nil // another request already acquired a new token
	}
	if current != "" && challenge.SentCredential == remote.CredentialFingerprint("") && a.lastAuthorized == fingerprint && time.Since(a.authorizedAt) < 5*time.Second {
		a.mu.Unlock()
		return nil // the initial no-token challenge has already been handled
	}
	if challenge.SentCredential != ([32]byte{}) && fingerprint == challenge.SentCredential && fingerprint == a.lastAuthorized && time.Since(a.authorizedAt) < 5*time.Second {
		a.mu.Unlock()
		return fmt.Errorf("server rejected the newly authorized token: %w", remote.ErrUnauthorized)
	}
	if challenge.SentCredential != ([32]byte{}) && fingerprint == challenge.SentCredential && fingerprint == a.lastFailed && a.lastError != nil && time.Since(a.failedAt) < 5*time.Second {
		err := a.lastError
		a.mu.Unlock()
		return err
	}

	flight := &authorizationFlight{done: make(chan struct{}), credential: challenge.SentCredential}
	a.flight = flight
	a.mu.Unlock()

	a.logger.Info("authorization required", "status", challenge.StatusCode, "phase", challenge.Phase)
	var clientInfo *oauth.ClientRegistration
	var err error
	if a.authorize != nil {
		_, clientInfo, err = a.authorize(ctx, challengeInfo)
	} else {
		_, clientInfo, err = a.mgr.Authorize(ctx, challengeInfo)
	}
	if err == nil {
		err = a.persistClientInfo(ctx, clientInfo)
	}

	a.mu.Lock()
	if err == nil {
		current = a.mgr.CurrentAccessToken()
		if current == "" {
			currentCredential = ""
		} else {
			currentCredential = "Bearer " + current
		}
		a.lastAuthorized = remote.CredentialFingerprint(currentCredential)
		a.authorizedAt = time.Now()
		a.lastError = nil
		a.logger.Info("authorization completed successfully")
	} else {
		a.lastFailed = fingerprint
		a.lastError = err
		a.failedAt = time.Now()
	}
	flight.err = err
	a.flight = nil
	close(flight.done)
	a.mu.Unlock()
	return err
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
