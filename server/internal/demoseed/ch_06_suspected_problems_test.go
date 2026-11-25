//go:build demoseed_safety

package demoseed

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
	healthrepo "github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The seeded connection health lists the Cursor, Internal Wiki, Dana and Support Desk problems, and not
// the people who recovered from this morning's Linear errors: at the hours around the
// story's switch to today, any other hour, and 23 hours after seeding, the read just
// before the next daily reseed. Each weekday is seeded, since weekends change the baseline's draws.
func TestSeededConnectionHealthListsTheIntendedProblems(t *testing.T) {
	t.Parallel()
	// On this Monday the baseline served a Felix/Notion request after his scripted failure.
	t.Run("weekday traffic does not heal Cursor's Notion failure", func(t *testing.T) {
		t.Parallel()
		seeded := time.Date(2026, 11, 23, 11, 46, 0, 0, time.Local).UTC()
		seededProblems(t, seeded, seeded.Add(time.Minute))
	})
	for daysAgo := 2; daysAgo < 9; daysAgo++ {
		local := time.Now().AddDate(0, 0, -daysAgo)
		at := func(hour, minute int) time.Time {
			return time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, time.Local).UTC()
		}
		for _, c := range []struct {
			name      string
			seeded    time.Time
			readAfter time.Duration
		}{
			{"just before the switch", at(11, 44), time.Minute},
			{"just after the switch", at(11, 46), time.Minute},
			{"just after midnight", at(0, 5), time.Minute},
			{"evening", at(18, 0), time.Minute},
			{"23 hours later", at(11, 46), 23 * time.Hour},
			{"23 hours after an evening seed", at(18, 0), 23 * time.Hour},
		} {
			t.Run(local.Weekday().String()+"/"+c.name, func(t *testing.T) {
				t.Parallel()
				seededProblems(t, c.seeded, c.seeded.Add(c.readAfter))
			})
		}
	}
}

// The problems read counts failures only from when logs were last turned on, so a fresh
// seed must not hide its own problems behind the logs feature it just created.
func TestSeededProblemsShowRightAfterAFreshSeed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	spec := DefaultSpec()
	spec.OrgID = "org-fresh-" + uuid.NewString()
	spec.UUIDPrefix = strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	require.NoError(t, catalogseed.EnsureCatalog(ctx, db))
	s, err := seedPostgres(ctx, logger, db, assetstest.NewTestBlobStore(t), testenv.NewEncryptionClient(t), testAssistantIssuerURL, testServerURL, spec, aivendors.CompiledSnapshot())
	require.NoError(t, err)

	rows, served, err := s.connectionHealthRows()
	require.NoError(t, err)
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	func() {
		otherTenantWrites.RLock()
		defer otherTenantWrites.RUnlock()
		require.NoError(t, mcpconnectionattempts.AppendRows(ctx, ch, rows))
		require.NoError(t, mcpconnectionattempts.AppendServed(ctx, ch, served))
	}()

	since, err := healthrepo.New(db).GetLogsOnSince(ctx, healthrepo.GetLogsOnSinceParams{OrganizationID: s.orgID(), FeatureName: string(productfeatures.FeatureLogs)})
	require.NoError(t, err)
	servers := []string{s.healthGateway().String()}
	for _, id := range s.healthServerIDs() {
		servers = append(servers, id.String())
	}
	got, err := problems.Read(ctx, ch, db, mcpconnectionattemptstest.LogsOn, problems.ReadScope{
		OrganizationID: s.orgID(), ProjectID: s.projectID(), WholeOrganization: false, IssuerID: s.healthIssuer(), IssuerServerIDs: servers,
	}, since.Time, s.now.Add(time.Minute))
	require.NoError(t, err)
	scopes := make([]string, 0, len(got))
	for _, p := range got {
		scopes = append(scopes, p.Scope.Key)
	}
	require.ElementsMatch(t, []string{
		mcpconnectionattempts.FamilyCursor + "@2.0.1",
		s.healthServerID("wiki").String(),
		s.healthServerID("support").String(),
		urn.NewUserSubject(s.userID("dana_reyes")).String(),
	}, scopes, "logs on since %s", since.Time)
}

