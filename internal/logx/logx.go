package logx

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	Debug  bool
	Silent bool
	Output io.Writer // defaults to os.Stderr
}

func New(cfg Config) *slog.Logger {
	out := cfg.Output
	if out == nil {
		out = os.Stderr
	}

	if cfg.Silent {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			return sanitizeAttr(a)
		},
	}

	return slog.New(slog.NewTextHandler(out, opts))
}

var sensitiveKeys = map[string]bool{
	"token":         true,
	"access_token":  true,
	"refresh_token": true,
	"authorization": true,
	"auth_code":     true,
	"client_secret": true,
	"code_verifier": true,
}

func sanitizeAttr(a slog.Attr) slog.Attr {
	if sensitiveKeys[strings.ToLower(a.Key)] {
		a.Value = slog.StringValue("[REDACTED]")
	}
	return a
}
