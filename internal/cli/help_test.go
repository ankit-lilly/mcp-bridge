package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteRootHelpIncludesConfigureClaude(t *testing.T) {
	var buf bytes.Buffer
	WriteRootHelp(&buf)

	out := buf.String()
	for _, want := range []string{
		inspectUsageLine,
		configureClaudeUsageLine,
		configureClaudePassthroughUsageLine,
		"configure-claude",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected root help to contain %q, got:\n%s", want, out)
		}
	}
}

func TestBridgeFlagSetUsageIncludesUsageLine(t *testing.T) {
	var buf bytes.Buffer
	fs, _ := newBridgeFlagSet("mcp-bridge inspect", inspectUsageLine)
	fs.SetOutput(&buf)
	fs.Usage()

	out := buf.String()
	if !strings.Contains(out, inspectUsageLine) {
		t.Fatalf("expected usage to contain %q, got:\n%s", inspectUsageLine, out)
	}
	if !strings.Contains(out, "Flags:") {
		t.Fatalf("expected usage to contain flags header, got:\n%s", out)
	}
}

func TestConfigureClaudeFlagSetUsageIncludesPassthroughForm(t *testing.T) {
	var buf bytes.Buffer
	fs, _ := newConfigureClaudeFlagSet()
	fs.SetOutput(&buf)
	fs.Usage()

	out := buf.String()
	if !strings.Contains(out, configureClaudeUsageLine) {
		t.Fatalf("expected usage to contain %q, got:\n%s", configureClaudeUsageLine, out)
	}
	if !strings.Contains(out, configureClaudePassthroughUsageLine) {
		t.Fatalf("expected usage to contain %q, got:\n%s", configureClaudePassthroughUsageLine, out)
	}
}
