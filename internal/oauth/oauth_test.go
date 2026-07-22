package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestManager_CallbackServer(t *testing.T) {
	result := make(chan callbackData, 1)
	state := "test-state-123"

	mgr := NewManager(ManagerConfig{
		ServerURL:    "https://example.com",
		Host:         "127.0.0.1",
		CallbackPort: 0, // let OS assign
		AuthTimeout:  10 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server, redirectURI, err := mgr.startCallbackServer(ctx, state, result)
	if err != nil {
		t.Fatalf("start callback server: %v", err)
	}
	defer server.Shutdown(context.Background())

	if !strings.HasPrefix(redirectURI, "http://127.0.0.1:") {
		t.Fatalf("unexpected redirect URI host: %s", redirectURI)
	}
	if !strings.HasSuffix(redirectURI, "/oauth/callback") {
		t.Fatalf("unexpected redirect URI path: %s", redirectURI)
	}

	// Test callback with valid state
	callbackURL := redirectURI + "?state=" + state + "&code=auth-code-xyz"
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback failed: %v", err)
	}
	resp.Body.Close()

	// Check result
	select {
	case r := <-result:
		if r.err != nil {
			t.Fatalf("unexpected error: %v", r.err)
		}
		if r.code != "auth-code-xyz" {
			t.Fatalf("expected code auth-code-xyz, got %s", r.code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback result")
	}
}

func TestManager_CallbackServer_InvalidState(t *testing.T) {
	result := make(chan callbackData, 1)
	state := "expected-state"

	mgr := NewManager(ManagerConfig{
		ServerURL:    "https://example.com",
		Host:         "127.0.0.1",
		CallbackPort: 0,
		AuthTimeout:  10 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server, redirectURI, err := mgr.startCallbackServer(ctx, state, result)
	if err != nil {
		t.Fatalf("start callback server: %v", err)
	}
	defer server.Shutdown(context.Background())

	// Try with wrong state
	callbackURL := redirectURI + "?state=wrong-state&code=code123"
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid state, got %d", resp.StatusCode)
	}

	select {
	case r := <-result:
		if r.err == nil {
			t.Fatal("expected error for invalid state")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for error")
	}
}

func TestManager_TokenExchange(t *testing.T) {
	// Fake token endpoint
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.ParseForm()
		if r.FormValue("grant_type") != "authorization_code" {
			http.Error(w, "invalid grant type", http.StatusBadRequest)
			return
		}
		if r.FormValue("code") != "valid-code" {
			http.Error(w, "invalid code", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer tokenServer.Close()

	mgr := NewManager(ManagerConfig{
		ServerURL:    "https://example.com",
		Host:         "127.0.0.1",
		CallbackPort: 0,
		AuthTimeout:  10 * time.Second,
	})
	mgr.tokenEndpoint = tokenServer.URL
	mgr.clientInfo = &ClientRegistration{ClientID: "test-client"}

	ctx := context.Background()
	tok, err := mgr.exchangeCode(ctx, "valid-code", "test-verifier", "http://localhost:8080/oauth/callback")
	if err != nil {
		t.Fatalf("exchange failed: %v", err)
	}
	if tok.AccessToken != "new-access-token" {
		t.Fatalf("unexpected access token: %s", tok.AccessToken)
	}
	if tok.RefreshToken != "new-refresh-token" {
		t.Fatalf("unexpected refresh token: %s", tok.RefreshToken)
	}
}

func TestManager_RefreshToken(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.FormValue("grant_type") != "refresh_token" {
			http.Error(w, "invalid grant type", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "refreshed-token",
			"refresh_token": "new-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer tokenServer.Close()

	mgr := NewManager(ManagerConfig{
		ServerURL:   "https://example.com",
		Host:        "127.0.0.1",
		AuthTimeout: 10 * time.Second,
	})
	mgr.tokenEndpoint = tokenServer.URL
	mgr.clientInfo = &ClientRegistration{ClientID: "test-client"}
	mgr.token = &Token{
		AccessToken:  "old-token",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour), // expired
	}

	ctx := context.Background()
	err := mgr.refreshToken(ctx)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if mgr.token.AccessToken != "refreshed-token" {
		t.Fatalf("unexpected token: %s", mgr.token.AccessToken)
	}
}

func TestManager_DiscoverMetadata(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"authorization_endpoint": "https://auth.example.com/authorize",
				"token_endpoint":         "https://auth.example.com/token",
				"registration_endpoint":  "https://auth.example.com/register",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer authServer.Close()

	mgr := NewManager(ManagerConfig{
		ServerURL: authServer.URL + "/mcp",
	})

	// Override the server URL host to match our test server
	u, _ := url.Parse(authServer.URL)
	mgr.serverURL = fmt.Sprintf("%s://%s/mcp", u.Scheme, u.Host)

	ctx := context.Background()
	err := mgr.discoverMetadata(ctx)
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if mgr.authzEndpoint != "https://auth.example.com/authorize" {
		t.Fatalf("unexpected authz endpoint: %s", mgr.authzEndpoint)
	}
}

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		t.Fatalf("PKCE generation failed: %v", err)
	}
	if len(verifier) == 0 {
		t.Fatal("verifier must not be empty")
	}
	if len(challenge) == 0 {
		t.Fatal("challenge must not be empty")
	}
	if verifier == challenge {
		t.Fatal("verifier and challenge must differ")
	}
}

func TestGenerateState(t *testing.T) {
	s1, err := generateState()
	if err != nil {
		t.Fatalf("state generation failed: %v", err)
	}
	s2, _ := generateState()
	if s1 == s2 {
		t.Fatal("states should be unique")
	}
}

func TestManager_WriteManualAuthorizationURL_IsInstanceScoped(t *testing.T) {
	var stderr1 bytes.Buffer
	var stderr2 bytes.Buffer

	mgr1 := NewManager(ManagerConfig{Stderr: &stderr1})
	mgr2 := NewManager(ManagerConfig{Stderr: &stderr2})

	mgr1.writeManualAuthorizationURL("https://example.com/one")

	if !strings.Contains(stderr1.String(), "https://example.com/one") {
		t.Fatalf("first manager did not write its auth URL: %q", stderr1.String())
	}
	if stderr2.Len() != 0 {
		t.Fatalf("second manager unexpectedly received output: %q", stderr2.String())
	}

	mgr2.writeManualAuthorizationURL("https://example.com/two")

	if !strings.Contains(stderr2.String(), "https://example.com/two") {
		t.Fatalf("second manager did not write its auth URL: %q", stderr2.String())
	}
	if strings.Contains(stderr1.String(), "https://example.com/two") {
		t.Fatalf("first manager unexpectedly received second manager output: %q", stderr1.String())
	}
}
