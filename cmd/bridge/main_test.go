package main

import "testing"

func TestSplitMode_DefaultBridgeMode(t *testing.T) {
	m, args, err := splitMode([]string{"https://example.com/mcp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != bridgeMode {
		t.Fatalf("expected bridge mode, got %v", m)
	}
	if len(args) != 1 || args[0] != "https://example.com/mcp" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestSplitMode_InspectSubcommand(t *testing.T) {
	m, args, err := splitMode([]string{"inspect", "https://example.com/mcp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != inspectMode {
		t.Fatalf("expected inspect mode, got %v", m)
	}
	if len(args) != 1 || args[0] != "https://example.com/mcp" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestSplitMode_ConfigureClaudeSubcommand(t *testing.T) {
	m, args, err := splitMode([]string{"configure-claude", "--name", "remote-server", "https://example.com/mcp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != configureClaudeMode {
		t.Fatalf("expected configure-claude mode, got %v", m)
	}
	if len(args) != 3 || args[0] != "--name" || args[1] != "remote-server" || args[2] != "https://example.com/mcp" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestSplitMode_BridgeAlias(t *testing.T) {
	m, args, err := splitMode([]string{"bridge", "https://example.com/mcp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != bridgeMode {
		t.Fatalf("bridge alias should map to bridge mode, got %v", m)
	}
	if len(args) != 1 || args[0] != "https://example.com/mcp" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestIsVersionArg(t *testing.T) {
	if !isVersionArg("--version") || !isVersionArg("-v") {
		t.Fatal("expected version flags to be recognized")
	}
	if isVersionArg("inspect") {
		t.Fatal("unexpected version match for inspect")
	}
}

func TestIsHelpArg(t *testing.T) {
	if !isHelpArg("--help") || !isHelpArg("-h") {
		t.Fatal("expected help flags to be recognized")
	}
	if isHelpArg("inspect") {
		t.Fatal("unexpected help match for inspect")
	}
}
