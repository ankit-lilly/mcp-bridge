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

	if challenge != nil && challenge.ResourceMetadataURL != "" {
		prm, err = m.fetchResourceMetadata(ctx, challenge.ResourceMetadataURL)
		if err != nil {
			m.logger.Debug("challenge resource_metadata fetch failed", "err", err)
		}
	}

	if prm == nil {
		prm, _ = m.fetchPathSpecificPRM(ctx)
	}

	if prm == nil {
		prm, _ = m.fetchRootPRM(ctx)
	}

	if prm != nil && len(prm.AuthorizationServers) > 0 {
		asMeta, err := m.fetchAuthServerMetadata(ctx, prm.AuthorizationServers[0])
		if err == nil {
			m.authzEndpoint = asMeta.AuthorizationEndpoint
			m.tokenEndpoint = asMeta.TokenEndpoint
			m.regEndpoint = asMeta.RegistrationEndpoint
			m.resolvedScope = m.effectiveScope(challenge, prm, asMeta)
			return nil
		}
		m.logger.Debug("auth server metadata fetch failed", "err", err)
	}

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

// effectiveScope resolves scope by priority: static client metadata,
// WWW-Authenticate challenge, PRM scopes_supported, AS metadata scopes_supported,
// then a safe default (many auth servers reject empty scope).
func (m *Manager) effectiveScope(challenge *ChallengeInfo, prm *resourceMetadata, asMeta *authServerMetadata) string {
	if m.clientMetadata != nil {
		var meta map[string]any
		if err := json.Unmarshal(m.clientMetadata, &meta); err == nil {
			if scope, ok := meta["scope"].(string); ok && scope != "" {
				return scope
			}
		}
	}

	if challenge != nil && challenge.Scope != "" {
		return challenge.Scope
	}

	if prm != nil && len(prm.ScopesSupported) > 0 {
		return strings.Join(prm.ScopesSupported, " ")
	}

	if asMeta != nil && len(asMeta.ScopesSupported) > 0 {
		return strings.Join(asMeta.ScopesSupported, " ")
	}

	return "openid email profile"
}
