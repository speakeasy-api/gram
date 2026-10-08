package remotemcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestGetServerContext(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		server *ServerContext
		auth   *contextvalues.AuthContext
		want   ServerContext
		ok     bool
	}{
		{name: "absent"},
		{name: "server without project", server: &ServerContext{OrganizationID: "org-owner"}, want: ServerContext{OrganizationID: "org-owner"}, ok: true},
		{name: "auth without project", auth: &contextvalues.AuthContext{ActiveOrganizationID: "org-caller"}, want: ServerContext{OrganizationID: "org-caller"}, ok: true},
		{name: "empty server overrides auth", server: new(ServerContext), auth: &contextvalues.AuthContext{ActiveOrganizationID: "org-caller"}, ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if tc.auth != nil {
				ctx = contextvalues.SetAuthContext(ctx, tc.auth)
			}
			if tc.server != nil {
				ctx = WithServerContext(ctx, *tc.server)
			}
			got, ok := getServerContext(ctx)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}
