package executionprofiles

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var testInfra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	testInfra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type fixture struct {
	t         *testing.T
	pool      *pgxpool.Pool
	q         *Queries
	orgID     string
	projectID uuid.UUID
	assistant uuid.UUID
	trigger   uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool, err := testInfra.CloneTestDatabase(t, "executionprofiles")
	require.NoError(t, err)
	ctx := t.Context()
	f := &fixture{t: t, pool: pool, q: New(pool), orgID: "org_" + uuid.NewString(), projectID: uuid.New(), assistant: uuid.Nil, trigger: uuid.Nil}
	require.NoError(t, f.q.SeedOrganizationFixture(ctx, f.orgID))
	require.NoError(t, f.q.SeedProjectFixture(ctx, SeedProjectFixtureParams{ID: f.projectID, Slug: "p-" + f.projectID.String()[:8], OrganizationID: f.orgID}))
	f.assistant, err = f.q.SeedAssistantFixture(ctx, SeedAssistantFixtureParams{OrganizationID: f.orgID, ProjectID: f.projectID, Name: "assistant"})
	require.NoError(t, err)
	f.trigger, err = f.q.SeedTriggerFixture(ctx, SeedTriggerFixtureParams{OrganizationID: f.orgID, ProjectID: f.projectID, Name: "trigger"})
	require.NoError(t, err)
	return f
}

// server seeds a remote server the assistant reaches. stored is the
// server's stored requires_upstream_auth, which nothing reads.
func (f *fixture) server(stored pgtype.Bool) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	id, remoteID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteMCPServerFixture(ctx, SeedRemoteMCPServerFixtureParams{ID: remoteID, ProjectID: f.projectID, Url: "https://mcp.example.com/" + remoteID.String()[:8]}))
	require.NoError(f.t, f.q.SeedMCPServerFixture(ctx, SeedMCPServerFixtureParams{
		ID:                   id,
		ProjectID:            f.projectID,
		Slug:                 pgtype.Text{String: "s-" + id.String()[:8], Valid: true},
		RemoteMcpServerID:    uuid.NullUUID{UUID: remoteID, Valid: true},
		RequiresUpstreamAuth: stored,
	}))
	require.NoError(f.t, f.q.SeedAssistantServerFixture(ctx, SeedAssistantServerFixtureParams{AssistantID: f.assistant, McpServerID: id, ProjectID: f.projectID}))
	return id
}

// registration seeds an upstream OAuth client associated with serverID.
func (f *fixture) registration(serverID uuid.UUID) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	issuerID, clientID := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedRemoteSessionIssuerFixture(ctx, SeedRemoteSessionIssuerFixtureParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true},
		Slug:      "rsi-" + issuerID.String()[:8],
		Issuer:    "https://auth.example.com/" + issuerID.String()[:8],
	}))
	require.NoError(f.t, f.q.SeedRemoteSessionClientFixture(ctx, SeedRemoteSessionClientFixtureParams{
		ID:                    clientID,
		ProjectID:             uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID:        pgtype.Text{String: f.orgID, Valid: true},
		RemoteSessionIssuerID: issuerID,
		ClientID:              "cid-" + clientID.String()[:8],
	}))
	require.NoError(f.t, f.q.SeedAssociationFixture(ctx, SeedAssociationFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, McpServerID: serverID, RemoteSessionClientID: clientID}))
	return clientID
}

// authClient seeds an auth client of clientType linked to serverID.
func (f *fixture) authClient(serverID uuid.UUID, clientType string, registration uuid.UUID) uuid.UUID {
	f.t.Helper()
	ctx := f.t.Context()
	id, err := f.q.SeedAuthClientFixture(ctx, SeedAuthClientFixtureParams{
		OrganizationID:        f.orgID,
		ProjectID:             uuid.NullUUID{UUID: f.projectID, Valid: true},
		ClientType:            clientType,
		RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: registration != uuid.Nil},
	})
	require.NoError(f.t, err)
	require.NoError(f.t, f.q.LinkAuthClientFixture(ctx, LinkAuthClientFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, McpServerID: serverID, AuthClientID: id}))
	return id
}

// workloadBinding binds the trigger to the assistant's workload identity.
func (f *fixture) workloadBinding() {
	f.t.Helper()
	require.NoError(f.t, f.q.SeedWorkloadBindingFixture(f.t.Context(), SeedWorkloadBindingFixtureParams{
		OrganizationID: f.orgID, ProjectID: f.projectID, AssistantID: f.assistant, TriggerID: f.trigger,
	}))
}

