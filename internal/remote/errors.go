package remote

import (
	"context"
	"errors"
)

var ErrUnauthorized = errors.New("unauthorized")

var ErrForbidden = errors.New("forbidden")

type TokenSource interface {
	Token(ctx context.Context) (string, error)
}
