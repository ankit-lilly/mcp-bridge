package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ankit-lilly/mcp-bridge/internal/bridge"
)

type HTTPConnector struct {
	client      *http.Client
	url         string
	headers     map[string]string
	tokenSource TokenSource
	authorizer  Authorizer
	logger      *slog.Logger
	userAgent   string
}

type HTTPConnectorConfig struct {
	Client      *http.Client
	URL         string
	Headers     map[string]string
	TokenSource TokenSource
	Authorizer  Authorizer
	Logger      *slog.Logger
	UserAgent   string
}

func NewHTTPConnector(cfg HTTPConnectorConfig) *HTTPConnector {
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &HTTPConnector{
		client:      client,
		url:         cfg.URL,
		headers:     cfg.Headers,
		tokenSource: cfg.TokenSource,
		authorizer:  cfg.Authorizer,
		logger:      logger,
		userAgent:   cfg.UserAgent,
	}
}

func (c *HTTPConnector) Connect(_ context.Context) (bridge.ByteConn, error) {
	return &streamableConn{
		connector: c,
		inbound:   make(chan []byte, 64),
		done:      make(chan struct{}),
		logger:    c.logger,
		userAgent: c.userAgent,
	}, nil
}

type streamableConn struct {
	connector *HTTPConnector
	inbound   chan []byte
	done      chan struct{}
	closeOnce sync.Once
	logger    *slog.Logger

	mu              sync.Mutex
	sessionID       string
	protocolVersion string // negotiated from initialize response
	initialized     bool
	initFrame       []byte // stored initialize request for session recovery
	recovering      bool   // guards against infinite recovery loops
	userAgent       string // updated with client name from initialize

	streamMu       sync.Mutex
	streamCancel   context.CancelFunc
	streamResp     *http.Response
	lastStreamAuth [32]byte // credential already rejected after a successful login
}

var sseDataPrefix = []byte("data: ")

func (c *streamableConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, io.EOF
	case frame, ok := <-c.inbound:
		if !ok {
			return nil, io.EOF
		}
		return frame, nil
	}
}

func (c *streamableConn) Write(ctx context.Context, frame []byte) error {
	if c.isInitializeRequest(frame) {
		c.mu.Lock()
		c.initFrame = bytes.Clone(frame)
		c.mu.Unlock()
		c.extractClientName(frame)
	}

	resp, err := c.doPost(ctx, frame)
	if err != nil {
		return err
	}

	resp, err = c.handleAuthChallenge(ctx, resp, frame)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
	}

	return c.dispatchResponse(ctx, resp, frame)
}

// handleAuthChallenge retries the request after re-authorization if the server
// returned 401 or 403. The caller must close the returned response body.
func (c *streamableConn) handleAuthChallenge(ctx context.Context, resp *http.Response, frame []byte) (*http.Response, error) {
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		return resp, nil
	}

	// Capture the credential from this request, not a later Token() call: another
	// request may refresh the token before this response arrives.
	sentCredential := CredentialFingerprint(resp.Request.Header.Get("Authorization"))
	resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden && !remediableForbidden(resp.Header) {
		return nil, ErrForbidden
	}
	if c.connector.authorizer == nil {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, ErrUnauthorized
		}
		return nil, ErrForbidden
	}

	challenge := &AuthRequiredError{
		StatusCode:     resp.StatusCode,
		Headers:        resp.Header,
		Phase:          "write",
		SentCredential: sentCredential,
	}
	if authErr := c.connector.authorizer.EnsureAuthorized(ctx, challenge); authErr != nil {
		return nil, authErr
	}

	return c.doPost(ctx, frame)
}

func (c *streamableConn) dispatchResponse(ctx context.Context, resp *http.Response, frame []byte) error {
	switch resp.StatusCode {
	case http.StatusOK:
		ct := resp.Header.Get("Content-Type")
		isInit := c.isInitializeRequest(frame)
		if strings.Contains(ct, "text/event-stream") {
			c.readInlineSSE(resp.Body, isInit)
		} else if strings.Contains(ct, "application/json") {
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return fmt.Errorf("reading response body: %w", readErr)
			}
			if len(bytes.TrimSpace(body)) > 0 {
				if isInit {
					c.handleInitializeResponse(body)
				}
				c.enqueueFrame(body)
			}
		}
	case http.StatusAccepted, http.StatusNoContent:
	case http.StatusNotFound:
		c.mu.Lock()
		hasSession := c.sessionID != ""
		c.mu.Unlock()
		if hasSession {
			return c.recover(ctx, frame)
		}
		return fmt.Errorf("streamable HTTP endpoint not found (404)")
	case http.StatusMethodNotAllowed:
		return fmt.Errorf("streamable HTTP POST not allowed (405)")
	default:
		return fmt.Errorf("POST returned status %d", resp.StatusCode)
	}

	return nil
}

