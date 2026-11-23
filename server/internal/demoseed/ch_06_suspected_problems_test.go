//go:build demoseed_safety

package demoseed

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/problems"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The seeded connection health lists the Cursor, Internal Wiki and Dana problems, and not
// the people who recovered from this morning's Linear errors, whatever the hour it runs.
func TestSeededConnectionHealthListsTheIntendedProblems(t *testing.T) {
	t.Parallel()
	for hours := 0; hours < 24; hours += 3 {
		ago := time.Duration(hours) * time.Hour
		t.Run(ago.String(), func(t *testing.T) {
			t.Parallel()
			seededProblems(t, time.Now().UTC().Add(-ago))
		})
	}
}

func seededProblems(t *testing.T, now time.Time) {
	t.Helper()
	spec := DefaultSpec()
	spec.OrgID = "org-problems-" + uuid.NewString()
	spec.UUIDPrefix = strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	s := &seeder{spec: spec, now: now.Add(-time.Minute)}

	rows, err := s.connectionHealthRows()
	require.NoError(t, err)
	ch, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	require.NoError(t, mcpconnectionattempts.AppendRows(t.Context(), ch, rows))
	require.NoError(t, mcpconnectionattempts.AppendServedInto(t.Context(), ch, mcpconnectionattempts.LastServedTable, s.healthServed(rows)))

	got, err := problems.Read(t.Context(), ch, db, mcpconnectionattemptstest.LogsOn, s.orgID(), s.projectID(), time.Time{}, now)
	require.NoError(t, err)

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
	type listed struct {
		Rule   problems.Rule
		Scope  problems.Scope
		People []string
	}
	var listedProblems []listed
	for _, p := range got {
		var subjects []string
		for _, e := range p.Episodes {
			if !slices.Contains(subjects, e.SubjectURN) {
				subjects = append(subjects, e.SubjectURN)
			}
		}
		slices.Sort(subjects)
		listedProblems = append(listedProblems, listed{Rule: p.Rule, Scope: p.Scope, People: subjects})
	}
	require.ElementsMatch(t, []listed{
		{Rule: problems.RuleSameAppVersion, Scope: problems.Scope{Kind: problems.ScopeAppVersion, Key: mcpconnectionattempts.FamilyCursor + "@2.0.1"}, People: people("lena", "felix", "lars")},
		{Rule: problems.RuleSameServer, Scope: problems.Scope{Kind: problems.ScopeServer, Key: s.healthServerID("wiki").String()}, People: people("ken", "ivy", "theo", "amara")},
		{Rule: problems.RuleOnePerson, Scope: problems.Scope{Kind: problems.ScopePerson, Key: subject("dana")}, People: people("dana")},
	}, listedProblems)
}