func (f *fixture) bind(execCtx Context) Binding {
	f.t.Helper()
	binding, err := Bind(f.t.Context(), f.pool, Target{OrganizationID: f.orgID, ProjectID: f.projectID, AssistantID: f.assistant, TriggerID: f.trigger, Context: execCtx}, SourceIssuance)
	require.NoError(f.t, err)
	return binding
}

func (f *fixture) choices(versionID uuid.UUID) map[uuid.UUID]uuid.UUID {
	f.t.Helper()
	rows, err := f.q.ListVersionChoicesFixture(f.t.Context(), versionID)
	require.NoError(f.t, err)
	out := map[uuid.UUID]uuid.UUID{}
	for _, row := range rows {
		require.Equal(f.t, row.NoAuth, !row.AuthClientID.Valid)
		out[row.McpServerID] = row.AuthClientID.UUID
	}
	return out
}

var unstored = pgtype.Bool{Bool: false, Valid: false}

func TestBindDecidesOnlyWhatConfigurationDecides(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	personal := f.server(unstored)
	personalClient := f.authClient(personal, typeRemoteSession, f.registration(personal))
	shared := f.server(unstored)
	sharedClient := f.authClient(shared, typeServiceAccount, f.registration(shared))
	headers := f.server(unstored)
	headerClient := f.authClient(headers, typeStaticCredential, uuid.Nil)
	open := f.server(unstored)
	// A stored requirement is never read: a server with no auth client needs
	// none.
	storedRequired := f.server(pgtype.Bool{Bool: true, Valid: true})
	// A stored no-auth requirement never overrides an enabled client.
	storedNone := f.server(pgtype.Bool{Bool: false, Valid: true})
	storedNoneClient := f.authClient(storedNone, typeStaticCredential, uuid.Nil)
	disabled := f.server(unstored)
	require.NoError(t, f.q.DisableAuthClientFixture(t.Context(), DisableAuthClientFixtureParams{ID: f.authClient(disabled, typeStaticCredential, uuid.Nil), OrganizationID: f.orgID}))
	detached := f.server(unstored)
	require.NoError(t, f.q.DetachAuthClientFixture(t.Context(), DetachAuthClientFixtureParams{McpServerID: detached, AuthClientID: f.authClient(detached, typeServiceAccount, f.registration(detached)), OrganizationID: f.orgID}))
	// An upstream OAuth client no auth client records yet.
	pending := f.server(unstored)
	f.registration(pending)
	several := f.server(unstored)
	f.authClient(several, typeStaticCredential, uuid.Nil)
	f.authClient(several, typePassThrough, uuid.Nil)

	binding := f.bind(ContextUserBacked)
	require.Equal(t, int64(1), binding.Version)
	require.Equal(t, map[uuid.UUID]uuid.UUID{
		personal:       personalClient,
		shared:         sharedClient,
		headers:        headerClient,
		open:           uuid.Nil,
		storedRequired: uuid.Nil,
		storedNone:     storedNoneClient,
		disabled:       uuid.Nil,
		detached:       uuid.Nil,
	}, f.choices(binding.VersionID))
	require.ElementsMatch(t, []Gap{
		{MCPServerID: pending, Reason: GapSetupIncomplete},
		{MCPServerID: several, Reason: GapSelectionRequired},
	}, binding.Gaps)
}

func TestBindTreatsEMAAsPersonal(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server(unstored)
	registration := f.registration(server)
	emaClient := f.authClient(server, typeEMA, registration)

	userless := f.bind(ContextUserless)
	require.Empty(t, f.choices(userless.VersionID))
	require.Equal(t, []Gap{{MCPServerID: server, Reason: GapPersonalRequiresUser}}, userless.Gaps)

	userBacked := f.bind(ContextUserBacked)
	require.Equal(t, map[uuid.UUID]uuid.UUID{server: emaClient}, f.choices(userBacked.VersionID))

}

func TestBindTreatsAnEMAAndAPersonalClientAsAmbiguous(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server(unstored)
	registration := f.registration(server)
	f.authClient(server, typeRemoteSession, registration)
	f.authClient(server, typeEMA, registration)

	userBacked := f.bind(ContextUserBacked)
	require.Empty(t, f.choices(userBacked.VersionID))
	require.Equal(t, []Gap{{MCPServerID: server, Reason: GapSelectionRequired}}, userBacked.Gaps)

	userless := f.bind(ContextUserless)
	require.Equal(t, []Gap{{MCPServerID: server, Reason: GapPersonalRequiresUser}}, userless.Gaps)
}

