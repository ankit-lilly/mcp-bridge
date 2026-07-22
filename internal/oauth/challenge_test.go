package oauth

import (
	"net/http"
	"testing"
)

func TestParseWWWAuthenticate_Scope(t *testing.T) {
	h := http.Header{}
	h.Set("Www-Authenticate", `Bearer scope="read write"`)
	info := ParseWWWAuthenticate(h)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.Scope != "read write" {
		t.Fatalf("expected scope 'read write', got %q", info.Scope)
	}
}

func TestParseWWWAuthenticate_ResourceMetadata(t *testing.T) {
	h := http.Header{}
	h.Set("Www-Authenticate", `Bearer resource_metadata="https://example.com/.well-known/oauth-protected-resource/mcp"`)
	info := ParseWWWAuthenticate(h)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.ResourceMetadataURL != "https://example.com/.well-known/oauth-protected-resource/mcp" {
		t.Fatalf("unexpected resource_metadata: %q", info.ResourceMetadataURL)
	}
}

func TestParseWWWAuthenticate_QuotedAndUnquoted(t *testing.T) {
	h := http.Header{}
	h.Set("Www-Authenticate", `Bearer scope=openid, error=invalid_token`)
	info := ParseWWWAuthenticate(h)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.Scope != "openid" {
		t.Fatalf("expected scope 'openid', got %q", info.Scope)
	}
	if info.Error != "invalid_token" {
		t.Fatalf("expected error 'invalid_token', got %q", info.Error)
	}
}

func TestParseWWWAuthenticate_MultipleHeaders(t *testing.T) {
	h := http.Header{}
	h.Add("Www-Authenticate", `Bearer scope="mcp:read"`)
	h.Add("Www-Authenticate", `Bearer resource_metadata="https://rs.example.com/.well-known/oauth-protected-resource"`)
	info := ParseWWWAuthenticate(h)
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.Scope != "mcp:read" {
		t.Fatalf("expected scope 'mcp:read', got %q", info.Scope)
	}
	if info.ResourceMetadataURL != "https://rs.example.com/.well-known/oauth-protected-resource" {
		t.Fatalf("unexpected resource_metadata: %q", info.ResourceMetadataURL)
	}
}

func TestParseWWWAuthenticate_NonBearer(t *testing.T) {
	h := http.Header{}
	h.Set("Www-Authenticate", `Basic realm="test"`)
	info := ParseWWWAuthenticate(h)
	if info != nil {
		t.Fatal("expected nil for non-Bearer")
	}
}

func TestParseWWWAuthenticate_Empty(t *testing.T) {
	h := http.Header{}
	info := ParseWWWAuthenticate(h)
	if info != nil {
		t.Fatal("expected nil for empty headers")
	}
}
