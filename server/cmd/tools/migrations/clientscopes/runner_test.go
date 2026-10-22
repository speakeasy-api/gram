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
	issuerID uuid.UUID
}

type clientSpec struct {
	project        uuid.NullUUID
	scope          []string
	createdAt      time.Time
	legacyCallback bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool, err := testInfra.CloneTestDatabase(t, "clientscopes")
	require.NoError(t, err)
	f := &fixture{pool: pool, orgID: "org_" + uuid.NewString(), issuerID: uuid.Nil}
	require.NoError(t, New(pool).SeedOrganizationFixture(t.Context(), f.orgID))
	f.issuerID, err = New(pool).SeedIssuerFixture(t.Context(), SeedIssuerFixtureParams{ProjectID: uuid.NullUUID{UUID: f.project(t), Valid: true}, Slug: "issuer", Issuer: "https://issuer.example", ScopesSupported: issuerScopes})
	require.NoError(t, err)
	return f
}

func (f *fixture) project(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := New(f.pool).SeedProjectFixture(t.Context(), SeedProjectFixtureParams{Slug: "p-" + uuid.NewString()[:8], OrganizationID: f.orgID})
	require.NoError(t, err)
	return id
}

func (f *fixture) client(t *testing.T, spec clientSpec) uuid.UUID {
	t.Helper()
	org := pgtype.Text{String: f.orgID, Valid: !spec.project.Valid}
	id, err := New(f.pool).SeedClientFixture(t.Context(), SeedClientFixtureParams{
		ProjectID:             spec.project,
		OrganizationID:        org,
		RemoteSessionIssuerID: f.issuerID,
		ClientID:              "client-" + uuid.NewString(),
		Scope:                 spec.scope,
		LegacyCallbackUrl:     spec.legacyCallback,
		CreatedAt:             pgtype.Timestamptz{Time: spec.createdAt, InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	return id
}

// projectClient is a project client created before the cutoff and linked to a fresh user session issuer.
func (f *fixture) projectClient(t *testing.T, project uuid.UUID, scope []string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	id := f.client(t, clientSpec{project: uuid.NullUUID{UUID: project, Valid: true}, scope: scope, createdAt: beforeCutoff, legacyCallback: false})
	return id, f.link(t, project, id)
}

func (f *fixture) link(t *testing.T, project, clientID uuid.UUID) uuid.UUID {
	t.Helper()
	usi, err := New(f.pool).SeedUserSessionIssuerFixture(t.Context(), SeedUserSessionIssuerFixtureParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, Slug: "usi-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	f.linkTo(t, clientID, usi)
	return usi
}

func (f *fixture) linkTo(t *testing.T, clientID, usi uuid.UUID) {
	t.Helper()
	require.NoError(t, New(f.pool).LinkClientFixture(t.Context(), LinkClientFixtureParams{ClientID: clientID, UserSessionIssuerID: usi}))
}

func (f *fixture) server(t *testing.T, project, usi uuid.UUID, scopes []string) {
	t.Helper()
	require.NoError(t, New(f.pool).SeedServerResourceFixture(t.Context(), SeedServerResourceFixtureParams{
		ProjectID: project, OrganizationID: f.orgID, Url: "https://mcp.example/" + uuid.NewString(), ScopesSupported: scopes, UserSessionIssuerID: uuid.NullUUID{UUID: usi, Valid: true},
	}))
}

func (f *fixture) auditUpdate(t *testing.T, project, clientID uuid.UUID, before, after []byte) {
	t.Helper()
	require.NoError(t, New(f.pool).SeedClientUpdateAuditFixture(t.Context(), SeedClientUpdateAuditFixtureParams{
		OrganizationID: f.orgID, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, SubjectID: clientID.String(), BeforeSnapshot: before, AfterSnapshot: after,
	}))
}

func scopeSnapshot(t *testing.T, scope []string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"Scope": scope})
	require.NoError(t, err)
	return b
}

func (f *fixture) scope(t *testing.T, clientID uuid.UUID) []string {
	t.Helper()
	scope, err := New(f.pool).GetClientScopeFixture(t.Context(), clientID)
	require.NoError(t, err)
	return scope
}

func (f *fixture) run(t *testing.T, apply bool) Report {
	t.Helper()
	report, err := NewRunner(f.pool, Options{CreatedBefore: cutoff, PageSize: 2, Apply: apply}).Run(t.Context())
	require.NoError(t, err)
	return report
}

func outcomes(report Report) map[uuid.UUID]Outcome {
	out := map[uuid.UUID]Outcome{}
	for _, row := range report.Rows {
		out[row.ClientID] = row.Outcome
	}
	return out
}

func TestRunClassifiesCandidates(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	project := f.project(t)
	q := New(f.pool)

	issuerCopy, _ := f.projectClient(t, project, []string{"admin", "read", "write", "read"})

	resourceCopy, usi := f.projectClient(t, project, []string{"read"})
	f.server(t, project, usi, []string{"read"})

	// A pin on the resource would change what the login requests after the clear.
	pinProject := f.project(t)
	pinned, pinUsi := f.projectClient(t, pinProject, []string{"read"})
	f.server(t, pinProject, pinUsi, []string{"read"})
	require.NoError(t, q.SetResourcePinFixture(t.Context(), SetResourcePinFixtureParams{ScopeOverride: []string{"read", "admin"}, ProjectID: pinProject}))

	// Two upstreams and a sibling serving them: the login reads no resource, so it would fall to the issuer list.
	notOwned, sharedUsi := f.projectClient(t, project, []string{"files"})
	f.server(t, project, sharedUsi, []string{"files"})
	f.server(t, project, sharedUsi, []string{"other"})
	sibling := f.client(t, clientSpec{project: uuid.NullUUID{UUID: project, Valid: true}, scope: nil, createdAt: beforeCutoff, legacyCallback: false})
	f.linkTo(t, sibling, sharedUsi)

	// A resource in another project of the client's organization does not count.
	foreign, foreignUsi := f.projectClient(t, project, []string{"mail"})
	f.server(t, f.project(t), foreignUsi, []string{"mail"})

	subset, _ := f.projectClient(t, project, []string{"read", "write"})

	edited, _ := f.projectClient(t, project, issuerScopes)
	f.auditUpdate(t, project, edited, scopeSnapshot(t, nil), scopeSnapshot(t, issuerScopes))
	missingSnapshot, _ := f.projectClient(t, project, issuerScopes)
	f.auditUpdate(t, project, missingSnapshot, nil, scopeSnapshot(t, issuerScopes))
	otherEdit, _ := f.projectClient(t, project, issuerScopes)
	f.auditUpdate(t, project, otherEdit, scopeSnapshot(t, issuerScopes), scopeSnapshot(t, issuerScopes))

	unlinked := f.client(t, clientSpec{project: uuid.NullUUID{UUID: project, Valid: true}, scope: issuerScopes, createdAt: beforeCutoff, legacyCallback: false})

	signIn, signInUsi := f.projectClient(t, project, issuerScopes)
	require.NoError(t, q.TrustClientFixture(t.Context(), TrustClientFixtureParams{ClientID: uuid.NullUUID{UUID: signIn, Valid: true}, ID: signInUsi}))

	legacy := f.client(t, clientSpec{project: uuid.NullUUID{UUID: project, Valid: true}, scope: issuerScopes, createdAt: beforeCutoff, legacyCallback: true})
	f.link(t, project, legacy)

	orgClient := f.client(t, clientSpec{project: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, scope: issuerScopes, createdAt: beforeCutoff, legacyCallback: false})
	f.link(t, project, orgClient)

	afterCutoff := f.client(t, clientSpec{project: uuid.NullUUID{UUID: project, Valid: true}, scope: issuerScopes, createdAt: cutoff.Add(time.Hour), legacyCallback: false})
	unset, _ := f.projectClient(t, project, nil)

	report := f.run(t, false)
	got := outcomes(report)
	require.Equal(t, map[uuid.UUID]Outcome{
		issuerCopy:      OutcomeWouldClear,
		resourceCopy:    OutcomeWouldClear,
		pinned:          OutcomeSkipRequestChanges,
		notOwned:        OutcomeSkipRequestChanges,
		foreign:         OutcomeSkipUnmatched,
		subset:          OutcomeSkipUnmatched,
		edited:          OutcomeSkipEdited,
		missingSnapshot: OutcomeSkipEdited,
		otherEdit:       OutcomeWouldClear,
		unlinked:        OutcomeSkipUnlinked,
		signIn:          OutcomeSkipSignIn,
		legacy:          OutcomeSkipKind,
		orgClient:       OutcomeWouldClear,
	}, got)
	require.NotContains(t, got, afterCutoff)
	require.NotContains(t, got, unset)
	for _, row := range report.Rows {
		switch row.ClientID {
		case issuerCopy:
			require.Equal(t, "issuer", row.Match)
		case resourceCopy, notOwned:
			require.Equal(t, "resource", row.Match)
		}
	}

	// A dry run writes nothing.
	require.Equal(t, []string{"admin", "read", "write", "read"}, f.scope(t, issuerCopy))
}

func TestRunApplyClearsAuditsAndRestores(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	project := f.project(t)
	copied, _ := f.projectClient(t, project, []string{"write", "read", "admin"})
	orgClient := f.client(t, clientSpec{project: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, scope: issuerScopes, createdAt: beforeCutoff, legacyCallback: false})
	f.link(t, project, orgClient)
	chosen, _ := f.projectClient(t, project, []string{"read"})

	report := f.run(t, true)
	got := outcomes(report)
	require.Equal(t, OutcomeCleared, got[copied])
	require.Equal(t, OutcomeCleared, got[orgClient])
	require.Equal(t, OutcomeSkipUnmatched, got[chosen])
	require.Nil(t, f.scope(t, copied))
	require.Nil(t, f.scope(t, orgClient))
	require.Equal(t, []string{"read"}, f.scope(t, chosen))

	entries, err := New(f.pool).ListClientAuditActionsFixture(t.Context(), copied.String())
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "remote-session-client:update", entries[0].Action)
	require.Equal(t, "system", entries[0].ActorType)
	require.Equal(t, ActorComponent, entries[0].ActorID)
	type snapshot struct {
		Scope []string `json:"Scope"`
	}
	var before, after snapshot
	require.NoError(t, json.Unmarshal(entries[0].BeforeSnapshot, &before))
	require.NoError(t, json.Unmarshal(entries[0].AfterSnapshot, &after))
	require.Equal(t, []string{"write", "read", "admin"}, before.Scope)
	require.Nil(t, after.Scope)

	again := f.run(t, true)
	require.Zero(t, again.Outcomes[OutcomeCleared])

	preview, err := NewRunner(f.pool, Options{CreatedBefore: time.Time{}, PageSize: 0, Apply: false}).Restore(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, preview.Outcomes[OutcomeWouldRestore])
	require.Nil(t, f.scope(t, copied))

	restored, err := NewRunner(f.pool, Options{CreatedBefore: time.Time{}, PageSize: 0, Apply: true}).Restore(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, restored.Outcomes[OutcomeRestored])
	require.Equal(t, []string{"write", "read", "admin"}, f.scope(t, copied))
	require.Equal(t, issuerScopes, f.scope(t, orgClient))
	entries, err = New(f.pool).ListClientAuditActionsFixture(t.Context(), copied.String())
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, ActorComponent, entries[1].ActorID)

	// A second restore leaves set scopes alone, and the backfill treats the restore as an edit.
	twice, err := NewRunner(f.pool, Options{CreatedBefore: time.Time{}, PageSize: 0, Apply: true}).Restore(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, twice.Outcomes[OutcomeSkipChanged])
	require.Equal(t, OutcomeSkipEdited, outcomes(f.run(t, false))[copied])
}

