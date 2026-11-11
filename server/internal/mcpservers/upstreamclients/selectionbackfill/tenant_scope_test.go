package selectionbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func siblingTenantFixture(t *testing.T, f *fixture) *fixture {
	t.Helper()
	other := &fixture{t: t, pool: f.pool, q: f.q, orgID: "org_" + uuid.NewString(), projectID: uuid.New()}
	require.NoError(t, other.q.SeedOrganizationFixture(t.Context(), other.orgID))
	require.NoError(t, other.q.SeedProjectFixture(t.Context(), SeedProjectFixtureParams{ID: other.projectID, Slug: "other-" + other.projectID.String()[:8], OrganizationID: other.orgID}))
	return other
}

// Project ownership is canonical even when legacy issuer metadata names a
// different tenant. Discovery, coverage, and the shared writer must agree.
func TestProjectIssuerTenantPrecedenceMatchesCoverageAndRecording(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newFixture(t)
	other := siblingTenantFixture(t, f)
	issuer := uuid.New()
	require.NoError(t, f.q.SeedUserSessionIssuerFixture(ctx, SeedUserSessionIssuerFixtureParams{ID: issuer, ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, OrganizationID: pgtype.Text{String: other.orgID, Valid: true}, Slug: "legacy-stale-owner"}))
	client := f.client(issuer)
	server := f.server(issuer, client)
	credential := f.session(issuer)
	live, err := f.q.ListLiveUserSessionCredentials(ctx, ListLiveUserSessionCredentialsParams{Cursor: uuid.Nil, OrganizationID: f.orgID, PageSize: 10, IssuerID: uuid.NullUUID{UUID: issuer, Valid: true}})
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.Equal(t, f.orgID, live[0].OrganizationID)
	classes, err := authclientbackfill.New(f.pool).ListLiveCredentialClasses(ctx, authclientbackfill.ListLiveCredentialClassesParams{OrganizationID: f.orgID, Cursor: uuid.Nil, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, classes, 1, "typed coverage must discover the same canonical-tenant lineage")
	require.Equal(t, credential, classes[0].CredentialID)
	coverage, err := f.q.ListUpstreamClientSelectionCoverage(ctx, ListUpstreamClientSelectionCoverageParams{UserSessionIssuerID: issuer, CredentialKind: "user_session", CredentialID: credential})
	require.NoError(t, err)
	require.Len(t, coverage, 1)
	require.Equal(t, server, coverage[0].McpServerID)
	require.Equal(t, OutcomeClient, Classify(coverage[0]))
	report, err := Run(ctx, f.pool, Options{OrganizationID: f.orgID, Apply: true, PageSize: 0})
	require.NoError(t, err)
	require.Equal(t, int64(1), report.Written)
	require.Equal(t, client, f.selection(issuer, credential, server).RemoteSessionClientID.UUID)
	foreign, err := f.q.ListLiveUserSessionCredentials(ctx, ListLiveUserSessionCredentialsParams{Cursor: uuid.Nil, OrganizationID: other.orgID, PageSize: 10, IssuerID: uuid.NullUUID{UUID: issuer, Valid: true}})
	require.NoError(t, err)
	require.Empty(t, foreign)
}

// The internal offline runner deliberately supports all-tenant maintenance.
// Every credential is still recorded only against its own issuer's reach;
// request-driven lifecycle entry points reject an empty tenant instead.
func TestTrustedGlobalMaintenancePreservesCredentialTenants(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newFixture(t)
	other := siblingTenantFixture(t, f)
	firstIssuer, secondIssuer := f.projectIssuer(), other.projectIssuer()
	firstServer, secondServer := f.server(firstIssuer), other.server(secondIssuer)
	firstCredential, secondCredential := f.session(firstIssuer), other.session(secondIssuer)
	report, err := Run(ctx, f.pool, Options{OrganizationID: "", Apply: true, PageSize: 0})
	require.NoError(t, err)
	require.Equal(t, 2, report.Credentials)
	require.Equal(t, map[uuid.UUID]uuid.UUID{firstServer: uuid.Nil}, f.selections(firstCredential))
	require.Equal(t, map[uuid.UUID]uuid.UUID{secondServer: uuid.Nil}, other.selections(secondCredential))
	tx := testenv.BeginTx(t, ctx, f.pool)
	require.Error(t, ReconcileIssuerInLockedTransaction(ctx, tx, "", firstIssuer))
}
