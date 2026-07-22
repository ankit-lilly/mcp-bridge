package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
}

type HTTPConnectorConfig struct {
	Client      *http.Client
	URL         string
	Headers     map[string]string
	TokenSource TokenSource
	Authorizer  Authorizer
}

func NewHTTPConnector(cfg HTTPConnectorConfig) *HTTPConnector {
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPConnector{
		client:      client,
		url:         cfg.URL,
		headers:     cfg.Headers,
		tokenSource: cfg.TokenSource,
		authorizer:  cfg.Authorizer,
	}
}

func (c *HTTPConnector) Connect(_ context.Context) (bridge.ByteConn, error) {
	return &streamableConn{
		connector: c,
		inbound:   make(chan []byte, 64),
		done:      make(chan struct{}),
	}, nil
}

type streamableConn struct {
	connector *HTTPConnector
	inbound   chan []byte
	done      chan struct{}
	closeOnce sync.Once

	mu              sync.Mutex
	sessionID       string
	protocolVersion string // negotiated from initialize response
	initialized     bool

	streamMu     sync.Mutex
	streamCancel context.CancelFunc
	streamResp   *http.Response
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
	resp, err := c.doPost(ctx, frame)
	if err != nil {
		return err
	}

	resp, err = c.handleAuthChallenge(ctx, resp, frame)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Capture session ID
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
	}

	return c.dispatchResponse(resp, frame)
}

// handleAuthChallenge retries the request after re-authorization if the server
// returned 401 or 403. Returns the (possibly new) response or an error.
// The caller must close the returned response body.
func (c *streamableConn) handleAuthChallenge(ctx context.Context, resp *http.Response, frame []byte) (*http.Response, error) {
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		return resp, nil
	}

	resp.Body.Close()

	if c.connector.authorizer == nil {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, ErrUnauthorized
		}
		return nil, ErrForbidden
	}

	challenge := &AuthRequiredError{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Phase:      "write",
	}
	if authErr := c.connector.authorizer.EnsureAuthorized(ctx, challenge); authErr != nil {
		return nil, authErr
	}

	return c.doPost(ctx, frame)
}

// dispatchResponse handles the response body based on status code and content type.
func (c *streamableConn) dispatchResponse(resp *http.Response, frame []byte) error {
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
		// no body expected
	case http.StatusNotFound:
		c.mu.Lock()
		hasSession := c.sessionID != ""
		c.mu.Unlock()
		if hasSession {
			return fmt.Errorf("session terminated by server (404)")
		}
		return fmt.Errorf("streamable HTTP endpoint not found (404)")
	case http.StatusMethodNotAllowed:
		return fmt.Errorf("streamable HTTP POST not allowed (405)")
	default:
		return fmt.Errorf("POST returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *streamableConn) doPost(ctx context.Context, frame []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.connector.url, bytes.NewReader(frame))
	if err != nil {
		return nil, err
	}

	c.applyHeaders(req)
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
		if err == errEventStreamNotSupported {
			return
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

	c.applyHeaders(req)
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
		// proceed
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		resp.Body.Close()
		return errEventStreamNotSupported
	case http.StatusUnauthorized, http.StatusForbidden:
		resp.Body.Close()
		if c.connector.authorizer != nil {
			challenge := &AuthRequiredError{
				StatusCode: resp.StatusCode,
				Headers:    resp.Header,
				Phase:      "server-event-stream",
			}
			if authErr := c.connector.authorizer.EnsureAuthorized(ctx, challenge); authErr != nil {
				return authErr
			}
			return fmt.Errorf("retrying after auth") // will trigger reconnect loop
		}
		return fmt.Errorf("server event stream returned %d", resp.StatusCode)
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
				c.applyHeaders(req)
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

func (c *streamableConn) applyHeaders(req *http.Request) {
	for k, v := range c.connector.headers {
		req.Header.Set(k, v)
	}
	if c.connector.tokenSource != nil {
		if tok, err := c.connector.tokenSource.Token(req.Context()); err == nil && tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
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
	// Only proceed if this is a successful response (has result, no error)
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
