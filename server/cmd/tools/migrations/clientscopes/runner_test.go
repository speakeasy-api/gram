package clientscopes

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

var (
	cutoff       = time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC)
	beforeCutoff = cutoff.Add(-24 * time.Hour)
	issuerScopes = []string{"read", "write", "admin"}
)

type fixture struct {
	pool     *pgxpool.Pool
	orgID    string
	project  uuid.UUID
	issuerID uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool, err := testInfra.CloneTestDatabase(t, "clientscopes")
	require.NoError(t, err)
	q := New(pool)
	orgID := "org_" + uuid.NewString()
	require.NoError(t, q.SeedOrganizationFixture(t.Context(), orgID))
	project, err := q.SeedProjectFixture(t.Context(), SeedProjectFixtureParams{Slug: "p-" + uuid.NewString()[:8], OrganizationID: orgID})
	require.NoError(t, err)
	issuerID, err := q.SeedIssuerFixture(t.Context(), SeedIssuerFixtureParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, Slug: "issuer", Issuer: "https://issuer.example", ScopesSupported: issuerScopes})
	require.NoError(t, err)
	return &fixture{pool: pool, orgID: orgID, project: project, issuerID: issuerID}
}

func (f *fixture) client(t *testing.T, scope []string, createdAt time.Time) uuid.UUID {
	t.Helper()
	id, err := New(f.pool).SeedClientFixture(t.Context(), SeedClientFixtureParams{
		ProjectID:             uuid.NullUUID{UUID: f.project, Valid: true},
		OrganizationID:        pgtype.Text{String: f.orgID, Valid: true},
		RemoteSessionIssuerID: f.issuerID,
		ClientID:              "client-" + uuid.NewString(),
		Scope:                 scope,
		CreatedAt:             pgtype.Timestamptz{Time: createdAt, InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	return id
}

func (f *fixture) bindResource(t *testing.T, clientID uuid.UUID, scopes []string) {
	t.Helper()
	require.NoError(t, New(f.pool).SeedBoundResourceFixture(t.Context(), SeedBoundResourceFixtureParams{
		ProjectID: f.project, OrganizationID: f.orgID, Url: "https://mcp.example/" + uuid.NewString(), ScopesSupported: scopes, Slug: "usi-" + uuid.NewString()[:8], ClientID: clientID,
	}))
}

func (f *fixture) auditUpdate(t *testing.T, clientID uuid.UUID, before, after []string) {
	t.Helper()
	snapshot := func(scope []string) []byte {
		b, err := json.Marshal(map[string]any{"Scope": scope})
		require.NoError(t, err)
		return b
	}
	require.NoError(t, New(f.pool).SeedClientUpdateAuditFixture(t.Context(), SeedClientUpdateAuditFixtureParams{
		OrganizationID: f.orgID, ProjectID: uuid.NullUUID{UUID: f.project, Valid: true}, SubjectID: clientID.String(), BeforeSnapshot: snapshot(before), AfterSnapshot: snapshot(after),
	}))
}

func (f *fixture) scope(t *testing.T, clientID uuid.UUID) []string {
	t.Helper()
	scope, err := New(f.pool).GetClientScopeFixture(t.Context(), clientID)
	require.NoError(t, err)
	return scope
}

func outcomes(report Report) map[uuid.UUID]Outcome {
	out := map[uuid.UUID]Outcome{}
	for _, row := range report.Rows {
		out[row.ClientID] = row.Outcome
	}
	return out
}

func TestRunClassifiesCopiedScopes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	issuerCopy := f.client(t, []string{"admin", "read", "write", "read"}, beforeCutoff)
	resourceCopy := f.client(t, []string{"read"}, beforeCutoff)
	f.bindResource(t, resourceCopy, []string{"read"})
	subset := f.client(t, []string{"read", "write"}, beforeCutoff)
	edited := f.client(t, issuerScopes, beforeCutoff)
	f.auditUpdate(t, edited, nil, issuerScopes)
	otherEdit := f.client(t, issuerScopes, beforeCutoff)
	f.auditUpdate(t, otherEdit, issuerScopes, issuerScopes)
	afterCutoff := f.client(t, issuerScopes, cutoff.Add(time.Hour))
	unset := f.client(t, nil, beforeCutoff)

	report, err := NewRunner(f.pool, Options{CreatedBefore: cutoff, PageSize: 2, Apply: false}).Run(t.Context())
	require.NoError(t, err)
	got := outcomes(report)
	require.Equal(t, OutcomeWouldClear, got[issuerCopy])
	require.Equal(t, OutcomeWouldClear, got[resourceCopy])
	require.Equal(t, OutcomeSkipUnmatched, got[subset])
	require.Equal(t, OutcomeSkipEdited, got[edited])
	require.Equal(t, OutcomeWouldClear, got[otherEdit])
	require.NotContains(t, got, afterCutoff)
	require.NotContains(t, got, unset)
	require.Equal(t, 5, report.Scanned)

	// A dry run writes nothing.
	require.ElementsMatch(t, []string{"admin", "read", "write", "read"}, f.scope(t, issuerCopy))
}

func TestRunApplyClearsAndAudits(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	copied := f.client(t, issuerScopes, beforeCutoff)
	chosen := f.client(t, []string{"read"}, beforeCutoff)

	report, err := NewRunner(f.pool, Options{CreatedBefore: cutoff, PageSize: 0, Apply: true}).Run(t.Context())
	require.NoError(t, err)
	got := outcomes(report)
	require.Equal(t, OutcomeCleared, got[copied])
	require.Equal(t, OutcomeSkipUnmatched, got[chosen])
	require.Nil(t, f.scope(t, copied))
	require.Equal(t, []string{"read"}, f.scope(t, chosen))
	for _, row := range report.Rows {
		if row.ClientID == copied {
			require.Equal(t, "issuer", row.Match)
			require.Equal(t, issuerScopes, row.Scope)
		}
	}

	entries, err := New(f.pool).ListClientAuditActionsFixture(t.Context(), copied.String())
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "remote-session-client:update", entries[0].Action)
	require.Equal(t, "system", entries[0].ActorType)
	require.Equal(t, Actor, entries[0].ActorID)
	type snapshot struct {
		Scope []string `json:"Scope"`
	}
	var before, after snapshot
	require.NoError(t, json.Unmarshal(entries[0].BeforeSnapshot, &before))
	require.NoError(t, json.Unmarshal(entries[0].AfterSnapshot, &after))
	require.Equal(t, issuerScopes, before.Scope)
	require.Nil(t, after.Scope)

	// A rerun finds nothing left to clear.
	again, err := NewRunner(f.pool, Options{CreatedBefore: cutoff, PageSize: 0, Apply: true}).Run(t.Context())
	require.NoError(t, err)
	require.Zero(t, again.Outcomes[OutcomeCleared])
	require.NotContains(t, outcomes(again), copied)
}

func TestClassifySkipsExcludedClients(t *testing.T) {
	t.Parallel()
	base := ListClientScopeCandidatesRow{OrganizationID: "org", MatchesIssuer: true}

	managed := base
	managed.Managed = true
	outcome, _ := Classify(managed)
	require.Equal(t, OutcomeSkipManaged, outcome)

	kind := base
	kind.ExcludedKind = true
	outcome, _ = Classify(kind)
	require.Equal(t, OutcomeSkipKind, outcome)

	global := base
	global.OrganizationID = ""
	outcome, _ = Classify(global)
	require.Equal(t, OutcomeSkipKind, outcome)

	outcome, match := Classify(base)
	require.Equal(t, OutcomeWouldClear, outcome)
	require.Equal(t, "issuer", match)
}
