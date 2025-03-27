package sessions

import (
	"context"
	"errors"
)

type Sessions struct {
}

func New() *Sessions {
	return &Sessions{}
}

func (s *Sessions) SessionAuth(ctx context.Context, key string) (context.Context, error) {
	// key = check if we already added this to context from cookie (GOA doesn't natively support)
	if key == "" {
		return ctx, errors.New("session key is required")
	}

	// check redis for session key
	// attach auth info to context

	return ctx, nil
}
