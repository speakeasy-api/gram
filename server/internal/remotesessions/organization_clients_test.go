package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func clientIDs(clients []remotesessions.Client) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(clients))
	for _, c := range clients {
		ids = append(ids, c.ID)
	}
	return ids
}

// An organization-level issuer's clients span its organization's projects, and
// the organization-wide listing returns every one of them with the project
// that owns it.
func TestListOrganizationClients_SpansProjects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgIssuer := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "org-clients-usi-"+uuid.NewString()[:8])

	firstProject := *authCtx.ProjectID
	secondProject := createProject(t, ctx, ti.conn, "org-clients-"+uuid.NewString()[:8])
	firstRemote := createRemoteIssuerInProject(t, ctx, ti.conn, firstProject, "first-idp-"+uuid.NewString()[:8])
	secondRemote := createRemoteIssuerInProject(t, ctx, ti.conn, secondProject, "second-idp-"+uuid.NewString()[:8])
	first := seedRemoteClientAtTier(t, ctx, ti.conn, conv.ToNullUUID(firstProject), conv.ToPGText(authCtx.ActiveOrganizationID), firstRemote, "first-client", orgIssuer)
	second := seedRemoteClientAtTier(t, ctx, ti.conn, conv.ToNullUUID(secondProject), conv.ToPGText(authCtx.ActiveOrganizationID), secondRemote, "second-client", orgIssuer)
	orgRemote := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "org-idp-"+uuid.NewString()[:8])
	shared := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgRemote, "shared-client", orgIssuer)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))

	clients, err := mgr.ListOrganizationClients(ctx, authCtx.ActiveOrganizationID, orgIssuer)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{first, second, shared}, clientIDs(clients))
	for _, c := range clients {
		switch c.ID {
		case first:
			require.Equal(t, conv.ToNullUUID(firstProject), c.ProjectID)
		case second:
			require.Equal(t, conv.ToNullUUID(secondProject), c.ProjectID)
		default:
			require.False(t, c.ProjectID.Valid, "an organization-level client belongs to no project")
		}
	}

	// A project's own listing still stops at that project.
	projectClients, err := mgr.ListClients(ctx, firstProject, authCtx.ActiveOrganizationID, orgIssuer)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{first, shared}, clientIDs(projectClients))
}

// The organization-wide listing widens only an organization-level issuer: a
// project's own issuer never reaches another project's clients through it.
func TestListOrganizationClients_ProjectIssuerDoesNotWiden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectIssuer := createUserSessionIssuer(t, ctx, ti.conn, "project-usi-"+uuid.NewString()[:8])
	remote := createRemoteIssuerInProject(t, ctx, ti.conn, *authCtx.ProjectID, "project-idp-"+uuid.NewString()[:8])
	seedRemoteClientAtTier(t, ctx, ti.conn, conv.ToNullUUID(*authCtx.ProjectID), conv.ToPGText(authCtx.ActiveOrganizationID), remote, "project-client", projectIssuer)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))

	clients, err := mgr.ListOrganizationClients(ctx, authCtx.ActiveOrganizationID, projectIssuer)
	require.NoError(t, err)
	require.Empty(t, clients)
}
