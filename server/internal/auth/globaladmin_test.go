package auth

import (
	"context"
	"log/slog"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestRequireGlobalAdmin(t *testing.T) {
	email := "admin@example.com"
	dashboard := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{IsAdmin: true, Email: &email})
	for _, tt := range []struct {
		name    string
		ctx     context.Context
		allowed bool
	}{
		{"missing", context.Background(), false},
		{"dashboard admin", dashboard, true},
		{"dashboard non-admin", contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{}), false},
		{"standalone admin", contextvalues.SetAdminAuthContext(context.Background(), &contextvalues.AdminAuthContext{SessionID: "session", OIDCSubject: "subject", Email: email}), true},
		{"nil admin is absent", contextvalues.SetAdminAuthContext(context.Background(), nil), false},
		{"missing session blocks fallback", contextvalues.SetAdminAuthContext(dashboard, &contextvalues.AdminAuthContext{OIDCSubject: "subject"}), false},
		{"missing subject blocks fallback", contextvalues.SetAdminAuthContext(dashboard, &contextvalues.AdminAuthContext{SessionID: "session"}), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := RequireGlobalAdmin(tt.ctx, slog.Default())
			if (err == nil) != tt.allowed {
				t.Fatalf("allowed=%v, err=%v", tt.allowed, err)
			}
			if tt.allowed && got != email {
				t.Fatalf("email=%q", got)
			}
		})
	}
}
