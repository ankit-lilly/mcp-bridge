package logx

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNew_Debug(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{Debug: true, Output: &buf})
	logger.Debug("test message", "key", "value")
	if !strings.Contains(buf.String(), "test message") {
		t.Fatal("debug message should appear when debug is enabled")
	}
}

func TestNew_Silent(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{Silent: true, Output: &buf})
	logger.Error("should not appear")
	if buf.Len() != 0 {
		t.Fatal("silent mode should suppress all output")
	}
}

func TestSanitization(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{Debug: true, Output: &buf})
	logger.Info("auth", slog.String("token", "secret-value"))
	output := buf.String()
	if strings.Contains(output, "secret-value") {
		t.Fatal("sensitive token value must be redacted")
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Fatal("redaction marker must appear")
	}
}

func TestSanitization_NonSensitive(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{Debug: true, Output: &buf})
	logger.Info("data", slog.String("method", "tools/list"))
	output := buf.String()
	if !strings.Contains(output, "tools/list") {
		t.Fatal("non-sensitive values should not be redacted")
	}
}
