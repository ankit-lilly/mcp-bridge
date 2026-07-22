package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscovery_FallbackToOrigin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"authorization_endpoint": "https://auth.example.com/authorize",
				"token_endpoint":         "https://auth.example.com/token",
				"registration_endpoint":  "https://auth.example.com/register",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mgr := NewManager(ManagerConfig{ServerURL: server.URL + "/mcp"})
	mgr.client = server.Client()

	err := mgr.discover(context.Background(), nil)
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if mgr.authzEndpoint != "https://auth.example.com/authorize" {
		t.Fatalf("unexpected authz endpoint: %s", mgr.authzEndpoint)
	}
}

func TestDiscovery_PathSpecificPRM(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"resource":              "https://mcp.example.com/mcp",
				"authorization_servers": []string{serverURL},
				"scopes_supported":      []string{"mcp:read", "mcp:write"},
			})
		case "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"authorization_endpoint": "https://auth.example.com/authorize",
				"token_endpoint":         "https://auth.example.com/token",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	mgr := NewManager(ManagerConfig{ServerURL: server.URL + "/mcp"})
	mgr.client = server.Client()

	err := mgr.discover(context.Background(), nil)
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if mgr.authzEndpoint != "https://auth.example.com/authorize" {
		t.Fatalf("unexpected authz endpoint: %s", mgr.authzEndpoint)
	}
	if mgr.resolvedScope != "mcp:read mcp:write" {
		t.Fatalf("unexpected scope: %q", mgr.resolvedScope)
	}
}

func TestDiscovery_ChallengeResourceMetadata(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/custom-prm":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"resource":              "https://mcp.example.com",
				"authorization_servers": []string{serverURL},
			})
		case "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"authorization_endpoint": "https://auth.example.com/authz",
				"token_endpoint":         "https://auth.example.com/token",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	mgr := NewManager(ManagerConfig{ServerURL: server.URL + "/mcp"})
	mgr.client = server.Client()

	challenge := &ChallengeInfo{
		ResourceMetadataURL: server.URL + "/custom-prm",
		Scope:               "mcp:admin",
	}
	err := mgr.discover(context.Background(), challenge)
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if mgr.authzEndpoint != "https://auth.example.com/authz" {
		t.Fatalf("unexpected authz endpoint: %s", mgr.authzEndpoint)
	}
	// Challenge scope should be used (priority 2 over empty PRM scopes)
	if mgr.resolvedScope != "mcp:admin" {
		t.Fatalf("unexpected scope: %q", mgr.resolvedScope)
	}
}

func TestScopePriority_StaticOverridesChallenge(t *testing.T) {
	mgr := NewManager(ManagerConfig{
		ServerURL:      "https://example.com",
		ClientMetadata: json.RawMessage(`{"scope":"static:scope"}`),
	})

	challenge := &ChallengeInfo{Scope: "challenge:scope"}
	scope := mgr.effectiveScope(challenge, nil, nil)
	if scope != "static:scope" {
		t.Fatalf("expected static scope, got %q", scope)
	}
}

func TestScopePriority_ChallengeOverridesPRM(t *testing.T) {
	mgr := NewManager(ManagerConfig{ServerURL: "https://example.com"})

	challenge := &ChallengeInfo{Scope: "from-challenge"}
	prm := &resourceMetadata{ScopesSupported: []string{"from-prm"}}
	scope := mgr.effectiveScope(challenge, prm, nil)
	if scope != "from-challenge" {
		t.Fatalf("expected challenge scope, got %q", scope)
	}
}
