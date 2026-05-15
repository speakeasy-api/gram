package workos

import (
	"context"
	"fmt"

	"github.com/workos/workos-go/v6/pkg/passwordless"
)

// PasswordlessSession is a magic-link session returned by WorkOS.
type PasswordlessSession struct {
	ID        string
	Email     string
	ExpiresAt string
	Link      string
}

// CreatePasswordlessSessionOpts configures a passwordless magic-link session.
type CreatePasswordlessSessionOpts struct {
	Email       string
	RedirectURI string
	ExpiresIn   int    // seconds
	State       string // opaque state forwarded through the flow
}

// CreatePasswordlessSession creates a WorkOS magic-link session and returns the
// session details including the magic link URL. Does NOT send an email — the
// caller is responsible for delivering the link (e.g. via Loops).
func (wc *Client) CreatePasswordlessSession(ctx context.Context, opts CreatePasswordlessSessionOpts) (*PasswordlessSession, error) {
	pwl := &passwordless.Client{
		APIKey:     wc.apiKey,
		HTTPClient: wc.httpClient,
		Endpoint:   wc.endpoint,
		JSONEncode: nil,
	}
	sess, err := pwl.CreateSession(ctx, passwordless.CreateSessionOpts{
		Email:       opts.Email,
		Type:        passwordless.MagicLink,
		Connection:  "",
		RedirectURI: opts.RedirectURI,
		ExpiresIn:   opts.ExpiresIn,
		State:       opts.State,
	})
	if err != nil {
		return nil, fmt.Errorf("create passwordless session: %w", err)
	}

	return &PasswordlessSession{
		ID:        sess.ID,
		Email:     sess.Email,
		ExpiresAt: sess.ExpiresAt,
		Link:      sess.Link,
	}, nil
}
