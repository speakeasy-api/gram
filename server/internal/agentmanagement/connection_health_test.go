package agentmanagement

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/conv"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestAgentConnectionHealth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		refresh        bool
		refreshExpired bool
		missingLink    bool
		want           bool
	}{
		{"expired access without refresh", false, false, false, true},
		{"expired access can refresh", true, false, false, false},
		{"expired refresh and access", true, true, false, true},
		{"removed issuer association", true, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			seedOrganization(t, db, "org-a")
			seedOrganizationUser(t, db, "org-a", "owner")
			project := seedProject(t, db, "org-a", "example")
			agent := createAgent(t, db, "org-a", "owner", "Example agent")
			_, issuer := seedManagedSession(t, db, "org-a", "user:owner")
			session := seedManagedUpstreamWith(t, db, "org-a", issuer, "user:owner", func(p *remoterepo.UpsertRemoteSessionParams) {
				p.AccessExpiresAt = pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true, InfinityModifier: pgtype.Finite}
				p.RefreshTokenEncrypted = pgtype.Text{String: "", Valid: false}
				if tc.refresh {
					p.RefreshTokenEncrypted = conv.ToPGText("fixture-refresh")
				}
				if tc.refreshExpired {
					p.RefreshExpiresAt = pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true, InfinityModifier: pgtype.Finite}
				}
			})
			q := remoterepo.New(db)
			require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(t.Context(), remoterepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: session.RemoteSessionClientID, UserSessionIssuerID: issuer}))
			_, err := q.AttachPrincipalRemoteSessionBinding(t.Context(), remoterepo.AttachPrincipalRemoteSessionBindingParams{ProjectID: project.ID, OrganizationID: "org-a", PrincipalID: agent.ID, UserSessionIssuerID: issuer, SubjectUrn: "user:owner", RemoteSessionID: session.ID})
			require.NoError(t, err)
			if tc.missingLink {
				removed, err := q.DetachOrganizationRemoteSessionClientFromUserSessionIssuer(t.Context(), remoterepo.DetachOrganizationRemoteSessionClientFromUserSessionIssuerParams{OrganizationID: "org-a", RemoteSessionClientID: session.RemoteSessionClientID, UserSessionIssuerID: issuer})
				require.NoError(t, err)
				require.EqualValues(t, 1, removed)
			}
			svc := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
			ctx := validatedHumanContext(t, "org-a", "owner")
			got, err := svc.Get(ctx, &gen.GetPayload{ID: agent.ID.String()})
			require.NoError(t, err)
			require.Equal(t, tc.want, *got.ConnectionNeedsRefresh)
			listed, err := svc.List(ctx, &gen.ListPayload{})
			require.NoError(t, err)
			require.Len(t, listed.Items, 1)
			require.Equal(t, tc.want, *listed.Items[0].ConnectionNeedsRefresh)
		})
	}
}
