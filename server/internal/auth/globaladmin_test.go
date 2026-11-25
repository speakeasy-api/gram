package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRequireGlobalAdmin(t *testing.T) {
	t.Parallel()
	email := "admin@example.com"
	dashboard := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{IsAdmin: true, Email: &email})
	for _, tt := range []struct {
		name    string
		ctx     func() context.Context
		allowed bool
	}{
		{"missing", context.Background, false},
		{"dashboard admin", func() context.Context { return dashboard }, true},
		{"dashboard non-admin", func() context.Context {
			return contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{})
		}, false},
		{"standalone admin", func() context.Context {
			return contextvalues.SetAdminAuthContext(context.Background(), &contextvalues.AdminAuthContext{SessionID: "session", OIDCSubject: "subject", Email: email})
		}, true},
		{"nil admin is absent", func() context.Context { return contextvalues.SetAdminAuthContext(context.Background(), nil) }, false},
		{"missing session blocks fallback", func() context.Context {
			return contextvalues.SetAdminAuthContext(dashboard, &contextvalues.AdminAuthContext{OIDCSubject: "subject"})
		}, false},
		{"missing subject blocks fallback", func() context.Context {
			return contextvalues.SetAdminAuthContext(dashboard, &contextvalues.AdminAuthContext{SessionID: "session"})
		}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _, err := auth.RequireGlobalAdmin(tt.ctx(), testenv.NewLogger(t))
			if (err == nil) != tt.allowed {
				t.Fatalf("allowed=%v, err=%v", tt.allowed, err)
			}
			if tt.allowed && got != email {
				t.Fatalf("email=%q", got)
			}
		})
	}
}

func TestRequireGlobalAdmin_NilLogger(t *testing.T) {
	t.Parallel()
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{
		SessionID: "session", OIDCSubject: "subject", Email: "admin@example.com",
	})
	email, logger, err := auth.RequireGlobalAdmin(ctx, nil)
	var shareable *oops.ShareableError
	if !errors.As(err, &shareable) || shareable.Code != oops.CodeUnavailable {
		t.Fatalf("expected unavailable, got %v", err)
	}
	if email != "" || logger != nil {
		t.Fatal("expected no actor or logger")
	}
}
