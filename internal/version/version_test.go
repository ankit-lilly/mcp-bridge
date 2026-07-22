package version

import "testing"

func TestString(t *testing.T) {
	s := String()
	if s == "" {
		t.Fatal("version string must not be empty")
	}
	if s != "mcp-bridge dev (commit=unknown, built=unknown)" {
		t.Fatalf("unexpected default version string: %s", s)
	}
}

func TestStringWithValues(t *testing.T) {
	old := Version
	Version = "1.0.0"
	defer func() { Version = old }()

	s := String()
	if s != "mcp-bridge 1.0.0 (commit=unknown, built=unknown)" {
		t.Fatalf("unexpected version string: %s", s)
	}
}