func seededProblems(t *testing.T, seeded, now time.Time) {
	t.Helper()
	spec := DefaultSpec()
	spec.OrgID = "org-problems-" + uuid.NewString()
	spec.UUIDPrefix = strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	s := &seeder{spec: spec, catalog: aivendors.CompiledSnapshot(), now: seeded}

	rows, served, err := s.connectionHealthRows()
	require.NoError(t, err)
	// The daily refusals of every reason, on the other providers' servers, list no problem.
	requestRows, err := s.connectionRequestRows()
	require.NoError(t, err)
	rows = append(rows, requestRows...)
	served = append(served, s.connectionRequestServed(requestRows)...)
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	// Other tenants' rows must not land while the safety test fingerprints them.
	func() {
		otherTenantWrites.RLock()
		defer otherTenantWrites.RUnlock()
		require.NoError(t, mcpconnectionattempts.AppendRows(t.Context(), ch, rows))
		require.NoError(t, mcpconnectionattempts.AppendServed(t.Context(), ch, served))
	}()

	// Read as the provider's page does: its project, its servers and gateway.
	servers := []string{s.healthGateway().String()}
	for _, id := range s.healthServerIDs() {
		servers = append(servers, id.String())
	}
	got, err := problems.Read(t.Context(), ch, db, mcpconnectionattemptstest.LogsOn, problems.ReadScope{
		OrganizationID: s.orgID(), ProjectID: s.projectID(), WholeOrganization: false, IssuerID: s.healthIssuer(), IssuerServerIDs: servers,
	}, time.Time{}, now)
	require.NoError(t, err)
	signIn := 0
	for _, p := range got {
		if p.Part == mcpconnectionattempts.PartAppSignIn {
			signIn++
		}
	}
	require.Equal(t, 3, signIn, "the provider Overview lists three problems signing in, as the review steps say")

	// On a provider page owning every server the daily refusals reach, their person is in no problem.
	var refusedServers []string
	for _, r := range requestRows {
		if r.ServerID != "" && !slices.Contains(refusedServers, r.ServerID) {
			refusedServers = append(refusedServers, r.ServerID)
		}
	}
	elsewhere, err := problems.Read(t.Context(), ch, db, mcpconnectionattemptstest.LogsOn, problems.ReadScope{
		OrganizationID: s.orgID(), ProjectID: s.projectID(), WholeOrganization: false, IssuerID: uuid.New(), IssuerServerIDs: refusedServers,
	}, time.Time{}, now)
	require.NoError(t, err)
	require.Empty(t, elsewhere, "the daily refusals end in answered requests")

	subject := func(key string) string {
		for _, p := range healthPeople {
			if p.key == key {
				return urn.NewUserSubject(s.userID(p.handle())).String()
			}
		}
		t.Fatalf("unknown person %s", key)
		return ""
	}
	people := func(keys ...string) []string {
		out := make([]string, len(keys))
		for i, k := range keys {
			out[i] = subject(k)
		}
		slices.Sort(out)
		return out
	}
	serverIDs := func(keys ...string) []string {
		out := make([]string, len(keys))
		for i, k := range keys {
			out[i] = s.healthServerID(k).String()
		}
		slices.Sort(out)
		return out
	}
	type listed struct {
		Rule    problems.Rule
		Scope   problems.Scope
		People  []string
		Servers []string
	}
	var listedProblems []listed
	for _, p := range got {
		var subjects, onServers []string
		for _, e := range p.Episodes {
			if !slices.Contains(subjects, e.SubjectURN) {
				subjects = append(subjects, e.SubjectURN)
			}
			if !slices.Contains(onServers, e.ServerID) {
				onServers = append(onServers, e.ServerID)
			}
		}
		slices.Sort(subjects)
		slices.Sort(onServers)
		listedProblems = append(listedProblems, listed{Rule: p.Rule, Scope: p.Scope, People: subjects, Servers: onServers})
	}
	require.ElementsMatch(t, []listed{
		{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyCursor + "@2.0.1"}, People: people("lena", "felix", "lars"), Servers: serverIDs("github", "notion")},
		{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: s.healthServerID("wiki").String()}, People: people("ken", "ivy", "theo", "amara"), Servers: serverIDs("wiki")},
		{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: subject("dana")}, People: people("dana"), Servers: serverIDs("linear")},
		{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: s.healthServerID("support").String()}, People: people("ana", "mei"), Servers: serverIDs("support")},
	}, listedProblems, "an app version takes only servers where another app worked: Claude Code on GitHub and on Notion")

	// The Project Overview reads the whole project and lists the same problems, the daily refusals still none.
	projectWide, err := problems.Read(t.Context(), ch, db, mcpconnectionattemptstest.LogsOn, problems.ReadScope{
		OrganizationID: s.orgID(), ProjectID: s.projectID(), WholeOrganization: false,
	}, time.Time{}, now)
	require.NoError(t, err)
	ids := func(ps []problems.Problem) []string {
		out := make([]string, len(ps))
		for i, p := range ps {
			out[i] = p.ID
		}
		return out
	}
	require.ElementsMatch(t, ids(got), ids(projectWide))
}