func TestClearSkipsClientGoneAfterScan(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	project := f.project(t)
	copied, _ := f.projectClient(t, project, issuerScopes)
	runner := NewRunner(f.pool, Options{CreatedBefore: cutoff, PageSize: 0, Apply: true})
	rows, err := New(f.pool).ListClientScopeCandidates(t.Context(), runner.listParams(uuid.Nil, uuid.NullUUID{UUID: copied, Valid: true}))
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.NoError(t, New(f.pool).SoftDeleteClientFixture(t.Context(), copied))
	outcome, _, _, err := runner.clear(t.Context(), rows[0])
	require.NoError(t, err)
	require.Equal(t, OutcomeSkipChanged, outcome)
	require.Equal(t, issuerScopes, f.scope(t, copied))
}

func TestClassifyOrder(t *testing.T) {
	t.Parallel()
	base := ListClientScopeCandidatesRow{OrganizationID: "org", Linked: true, Scope: []string{"a"}, IssuerScopesSupported: []string{"a"}}
	neutral := []Login{{Request: []string{"a"}, ResourceCopy: false}}

	managed := base
	managed.Managed = true
	outcome, _ := Classify(managed, neutral)
	require.Equal(t, OutcomeSkipManaged, outcome)

	global := base
	global.OrganizationID = ""
	outcome, _ = Classify(global, neutral)
	require.Equal(t, OutcomeSkipKind, outcome)

	chaining := base
	chaining.IdentityChaining = true
	outcome, _ = Classify(chaining, neutral)
	require.Equal(t, OutcomeSkipIdentityChaining, outcome)

	omitted := []Login{{Request: nil, ResourceCopy: false}}
	outcome, _ = Classify(base, omitted)
	require.Equal(t, OutcomeSkipRequestChanges, outcome)

	outcome, match := Classify(base, neutral)
	require.Equal(t, OutcomeWouldClear, outcome)
	require.Equal(t, "issuer", match)
}