// recover re-establishes the session after a 404 by re-sending the stored
// initialize handshake, then retries the failed frame.
func (c *streamableConn) recover(ctx context.Context, failedFrame []byte) error {
	c.mu.Lock()
	initFrame := c.initFrame
	alreadyRecovering := c.recovering
	c.mu.Unlock()

	if initFrame == nil || alreadyRecovering {
		return fmt.Errorf("session terminated by server (404)")
	}

	c.mu.Lock()
	c.recovering = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.recovering = false
		c.mu.Unlock()
	}()

	c.logger.Info("session expired (404), attempting recovery...")

	c.streamMu.Lock()
	if c.streamCancel != nil {
		c.streamCancel()
	}
	if c.streamResp != nil {
		c.streamResp.Body.Close()
	}
	c.streamCancel = nil
	c.streamResp = nil
	c.streamMu.Unlock()

	c.mu.Lock()
	c.sessionID = ""
	c.protocolVersion = ""
	c.initialized = false
	c.mu.Unlock()

	if err := c.Write(ctx, initFrame); err != nil {
		return fmt.Errorf("session recovery: re-initialize failed: %w", err)
	}

	notif := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if err := c.Write(ctx, notif); err != nil {
		return fmt.Errorf("session recovery: initialized notification failed: %w", err)
	}

	c.mu.Lock()
	newSID := c.sessionID
	c.mu.Unlock()
	c.logger.Info("session recovery successful", "new_session", newSID)

	if err := c.Write(ctx, failedFrame); err != nil {
		return fmt.Errorf("session recovery: retry failed: %w", err)
	}
	return nil
}

func (c *streamableConn) doPost(ctx context.Context, frame []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.connector.url, bytes.NewReader(frame))
	if err != nil {
		return nil, err
	}

	// A token-source failure here must not abort the POST: the original code
	// swallowed it and let the resulting 401 drive the authorization flow, which
	// is what keeps the bridge self-healing on token expiry. Propagating the
	// error would kill the relay process and force a manual restart. Log it so
	// the failure is diagnosable, then send the request and let the 401 (if any)
	// trigger recovery.
	if err := c.applyHeaders(req); err != nil {
		c.logger.Warn("token source failed before POST; relying on server challenge for recovery", "err", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	c.mu.Lock()
	if c.protocolVersion != "" {
		req.Header.Set("Mcp-Protocol-Version", c.protocolVersion)
	} else {
		req.Header.Set("Mcp-Protocol-Version", "2025-03-26")
	}
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.Unlock()

	return c.connector.client.Do(req)
}

var errEventStreamNotSupported = fmt.Errorf("server event stream not supported")

func (c *streamableConn) runEventStream(ctx context.Context) {
	backoff := time.Second
	for {
		err := c.connectEventStream(ctx)
		if errors.Is(err, errEventStreamNotSupported) || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden) {
			if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden) {
				c.logger.Warn("stopping event stream after rejected authorization", "err", err)
			}
			return
		}
		if err != nil {
			c.logger.Warn("event stream disconnected", "err", err)
		}
		select {
		case <-c.done:
			return
		case <-ctx.Done():
			return
		default:
		}
		jitter := time.Duration(rand.Int64N(int64(backoff / 4)))
		select {
		case <-time.After(backoff + jitter):
		case <-c.done:
			return
		case <-ctx.Done():
			return
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (c *streamableConn) connectEventStream(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.connector.url, nil)
	if err != nil {
		return err
	}

	if err := c.applyHeaders(req); err != nil {
		return fmt.Errorf("preparing event stream authorization: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	c.mu.Lock()
	if c.protocolVersion != "" {
		req.Header.Set("Mcp-Protocol-Version", c.protocolVersion)
	} else {
		req.Header.Set("Mcp-Protocol-Version", "2025-03-26")
	}
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.Unlock()

	resp, err := c.connector.client.Do(req)
	if err != nil {
		return err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		c.streamMu.Lock()
		c.lastStreamAuth = [32]byte{}
		c.streamMu.Unlock()
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		resp.Body.Close()
		return errEventStreamNotSupported
	case http.StatusUnauthorized, http.StatusForbidden:
		sentCredential := CredentialFingerprint(resp.Request.Header.Get("Authorization"))
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden && !remediableForbidden(resp.Header) {
			return ErrForbidden
		}
		c.streamMu.Lock()
		alreadyRejected := c.lastStreamAuth == sentCredential && sentCredential != ([32]byte{}) && sentCredential != CredentialFingerprint("")
		c.streamMu.Unlock()
		if alreadyRejected {
			return ErrUnauthorized
		}
		if c.connector.authorizer != nil {
			challenge := &AuthRequiredError{
				StatusCode:     resp.StatusCode,
				Headers:        resp.Header,
				Phase:          "server-event-stream",
				SentCredential: sentCredential,
			}
			if authErr := c.connector.authorizer.EnsureAuthorized(ctx, challenge); authErr != nil {
				return authErr
			}
			c.streamMu.Lock()
			c.lastStreamAuth = CredentialFingerprint(req.Header.Get("Authorization"))
			c.streamMu.Unlock()
			return fmt.Errorf("retrying event stream after authorization")
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return ErrUnauthorized
		}
		return ErrForbidden
	default:
		resp.Body.Close()
		return fmt.Errorf("server event stream returned %d", resp.StatusCode)
	}

	c.streamMu.Lock()
	c.streamResp = resp
	c.streamMu.Unlock()

	c.readStream(resp)
	return nil
}

func (c *streamableConn) readStream(resp *http.Response) {
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1<<20), 10<<20)

	var dataBuf bytes.Buffer
	for scanner.Scan() {
		line := scanner.Bytes()

		if data, ok := bytes.CutPrefix(line, sseDataPrefix); ok {
			dataBuf.Write(data)
			continue
		}

		if len(line) == 0 && dataBuf.Len() > 0 {
			data := bytes.Clone(dataBuf.Bytes())
			dataBuf.Reset()

			if !c.enqueueFrame(data) {
				return
			}
		}
	}
}

func (c *streamableConn) readInlineSSE(body io.Reader, isInitReq bool) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 1<<20), 10<<20)
	var dataBuf bytes.Buffer
	initHandled := false

	for scanner.Scan() {
		line := scanner.Bytes()
		if data, ok := bytes.CutPrefix(line, sseDataPrefix); ok {
			dataBuf.Write(data)
			continue
		}
		if len(line) == 0 && dataBuf.Len() > 0 {
			data := bytes.Clone(dataBuf.Bytes())
			dataBuf.Reset()
			if isInitReq && !initHandled {
				c.handleInitializeResponse(data)
				initHandled = true
			}
			if !c.enqueueFrame(data) {
				return
			}
		}
	}
}