func TestBindConcurrentTurnsDecideOneVersion(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.server(unstored)
	f.authClient(first, typeServiceAccount, f.registration(first))
	v1 := f.bind(ContextUserBacked)
	second := f.server(unstored)
	f.authClient(second, typeServiceAccount, f.registration(second))

	const turns = 4
	results := make(chan Binding, turns)
	errs := make(chan error, turns)
	start := make(chan struct{})
	for range turns {
		go func() {
			<-start
			binding, err := Bind(t.Context(), f.pool, Target{OrganizationID: f.orgID, ProjectID: f.projectID, AssistantID: f.assistant, TriggerID: f.trigger, Context: ContextUserBacked}, SourceIssuance)
			results <- binding
			errs <- err
		}()
	}
	close(start)
	written := 0
	for range turns {
		require.NoError(t, <-errs)
		binding := <-results
		require.Equal(t, v1.Version+1, binding.Version, "every turn binds the one new version")
		if binding.Written {
			written++
		}
	}
	require.Equal(t, 1, written, "exactly one turn records it")
}

func TestBackfillDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	server := f.server(unstored)
	f.authClient(server, typeServiceAccount, f.registration(server))
	f.workloadBinding()

	dry, err := Backfill(t.Context(), f.pool, f.orgID, false)
	require.NoError(t, err)
	require.Equal(t, 1, dry.Targets)
	require.Equal(t, len(Contexts), dry.VersionsWritten)
	for _, execCtx := range Contexts {
		_, err := f.q.GetProfile(t.Context(), GetProfileParams{
			OrganizationID: f.orgID, ProjectID: f.projectID, AssistantID: f.assistant,
			TriggerInstanceID: f.trigger, ExecutionContext: string(execCtx),
		})
		require.ErrorIs(t, err, pgx.ErrNoRows, "a dry run creates no profile")
	}

	applied, err := Backfill(t.Context(), f.pool, f.orgID, true)
	require.NoError(t, err)
	require.Equal(t, len(Contexts), applied.VersionsWritten)

	again, err := Backfill(t.Context(), f.pool, f.orgID, false)
	require.NoError(t, err)
	require.Zero(t, again.VersionsWritten)
}

func TestBindNeverGivesAUserlessTurnAPersonalClient(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	personal := f.server(unstored)
	f.authClient(personal, typeRemoteSession, f.registration(personal))
	shared := f.server(unstored)
	registration := f.registration(shared)
	f.authClient(shared, typeRemoteSession, registration)
	sharedClient := f.authClient(shared, typeServiceAccount, registration)

	binding := f.bind(ContextUserless)
	require.Equal(t, map[uuid.UUID]uuid.UUID{shared: sharedClient}, f.choices(binding.VersionID))
	require.Equal(t, []Gap{{MCPServerID: personal, Reason: GapPersonalRequiresUser}}, binding.Gaps)

	userBacked := f.bind(ContextUserBacked)
	require.NotEqual(t, binding.VersionID, userBacked.VersionID, "each context has its own profile")
	require.Contains(t, userBacked.Gaps, Gap{MCPServerID: shared, Reason: GapSelectionRequired}, "a user-backed turn could use either, so it is not guessed")
}

func TestBindKeepsEveryChoiceAndVersionsOnlyNewOnes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first := f.server(unstored)
	firstClient := f.authClient(first, typeServiceAccount, f.registration(first))

	v1 := f.bind(ContextUserBacked)
	again := f.bind(ContextUserBacked)
	require.Equal(t, v1.VersionID, again.VersionID, "nothing new to decide binds the same version")

	// A second client on the first server would make it ambiguous now, but
	// its choice is kept.
	f.authClient(first, typeServiceAccount, f.registration(first))
	second := f.server(unstored)
	secondClient := f.authClient(second, typeServiceAccount, f.registration(second))

	v2 := f.bind(ContextUserBacked)
	require.Equal(t, int64(2), v2.Version)
	require.Equal(t, map[uuid.UUID]uuid.UUID{first: firstClient, second: secondClient}, f.choices(v2.VersionID))
	require.Equal(t, map[uuid.UUID]uuid.UUID{first: firstClient}, f.choices(v1.VersionID), "an earlier version never changes")
}
