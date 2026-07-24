package remote

import (
	"context"
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
}

func (e *AuthRequiredError) Error() string {
	return fmt.Sprintf("auth required (status %d, phase %s)", e.StatusCode, e.Phase)
}
