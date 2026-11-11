package selectionbackfill

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func batchSelectionParams(issuerID uuid.UUID, ids []uuid.UUID) usersessionsrepo.RecordUpstreamClientSelectionsForCredentialsParams {
	return usersessionsrepo.RecordUpstreamClientSelectionsForCredentialsParams{
		UserSessionIssuerID: issuerID, CredentialKind: upstreamclients.CredentialKindUserSession,
		CredentialIds: ids, Source: upstreamclients.SelectionSourceBackfill,
		ScopeMcpServerIds: nil, PickedMcpServerIds: []uuid.UUID{}, PickedClientIds: []uuid.UUID{}, PickedAuthClientIds: nil,
	}
}

// The batch statement must decide what issuance decides for one credential:
// the two statements are separate so sign-in stays untouched by bulk work.
func TestRecordUpstreamClientSelectionsForCredentialsMatchesIssuance(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.projectIssuer()
	registration := f.client(issuer)
	server := f.server(issuer, registration)
	unassociated := f.server(issuer)
	ambiguous := f.server(issuer, registration, f.client(issuer))
	typed := f.authClient(authclientbackfill.TypeRemoteSession, registration, server)
	q := usersessionsrepo.New(f.pool)
	issued := f.session(issuer)
	empty := f.session(issuer)
	first, second := f.session(issuer), f.session(issuer)
	require.Equal(t, int64(2), f.record(issuer, issued))
	n, err := q.RecordUpstreamClientSelectionsForCredentials(t.Context(), batchSelectionParams(issuer, []uuid.UUID{}))
	require.NoError(t, err)
	require.Zero(t, n)
	require.Empty(t, f.selections(empty), "an empty batch records nothing")
	params := batchSelectionParams(issuer, []uuid.UUID{first, second, first})
	params.ScopeMcpServerIds = []uuid.UUID{}
	n, err = q.RecordUpstreamClientSelectionsForCredentials(t.Context(), params)
	require.NoError(t, err)
	require.Zero(t, n, "an empty server scope is independently a no-op")
	params.ScopeMcpServerIds = nil
	n, err = q.RecordUpstreamClientSelectionsForCredentials(t.Context(), params)
	require.NoError(t, err)
	require.Equal(t, int64(4), n, "duplicate IDs must not conflict twice in one statement")
	want := f.selections(issued)
	require.Len(t, want, 2)
	require.Equal(t, registration, want[server])
	require.Contains(t, want, unassociated, "a server with no client records no-auth")
	require.NotContains(t, want, ambiguous, "two eligible clients record nothing")
	for _, credential := range []uuid.UUID{first, second} {
		require.Equal(t, want, f.selections(credential))
		require.Equal(t, f.selection(issuer, issued, unassociated).NoAuth, f.selection(issuer, credential, unassociated).NoAuth)
		require.Equal(t, uuid.NullUUID{UUID: typed, Valid: true}, f.namedAuthClient(credential, server).AuthClientID)
	}
	require.Equal(t, uuid.NullUUID{UUID: typed, Valid: true}, f.namedAuthClient(issued, server).AuthClientID)
}

func TestRecordUpstreamClientSelectionsBatchPreservesImmutableChoices(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.projectIssuer()
	server := f.server(issuer)
	replaceable := f.session(issuer)
	require.Equal(t, int64(1), f.record(issuer, replaceable))
	require.True(t, f.selection(issuer, replaceable, server).NoAuth)
	registration := f.client(issuer)
	f.associate(server, registration)
	typed := f.authClient(authclientbackfill.TypeRemoteSession, registration, server)
	immutable, missing := f.session(issuer), f.session(issuer)
	require.Equal(t, int64(1), f.record(issuer, immutable))
	before := f.selection(issuer, immutable, server)
	require.Equal(t, upstreamclients.SelectionSourceIssuance, before.Source)
	q := usersessionsrepo.New(f.pool)
	params := batchSelectionParams(issuer, []uuid.UUID{immutable, replaceable, missing})
	n, err := q.RecordUpstreamClientSelectionsForCredentials(t.Context(), params)
	require.NoError(t, err)
	require.Equal(t, int64(2), n, "a server is not excluded just because one lineage has an immutable choice")
	require.Equal(t, before, f.selection(issuer, immutable, server))
	for _, credential := range []uuid.UUID{replaceable, missing} {
		choice := f.selection(issuer, credential, server)
		require.False(t, choice.NoAuth)
		require.Equal(t, uuid.NullUUID{UUID: registration, Valid: true}, choice.RemoteSessionClientID)
		require.Equal(t, upstreamclients.SelectionSourceBackfill, choice.Source)
		require.Equal(t, uuid.NullUUID{UUID: typed, Valid: true}, f.namedAuthClient(credential, server).AuthClientID)
	}
	n, err = q.RecordUpstreamClientSelectionsForCredentials(t.Context(), params)
	require.NoError(t, err)
	require.Zero(t, n, "all immutable choices make the batch idempotent")
}

// observeCredentialBatchWrites counts shared SQL invocations, not credential
// insert rows: lifecycle work must stay bounded by live pages and scope groups.
type observeCredentialBatchWrites struct {
	pgx.Tx
	writes *int
}

func (tx observeCredentialBatchWrites) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(query, "-- name: RecordUpstreamClientSelectionsForCredentials ") {
		*tx.writes++
	}
	tag, err := tx.Tx.Exec(ctx, query, args...) //nolint:glint // notestingrawsql: batch instrumentation delegates dynamically intercepted SQLc statements.
	if err != nil {
		return tag, fmt.Errorf("execute observed credential batch: %w", err)
	}
	return tag, nil
}

func TestReconcileIssuerScopeBatchesEachLivePageByReach(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.organizationIssuer()
	firstRegistration, secondRegistration := f.client(issuer), f.client(issuer)
	affected, untouched := f.server(issuer, firstRegistration), f.server(issuer, secondRegistration)
	f.authClient(authclientbackfill.TypeRemoteSession, firstRegistration, affected)
	f.authClient(authclientbackfill.TypeRemoteSession, secondRegistration, untouched)
	credentials := make([]uuid.UUID, 0, 2*defaultPageSize+5)
	for range 2*defaultPageSize + 5 {
		credentials = append(credentials, f.session(issuer))
	}
	slices.SortFunc(credentials, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	// Interleave the two reach groups so every bounded page has both, including
	// the final partial page. This also checks cursoring while rows are inserted.
	moved := make([]uuid.UUID, 0, len(credentials)/2)
	for i, credential := range credentials {
		if i%2 == 1 {
			moved = append(moved, credential)
		}
	}
	tx := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, authclientbackfill.LockEMAWriterScope(t.Context(), tx, f.orgID, []uuid.UUID{f.projectID}))
	require.NoError(t, authclientbackfill.PreflightServerAssociations(t.Context(), tx, f.orgID, affected, untouched))
	writes := 0
	skipped, err := ReconcileIssuerScopeInLockedTransaction(t.Context(), observeCredentialBatchWrites{Tx: tx, writes: &writes}, f.orgID, issuer, []uuid.UUID{affected}, moved)
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Equal(t, 6, writes, "405 lineages need two shared statements per page, not one per lineage")
	require.NoError(t, tx.Commit(t.Context()))
	for i, credential := range credentials {
		choices := f.selections(credential)
		require.Equal(t, firstRegistration, choices[affected])
		if i%2 == 1 {
			require.Len(t, choices, 2, "moved lineages retain full target reach")
			require.Equal(t, secondRegistration, choices[untouched])
		} else {
			require.Len(t, choices, 1, "existing target lineages stay within maintenance scope")
		}
	}
}
