//go:build demoseed_safety

package demoseed

import (
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
)

func TestDemoServerClientChoices(t *testing.T) {
	t.Parallel()
	testServerClientChoices(t, DefaultSpec())
}

func TestLocalServerClientChoices(t *testing.T) {
	t.Parallel()
	testServerClientChoices(t, LocalSpec())
}

// Every seeded credential carries the choice issuance would have recorded for
// each server it reaches, so a reseeded organization is already migrated and
// no seeded credential is asked to reconnect.
func testServerClientChoices(t *testing.T, spec Spec) {
	t.Helper()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	// The connection health sign-ins, and the two live organization gateway
	// sign-ins, reach each connection health server.
	held := &seeder{spec: spec, now: time.Now()}
	live := 0
	for _, session := range held.healthSessions() {
		if session.deleted == nil && session.refreshExpires.After(held.now) {
			live++
		}
	}
	for range 2 {
		seedLocalPostgres(ctx, t, db, spec)

		report, err := selectionbackfill.Run(ctx, db, selectionbackfill.Options{OrganizationID: spec.OrgID, Apply: false, PageSize: 0})
		require.NoError(t, err)
		require.Positive(t, report.Credentials, "the seed must leave live credentials to check")
		// Two live organization gateway sign-ins also reach the Auto-Configured
		// OAuth Server, Linear and Support Tools.
		require.Equal(t, 20+(live+2)*len(healthServers), report.Pairs[selectionbackfill.OutcomeSelected], "every active seeded sign-in's reachable servers")
		require.Empty(t, report.Gaps, "no seeded credential may be asked to reconnect")
		require.Len(t, report.Pairs, 1, "the seed must record every choice: %v", report.Pairs)
		require.Positive(t, report.Credentials, "the seed must leave live credentials to check")
		require.NotEmpty(t, report.Pairs, "the seed must leave server choices to check")
		require.Equal(t, report.Credentials, report.FullyCovered)
	}
}

// A connection health sign-in's choice on a server outside the provider fails postflight.
func TestMisassignedChoicesCoverConnectionHealthSessions(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	spec := DefaultSpec()
	seedLocalPostgres(ctx, t, db, spec)

	s := &seeder{spec: spec, now: time.Now()}
	q := repo.New(db)
	params := repo.PostflightIDMisassignedUpstreamClientChoicesParams{
		OrgSessionIds:      []uuid.UUID{s.det("org-gateway-session-1"), s.det("org-gateway-session-2"), s.det("org-gateway-session-3")},
		OrgNoAuthServerIds: append(s.healthServerIDs(), s.det("mcpserver-shadow-official")),
		ProjectID:          s.projectID(),
		HealthIssuerID:     s.healthIssuer(),
		LinearServerID:     s.det("mcpserver-linear"),
		LinearClientID:     s.det("remote-identity-client-linear"),
		SlackServerID:      s.det("mcpserver-slack"),
	}
	// The upstream auth migration moves some seeded choices to auth clients, so compare counts.
	before, err := q.PostflightIDMisassignedUpstreamClientChoices(ctx, params)
	require.NoError(t, err)

	session := uuid.Nil
	for n, held := range s.healthSessions() {
		if held.deleted == nil {
			session = s.det("health-session-" + strconv.Itoa(n))
			break
		}
	}
	require.NotEqual(t, uuid.Nil, session, "the seed holds a connection health sign-in")
	require.NoError(t, q.ServersInsertUpstreamClientChoice(ctx, repo.ServersInsertUpstreamClientChoiceParams{
		ID: uuid.New(), ProjectID: s.projectID(), OrgID: s.orgID(),
		CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: session, McpServerID: s.det("mcpserver-linear"),
		RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Source: upstreamclients.SelectionSourceIssuance,
	}))
	after, err := q.PostflightIDMisassignedUpstreamClientChoices(ctx, params)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "a connection health sign-in's choice on another server is misassigned")
}
