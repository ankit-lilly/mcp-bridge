package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type resourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type authServerMetadata struct {
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint,omitempty"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
}

func (m *Manager) discover(ctx context.Context, challenge *ChallengeInfo) error {
	var prm *resourceMetadata
	var err error

	// 1. If challenge has resource_metadata URL, fetch that directly
	if challenge != nil && challenge.ResourceMetadataURL != "" {
		prm, err = m.fetchResourceMetadata(ctx, challenge.ResourceMetadataURL)
		if err != nil {
			m.logger.Debug("challenge resource_metadata fetch failed", "err", err)
		}
	}

	// 2. Try path-specific PRM
	if prm == nil {
		prm, _ = m.fetchPathSpecificPRM(ctx)
	}

	// 3. Try root PRM
	if prm == nil {
		prm, _ = m.fetchRootPRM(ctx)
	}

	// 4. If PRM found, use first authorization_servers[]
	if prm != nil && len(prm.AuthorizationServers) > 0 {
		asMeta, err := m.fetchAuthServerMetadata(ctx, prm.AuthorizationServers[0])
		if err == nil {
			m.authzEndpoint = asMeta.AuthorizationEndpoint
			m.tokenEndpoint = asMeta.TokenEndpoint
			m.regEndpoint = asMeta.RegistrationEndpoint
			// Resolve scope
			m.resolvedScope = m.effectiveScope(challenge, prm, asMeta)
			return nil
		}
		m.logger.Debug("auth server metadata fetch failed", "err", err)
	}

	// 5. Fallback: origin-based discovery
	return m.discoverFromOrigin(ctx, challenge)
}

func (m *Manager) discoverFromOrigin(ctx context.Context, challenge *ChallengeInfo) error {
	u, err := url.Parse(m.serverURL)
	if err != nil {
		return err
	}

	wellKnown := fmt.Sprintf("%s://%s/.well-known/oauth-authorization-server", u.Scheme, u.Host)
	asMeta, err := m.fetchAuthServerMetadataURL(ctx, wellKnown)
	if err != nil {
		return err
	}

	m.authzEndpoint = asMeta.AuthorizationEndpoint
	m.tokenEndpoint = asMeta.TokenEndpoint
	m.regEndpoint = asMeta.RegistrationEndpoint
	m.resolvedScope = m.effectiveScope(challenge, nil, asMeta)
	return nil
}

func (m *Manager) fetchResourceMetadata(ctx context.Context, metaURL string) (*resourceMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", metaURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resource metadata returned %d", resp.StatusCode)
	}
	var meta resourceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (m *Manager) fetchPathSpecificPRM(ctx context.Context) (*resourceMetadata, error) {
	u, err := url.Parse(m.serverURL)
	if err != nil {
		return nil, err
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	prmURL := fmt.Sprintf("%s://%s/.well-known/oauth-protected-resource%s", u.Scheme, u.Host, path)
	return m.fetchResourceMetadata(ctx, prmURL)
}

func (m *Manager) fetchRootPRM(ctx context.Context) (*resourceMetadata, error) {
	u, err := url.Parse(m.serverURL)
	if err != nil {
		return nil, err
	}
	prmURL := fmt.Sprintf("%s://%s/.well-known/oauth-protected-resource", u.Scheme, u.Host)
	return m.fetchResourceMetadata(ctx, prmURL)
}

func (m *Manager) fetchAuthServerMetadata(ctx context.Context, asURL string) (*authServerMetadata, error) {
	wellKnown := strings.TrimSuffix(asURL, "/") + "/.well-known/oauth-authorization-server"
	return m.fetchAuthServerMetadataURL(ctx, wellKnown)
}

func (m *Manager) fetchAuthServerMetadataURL(ctx context.Context, wellKnown string) (*authServerMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", wellKnown, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth server metadata returned %d", resp.StatusCode)
	}
	var meta authServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// effectiveScope resolves scope with priority:
// 1. Static scope from user-provided client metadata
// 2. scope from WWW-Authenticate challenge
// 3. scopes_supported from PRM
// 4. scopes_supported from AS metadata
// 5. empty (let server decide)
func (m *Manager) effectiveScope(challenge *ChallengeInfo, prm *resourceMetadata, asMeta *authServerMetadata) string {
	// 1. Static scope from client metadata
	if m.clientMetadata != nil {
		var meta map[string]any
		if err := json.Unmarshal(m.clientMetadata, &meta); err == nil {
			if scope, ok := meta["scope"].(string); ok && scope != "" {
				return scope
			}
		}
	}

	// 2. From challenge
	if challenge != nil && challenge.Scope != "" {
		return challenge.Scope
	}

	// 3. From PRM
	if prm != nil && len(prm.ScopesSupported) > 0 {
		return strings.Join(prm.ScopesSupported, " ")
	}

	// 4. From AS metadata
	if asMeta != nil && len(asMeta.ScopesSupported) > 0 {
		return strings.Join(asMeta.ScopesSupported, " ")
	}

	// 5. Safe default — many auth servers reject empty scope
	return "openid email profile"
}