func (c *streamableConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)

		c.streamMu.Lock()
		if c.streamCancel != nil {
			c.streamCancel()
		}
		if c.streamResp != nil {
			c.streamResp.Body.Close()
		}
		c.streamMu.Unlock()

		c.mu.Lock()
		sid := c.sessionID
		pv := c.protocolVersion
		c.mu.Unlock()
		if sid != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "DELETE", c.connector.url, nil)
			if err == nil {
				if err := c.applyHeaders(req); err != nil {
					c.logger.Warn("skipping session cleanup after token failure", "err", err)
					return
				}
				req.Header.Set("Mcp-Session-Id", sid)
				if pv != "" {
					req.Header.Set("Mcp-Protocol-Version", pv)
				}
				resp, err := c.connector.client.Do(req)
				if err == nil {
					resp.Body.Close()
				}
			}
		}
	})
	return nil
}

func (c *streamableConn) applyHeaders(req *http.Request) error {
	for k, v := range c.connector.headers {
		req.Header.Set(k, v)
	}
	if c.connector.tokenSource != nil {
		tok, err := c.connector.tokenSource.Token(req.Context())
		if err != nil {
			return fmt.Errorf("obtaining access token: %w", err)
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	c.mu.Lock()
	ua := c.userAgent
	c.mu.Unlock()
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	return nil
}

func remediableForbidden(headers http.Header) bool {
	for _, value := range headers.Values("WWW-Authenticate") {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "bearer ") {
			continue
		}
		for _, field := range strings.Split(value, ",") {
			field = strings.TrimSpace(field)
			if len(field) >= 7 && strings.EqualFold(field[:7], "Bearer ") {
				field = strings.TrimSpace(field[7:])
			}
			key, code, ok := strings.Cut(field, "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "error") && strings.Trim(strings.TrimSpace(code), "\"") == "insufficient_scope" {
				return true
			}
		}
	}
	return false
}

// extractClientName reads clientInfo.name from the initialize frame and
// prepends it to the User-Agent string so the remote server sees who the
// actual MCP client is (e.g. "Claude Desktop mcp-bridge/v1.2.3").
func (c *streamableConn) extractClientName(frame []byte) {
	var msg struct {
		Params *struct {
			ClientInfo *struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"clientInfo"`
		} `json:"params"`
	}
	if err := json.Unmarshal(frame, &msg); err != nil {
		return
	}
	if msg.Params == nil || msg.Params.ClientInfo == nil {
		return
	}

	clientName := msg.Params.ClientInfo.Name
	if clientName == "" {
		return
	}

	c.mu.Lock()
	base := c.userAgent
	if base != "" {
		c.userAgent = clientName + " " + base
	} else {
		c.userAgent = clientName
	}
	c.mu.Unlock()
}

func (c *streamableConn) enqueueFrame(frame []byte) bool {
	select {
	case c.inbound <- frame:
		return true
	case <-c.done:
		return false
	}
}

func (c *streamableConn) isInitializeRequest(frame []byte) bool {
	var msg struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(frame, &msg); err != nil {
		return false
	}
	return msg.Method == "initialize"
}

func (c *streamableConn) handleInitializeResponse(body []byte) {
	var resp struct {
		Result *struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
		Error *json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}
	if resp.Result == nil || resp.Error != nil {
		return
	}

	c.mu.Lock()
	wasInit := c.initialized
	c.initialized = true
	if resp.Result.ProtocolVersion != "" {
		c.protocolVersion = resp.Result.ProtocolVersion
	}
	c.mu.Unlock()

	if !wasInit {
		streamCtx, streamCancel := context.WithCancel(context.Background())
		c.streamMu.Lock()
		c.streamCancel = streamCancel
		c.streamMu.Unlock()
		go c.runEventStream(streamCtx)
	}
}
