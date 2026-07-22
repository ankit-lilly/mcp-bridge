package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type callbackData struct {
	code string
	err  error
}

// Authorize runs the full OAuth flow: metadata discovery, client registration, browser auth, token exchange.
// An optional ChallengeInfo from the failing HTTP response drives discovery.
func (m *Manager) Authorize(ctx context.Context, challenges ...*ChallengeInfo) (*Token, *ClientRegistration, error) {
	var challenge *ChallengeInfo
	if len(challenges) > 0 {
		challenge = challenges[0]
	}

	if err := m.discover(ctx, challenge); err != nil {
		return nil, nil, fmt.Errorf("metadata discovery: %w", err)
	}

	if m.currentClientInfo() == nil {
		if err := m.registerClient(ctx); err != nil {
			return nil, nil, fmt.Errorf("client registration: %w", err)
		}
	}

	verifier, codeChallenge, err := generatePKCE()
	if err != nil {
		return nil, nil, fmt.Errorf("PKCE generation: %w", err)
	}

	state, err := generateState()
	if err != nil {
		return nil, nil, fmt.Errorf("state generation: %w", err)
	}

	callbackResult := make(chan callbackData, 1)
	server, redirectURI, err := m.startCallbackServer(ctx, state, callbackResult)
	if err != nil {
		return nil, nil, fmt.Errorf("callback server: %w", err)
	}
	defer server.Shutdown(context.Background())

	authURL := m.buildAuthURL(state, codeChallenge, redirectURI)
	m.logger.Info("opening browser for authorization", "url", authURL)

	if err := openBrowser(authURL); err != nil {
		m.logger.Info("could not open browser, please visit URL manually")
		m.writeManualAuthorizationURL(authURL)
	}

	var result callbackData
	select {
	case result = <-callbackResult:
	case <-time.After(m.authTimeout):
		return nil, nil, errors.New("authorization timed out waiting for callback")
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}

	if result.err != nil {
		return nil, nil, result.err
	}

	tok, err := m.exchangeCode(ctx, result.code, verifier, redirectURI)
	if err != nil {
		return nil, nil, fmt.Errorf("token exchange: %w", err)
	}

	m.SetToken(tok)

	if m.onTokenChange != nil {
		if err := m.onTokenChange(ctx, tok); err != nil {
			return nil, nil, fmt.Errorf("persisting token: %w", err)
		}
	}

	return tok, m.currentClientInfo(), nil
}

func (m *Manager) discoverMetadata(ctx context.Context) error {
	return m.discover(ctx, nil)
}

func (m *Manager) registerClient(ctx context.Context) error {
	if m.regEndpoint == "" {
		return errors.New("no registration endpoint available")
	}

	redirectURI := fmt.Sprintf("http://%s:%d/oauth/callback", m.callbackHost(), m.registeredCallbackPort())

	metadata := map[string]any{
		"client_name":                "mcp-bridge",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}

	if m.clientMetadata != nil {
		var userMeta map[string]any
		if err := json.Unmarshal(m.clientMetadata, &userMeta); err == nil {
			maps.Copy(metadata, userMeta)
		}
	}

	body, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", m.regEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("client registration returned %d", resp.StatusCode)
	}

	var reg ClientRegistration
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return err
	}
	reg.RedirectURI = redirectURI

	m.SetClientInfo(&reg)
	return nil
}

func (m *Manager) buildAuthURL(state, challenge, redirectURI string) string {
	clientID := ""
	if clientInfo := m.currentClientInfo(); clientInfo != nil {
		clientID = clientInfo.ClientID
	}

	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if m.resource != "" {
		params.Set("resource", m.resource)
	} else if m.serverURL != "" {
		params.Set("resource", m.serverURL)
	}
	if m.resolvedScope != "" {
		params.Set("scope", m.resolvedScope)
	}
	return m.authzEndpoint + "?" + params.Encode()
}

func (m *Manager) exchangeCode(ctx context.Context, code, verifier, redirectURI string) (*Token, error) {
	clientInfo := m.currentClientInfo()
	if clientInfo == nil {
		return nil, errors.New("no client registration available")
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientInfo.ClientID},
		"code_verifier": {verifier},
	}
	if clientInfo.ClientSecret != "" {
		data.Set("client_secret", clientInfo.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", m.tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	tok := &Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Scope:        tokenResp.Scope,
	}
	if tokenResp.ExpiresIn > 0 {
		tok.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return tok, nil
}

func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
	return exec.Command(cmd, args...).Start()
}

func (m *Manager) writeManualAuthorizationURL(authURL string) {
	fmt.Fprintf(m.stderr, "\nPlease open this URL to authorize:\n%s\n\n", authURL)
}
