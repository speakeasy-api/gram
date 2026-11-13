package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestExplicitNoAuthDoesNotBorrowClientHeaderCoverage(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_explicit_noauth_review")
	require.NoError(t, err)
	for _, tc := range []struct {
		name                             string
		admin, sourceCredential, enabled bool
	}{
		{name: "disabled admin without source credentials", admin: true},
		{name: "disabled backfilled without source credentials"},
		{name: "disabled admin with copied source credentials", admin: true, sourceCredential: true},
		{name: "disabled backfilled with copied source credentials", sourceCredential: true},
		{name: "enabled admin refuses no auth", admin: true, enabled: true},
		{name: "enabled backfilled refuses no auth", enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			var id uuid.UUID
			if tc.admin {
				id = f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
			} else {
				id = f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
			}
			// The discarded client's matching header must not cover credentials
			// required by the server when no client will send them.
			f.copyHeader(id, "Authorization", true, true, text("sealed-fixture"), pgtype.Text{})
			if tc.sourceCredential {
				f.requireSourceHeader(f.sourceHeader("Authorization", true, text("sealed-fixture"), pgtype.Text{}))
			}
			require.NoError(t, f.q.SetAuthClientEnabledFixture(t.Context(), authclientbackfill.SetAuthClientEnabledFixtureParams{ID: id, Enabled: tc.enabled}))
			res, err := f.service.resolveNoAuthChoice(t.Context(), f.orgID, f.serverID)
			require.NoError(t, err)
			res = enforcedCompatibility(res)
			want := authClientNoAuth
			if tc.sourceCredential || tc.enabled {
				want = authClientGap
			}
			require.Equal(t, want, res.outcome)
			require.Equal(t, uuid.Nil, res.clientID)
			require.Empty(t, res.headers)

			if !tc.enabled {
				subject := urn.NewUserSubject("test-user")
				unchosen, err := f.service.resolveUnchosenAuthClient(t.Context(), f.orgID, uuid.Nil, subject, consentAuthServer{id: f.serverID, projectID: f.projectID})
				require.NoError(t, err)
				require.Equal(t, want, unchosen.outcome)
				// Explicit no-auth must not change named or inferred client consent.
				named, err := f.service.resolveAuthClientChoiceForConsent(t.Context(), f.orgID, f.serverID, f.projectID, id, subject)
				require.NoError(t, err)
				require.Equal(t, authClientReasonClientDisabled, named.reason)
				inferred, err := f.service.resolveAuthClientForConsent(t.Context(), f.orgID, f.serverID, f.projectID, uuid.NullUUID{}, subject)
				require.NoError(t, err)
				require.Equal(t, authClientReasonClientDisabled, inferred.reason)
			}
		})
	}
}
