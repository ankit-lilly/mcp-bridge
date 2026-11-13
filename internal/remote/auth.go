package remote

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
)

var ErrUnauthorized = errors.New("unauthorized")

var ErrForbidden = errors.New("forbidden")

// TokenSource provides access tokens for authenticated requests.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// Authorizer handles the full authorization flow when a challenge is received.
type Authorizer interface {
	EnsureAuthorized(ctx context.Context, challenge *AuthRequiredError) error
}

type AuthRequiredError struct {
	StatusCode int
	Headers    http.Header
	Phase      string // "write", "server-event-stream"
	// SentCredential identifies the exact Authorization header rejected by the server.
	// An empty header has its own fingerprint; the credential is never retained here.
	SentCredential [32]byte
}

func CredentialFingerprint(authorization string) [32]byte {
	return sha256.Sum256([]byte(authorization))
}

func (e *AuthRequiredError) Error() string {
	return fmt.Sprintf("auth required (status %d, phase %s)", e.StatusCode, e.Phase)
}
