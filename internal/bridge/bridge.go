// Package bridge implements the stdio<->remote MCP relay.
package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
)

// ByteConn is the interface for bidirectional frame transport.
type ByteConn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, frame []byte) error
	Close() error
}

// StdioConn wraps stdin/stdout as a ByteConn using newline-delimited frames.
type StdioConn struct {
	reader   *bufio.Scanner
	writer   io.Writer
	closer   io.Closer // underlying reader, closed to unblock Scan
	mu       sync.Mutex
	scanCh   chan scanResult
	done     chan struct{}
	doneOnce sync.Once
}

var newlineFrame = []byte{'\n'}

type scanResult struct {
	data []byte
	err  error
}

// NewStdioConn creates a ByteConn over the given reader and writer.
func NewStdioConn(r io.Reader, w io.Writer) *StdioConn {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1<<20), 10<<20) // 10 MiB max line
	var closer io.Closer
	if c, ok := r.(io.Closer); ok {
		closer = c
	}
	s := &StdioConn{
		reader: scanner,
		writer: w,
		closer: closer,
		scanCh: make(chan scanResult, 1),
		done:   make(chan struct{}),
	}
	go s.scanLoop()
	return s
}

func (s *StdioConn) scanLoop() {
	for {
		if s.reader.Scan() {
			data := bytes.Clone(s.reader.Bytes())
			select {
			case s.scanCh <- scanResult{data: data}:
			case <-s.done:
				return
			}
		} else {
			err := s.reader.Err()
			if err == nil {
				err = io.EOF
			}
			select {
			case s.scanCh <- scanResult{err: err}:
			case <-s.done:
			}
			return
		}
	}
}

func (s *StdioConn) Read(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, io.EOF
	case r := <-s.scanCh:
		return r.data, r.err
	}
}

func (s *StdioConn) Write(_ context.Context, frame []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Trim trailing whitespace to prevent double-newline framing errors.
	// HTTP response bodies may include trailing \r\n which, combined with our
	// delimiter newline, produces an empty line that the client fails to parse.
	frame = bytes.TrimRight(frame, " \t\r\n")
	if len(frame) == 0 {
		return nil
	}
	if _, err := s.writer.Write(frame); err != nil {
		return err
	}
	_, err := s.writer.Write(newlineFrame)
	return err
}

func (s *StdioConn) Close() error {
	s.doneOnce.Do(func() {
		close(s.done)
		if s.closer != nil {
			s.closer.Close()
		}
	})
	return nil
}

// Relay bridges messages between local and remote connections.
type Relay struct {
	local  ByteConn
	remote ByteConn
	logger *slog.Logger

	clientID string
}

// RelayConfig holds relay configuration.
type RelayConfig struct {
	Logger   *slog.Logger
	ClientID string
}

// NewRelay creates a relay between local and remote connections.
func NewRelay(local, remote ByteConn, cfg RelayConfig) *Relay {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Relay{
		local:    local,
		remote:   remote,
		logger:   logger,
		clientID: cfg.ClientID,
	}
}

// Run starts the bidirectional relay, blocking until disconnect or context cancellation.
func (r *Relay) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)

	go func() { errCh <- r.relayLocalToRemote(ctx) }()
	go func() { errCh <- r.relayRemoteToLocal(ctx) }()

	err := <-errCh
	cancel()
	r.local.Close()
	r.remote.Close()
	<-errCh

	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (r *Relay) relayLocalToRemote(ctx context.Context) error {
	for {
		frame, err := r.local.Read(ctx)
		if err != nil {
			return err
		}

		r.logger.Debug("local->remote", "size", len(frame))

		transformed := r.transformOutbound(frame)
		if err := r.remote.Write(ctx, transformed); err != nil {
			return err
		}
	}
}

func (r *Relay) relayRemoteToLocal(ctx context.Context) error {
	for {
		frame, err := r.remote.Read(ctx)
		if err != nil {
			return err
		}

		r.logger.Debug("remote->local", "size", len(frame))

		if err := r.local.Write(ctx, frame); err != nil {
			return err
		}
	}
}

// jsonRPCMessage is a minimal JSON-RPC envelope for inspection.
type jsonRPCMessage struct {
	Method string `json:"method,omitempty"`
}

func (r *Relay) transformOutbound(frame []byte) []byte {
	if r.clientID == "" {
		return frame
	}

	var msg jsonRPCMessage
	if err := json.Unmarshal(frame, &msg); err != nil {
		return frame
	}

	if msg.Method == "initialize" {
		return r.transformInitialize(frame)
	}

	return frame
}

func (r *Relay) transformInitialize(frame []byte) []byte {
	if r.clientID == "" {
		return frame
	}

	var raw map[string]any
	if err := json.Unmarshal(frame, &raw); err != nil {
		return frame
	}

	params, _ := raw["params"].(map[string]any)
	if params == nil {
		return frame
	}
	clientInfo, _ := params["clientInfo"].(map[string]any)
	if clientInfo == nil {
		return frame
	}

	name, _ := clientInfo["name"].(string)
	clientInfo["name"] = name + " (via " + r.clientID + ")"
	params["clientInfo"] = clientInfo
	raw["params"] = params

	out, err := json.Marshal(raw)
	if err != nil {
		return frame
	}
	return out
}
