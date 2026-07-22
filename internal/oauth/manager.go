package oauth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const defaultCallbackPort = 19876

type Manager struct {
	serverURL    string
	resource     string
	host         string
	callbackPort int
	authTimeout  time.Duration
	logger       *slog.Logger
	client       *http.Client
	stderr       io.Writer

	stateMu       sync.Mutex
	token         *Token
	clientInfo    *ClientRegistration
	refreshMu     sync.Mutex
	refreshing    bool
	refreshDone   chan struct{}
	refreshErr    error
	onTokenChange func(context.Context, *Token) error

	// static client info
	clientMetadata json.RawMessage

	// discovered metadata
	authzEndpoint string
	tokenEndpoint string
	regEndpoint   string
	resolvedScope string
}

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`
	Scope        string    `json:"scope,omitempty"`
}

// ClientRegistration holds client registration data.
type ClientRegistration struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
}

// ManagerConfig configures the OAuth Manager.
type ManagerConfig struct {
	ServerURL      string
	Resource       string
	Host           string
	CallbackPort   int
	AuthTimeout    time.Duration
	Logger         *slog.Logger
	Client         *http.Client
	Stderr         io.Writer
	ClientMetadata json.RawMessage
	ClientInfo     *ClientRegistration
	Token          *Token
	OnTokenChange  func(context.Context, *Token) error
}

// NewManager creates a new OAuth Manager.
func NewManager(cfg ManagerConfig) *Manager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	return &Manager{
		serverURL:      cfg.ServerURL,
		resource:       cfg.Resource,
		host:           cfg.Host,
		callbackPort:   cfg.CallbackPort,
		authTimeout:    cfg.AuthTimeout,
		logger:         logger,
		client:         client,
		stderr:         stderr,
		token:          cfg.Token,
		clientMetadata: cfg.ClientMetadata,
		clientInfo:     cfg.ClientInfo,
		onTokenChange:  cfg.OnTokenChange,
	}
}

func (m *Manager) SetToken(tok *Token) {
	m.stateMu.Lock()
	m.token = tok
	m.stateMu.Unlock()
}

func (m *Manager) SetClientInfo(ci *ClientRegistration) {
	m.stateMu.Lock()
	m.clientInfo = ci
	m.stateMu.Unlock()
}

func (m *Manager) currentToken() *Token {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	return m.token
}

func (m *Manager) currentClientInfo() *ClientRegistration {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	return m.clientInfo
}

func (m *Manager) callbackHost() string {
	if m.host != "" {
		return m.host
	}
	return "127.0.0.1"
}

func (m *Manager) registeredCallbackPort() int {
	if m.callbackPort != 0 {
		return m.callbackPort
	}
	return defaultCallbackPort
}
