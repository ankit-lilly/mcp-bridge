package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (m *Manager) Token(ctx context.Context) (string, error) {
	return m.GetToken(ctx)
}

func (m *Manager) GetToken(ctx context.Context) (string, error) {
	tok := m.currentToken()
	if tok == nil {
		return "", nil
	}

	if !tok.ExpiresAt.IsZero() && time.Now().After(tok.ExpiresAt) {
		if err := m.dedupRefresh(ctx, tok); err != nil {
			return "", err
		}
		tok = m.currentToken()
		if tok == nil {
			return "", errors.New("token refresh did not yield a token")
		}
	}

	return tok.AccessToken, nil
}

func (m *Manager) dedupRefresh(ctx context.Context, observed *Token) error {
	m.refreshMu.Lock()
	if m.refreshing {
		done := m.refreshDone
		m.refreshMu.Unlock()
		select {
		case <-done:
			m.refreshMu.Lock()
			err := m.refreshErr
			m.refreshMu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		m.refreshMu.Unlock()
		return err
	}
	// The caller may have observed the old token before another refresh finished.
	tok := m.currentToken()
	if tok != observed || tok == nil || tok.ExpiresAt.IsZero() || !time.Now().After(tok.ExpiresAt) {
		m.refreshMu.Unlock()
		return nil
	}
	if tok.RefreshToken == "" {
		m.refreshMu.Unlock()
		return errors.New("token expired and no refresh token available")
	}
	m.refreshing = true
	m.refreshDone = make(chan struct{})
	m.refreshMu.Unlock()

	err := m.doRefresh(ctx)

	m.refreshMu.Lock()
	m.refreshErr = err
	m.refreshing = false
	close(m.refreshDone)
	m.refreshMu.Unlock()
	return err
}

func (m *Manager) refreshToken(ctx context.Context) error {
	return m.doRefresh(ctx)
}

func (m *Manager) doRefresh(ctx context.Context) error {
	tok := m.currentToken()
	if tok == nil || tok.RefreshToken == "" {
		return errors.New("no refresh token available")
	}

	clientInfo := m.currentClientInfo()
	if clientInfo == nil {
		return errors.New("no client registration available")
	}

	if m.tokenEndpoint == "" {
		if err := m.discoverMetadata(ctx); err != nil {
			return err
		}
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
		"client_id":     {clientInfo.ClientID},
	}
	if m.resource != "" {
		data.Set("resource", m.resource)
	} else if m.serverURL != "" {
		data.Set("resource", m.serverURL)
	}
	if clientInfo.ClientSecret != "" {
		data.Set("client_secret", clientInfo.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", m.tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var oauthErr struct {
			Code string `json:"error"`
		}
		// Only include the OAuth error code, never an arbitrary response body or description.
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&oauthErr)
		if len(oauthErr.Code) > 0 && len(oauthErr.Code) <= 64 {
			valid := true
			for _, ch := range oauthErr.Code {
				if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' && ch != '-' {
					valid = false
					break
				}
			}
			if valid {
				return fmt.Errorf("token refresh returned %d (%s)", resp.StatusCode, oauthErr.Code)
			}
		}
		return fmt.Errorf("token refresh returned %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	newTok := &Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tok.RefreshToken,
		TokenType:    tokenResp.TokenType,
	}
	if tokenResp.RefreshToken != "" {
		newTok.RefreshToken = tokenResp.RefreshToken
	}
	if tokenResp.ExpiresIn > 0 {
		newTok.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	m.SetToken(newTok)

	if m.onTokenChange != nil {
		if err := m.onTokenChange(ctx, newTok); err != nil {
			m.logger.Warn("token persistence failed after refresh", "err", err)
			return fmt.Errorf("persisting refreshed token: %w", err)
		}
	}
	return nil
}
