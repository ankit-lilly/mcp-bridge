package remote

import (
	"context"
	"fmt"
	"net/http"
)

type AuthRequiredError struct {
	StatusCode int
	Headers    http.Header
	Phase      string // "write", "server-event-stream"
}

func (e *AuthRequiredError) Error() string {
	return fmt.Sprintf("auth required (status %d, phase %s)", e.StatusCode, e.Phase)
}

type Authorizer interface {
	EnsureAuthorized(ctx context.Context, challenge *AuthRequiredError) error
}
