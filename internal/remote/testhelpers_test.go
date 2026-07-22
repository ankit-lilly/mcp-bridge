package remote

import "context"

type StaticToken string

func (s StaticToken) Token(_ context.Context) (string, error) { return string(s), nil }
