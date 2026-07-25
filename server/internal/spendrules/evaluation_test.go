package spendrules_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/spendrules"
	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
)

func testActors() []spendrules.Actor {
	return []spendrules.Actor{
		{
			UserID:      "user_ada",
			Email:       "ada@acme.com",
			DisplayName: "Ada",
			Attrs: celenv.Actor{
				Email:          "ada@acme.com",
				DepartmentName: "Engineering",
				JobTitle:       "Staff Engineer",
				EmployeeType:   "full_time",
				DivisionName:   "R&D",
				CostCenterName: "CC-1001",
				Groups:         []string{"eng-frontier", "leadership"},
				Roles:          []string{"admin"},
				SpendUSD:       0,
				LimitUSD:       0,
				UsedPct:        0,
				WarnAtPct:      0,
			},
		},
		{
			// A member without a synced directory profile: only identity and
			// role attributes are populated.
			UserID: "user_sam",
			Email:  "Sam@Acme.com",
			Attrs: celenv.Actor{
				Email:          "Sam@Acme.com",
				DepartmentName: "",
				JobTitle:       "",
				EmployeeType:   "",
				DivisionName:   "",
				CostCenterName: "",
				Groups:         nil,
				Roles:          []string{"member"},
				SpendUSD:       0,
				LimitUSD:       0,
				UsedPct:        0,
				WarnAtPct:      0,
			},
		},
		{
			UserID: "user_bea",
			Email:  "bea@acme.com",
			Attrs: celenv.Actor{
				Email:          "bea@acme.com",
				DepartmentName: "Finance",
				JobTitle:       "Manager",
				EmployeeType:   "full_time",
				DivisionName:   "Go-To-Market",
				CostCenterName: "CC-3100",
				Groups:         []string{"leadership"},
				Roles:          []string{"member"},
				SpendUSD:       0,
				LimitUSD:       0,
				UsedPct:        0,
				WarnAtPct:      0,
			},
		},
	}
}

func newEngine(t *testing.T) *celenv.Engine {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	return eng
}

func TestMatchActorsByDepartment(t *testing.T) {
	t.Parallel()

	matched, err := spendrules.MatchActors(newEngine(t), `department_name == "Engineering"`, testActors())
	require.NoError(t, err)
	require.Len(t, matched, 1, "members without a synced directory profile have no department")
	require.Equal(t, "ada@acme.com", matched[0].Email)
}

func TestMatchActorsByGroupMembership(t *testing.T) {
	t.Parallel()

	matched, err := spendrules.MatchActors(newEngine(t), `"leadership" in groups`, testActors())
	require.NoError(t, err)
	require.Len(t, matched, 2)
}

func TestMatchActorsByRole(t *testing.T) {
	t.Parallel()

	matched, err := spendrules.MatchActors(newEngine(t), `"member" in roles`, testActors())
	require.NoError(t, err)
	require.Len(t, matched, 2)
	require.Equal(t, "Sam@Acme.com", matched[0].Email)
	require.Equal(t, "bea@acme.com", matched[1].Email)

	admins, err := spendrules.MatchActors(newEngine(t), `"admin" in roles`, testActors())
	require.NoError(t, err)
	require.Len(t, admins, 1)
	require.Equal(t, "ada@acme.com", admins[0].Email)
}

func TestMatchActorsByEmailSuffix(t *testing.T) {
	t.Parallel()

	matched, err := spendrules.MatchActors(newEngine(t), `email.endsWith("@acme.com")`, testActors())
	require.NoError(t, err)
	require.Len(t, matched, 2, "matching is case-sensitive on the raw email value")
}

func TestMatchActorsRejectsInvalidExpression(t *testing.T) {
	t.Parallel()

	_, err := spendrules.MatchActors(newEngine(t), `nonexistent_attr == "x"`, testActors())
	require.Error(t, err)

	_, err = spendrules.MatchActors(newEngine(t), `department_name`, testActors())
	require.Error(t, err, "non-boolean expressions must be rejected")
}

func TestBuildActorUsagesOrdersBySpendAndNormalizesEmail(t *testing.T) {
	t.Parallel()

	actors := testActors()
	spend := map[string]float64{
		"ada@acme.com": 120,
		// Keyed lowercase: Sam's mixed-case directory email must still match.
		"sam@acme.com": 450,
	}

	usages := spendrules.BuildActorUsages(actors, spend, 500)
	require.Len(t, usages, 3)
	require.Equal(t, "Sam@Acme.com", usages[0].Actor.Email)
	require.InDelta(t, 450.0, usages[0].SpendUSD, 0.001)
	require.InDelta(t, 90.0, usages[0].UsedPct, 0.001)
	require.Equal(t, "ada@acme.com", usages[1].Actor.Email)
	require.InDelta(t, 24.0, usages[1].UsedPct, 0.001)
	require.InDelta(t, 0.0, usages[2].SpendUSD, 0.001)
}

func TestEvalRuleUsagesMarksBreachesFromCEL(t *testing.T) {
	t.Parallel()

	usages := spendrules.BuildActorUsages(testActors(), map[string]float64{
		"ada@acme.com": 120,
		"sam@acme.com": 95,
		"bea@acme.com": 50,
	}, 100)

	evaluated, err := spendrules.EvalRuleUsages(
		newEngine(t),
		`(spend_usd >= limit_usd) || (employee_type == "intern" && used_pct >= 90.0)`,
		80,
		usages,
	)
	require.NoError(t, err)
	require.Len(t, evaluated, 3)

	byEmail := map[string]spendrules.ActorUsage{}
	for _, usage := range evaluated {
		byEmail[usage.Actor.Email] = usage
	}
	require.True(t, byEmail["ada@acme.com"].Breached)
	require.False(t, byEmail["Sam@Acme.com"].Breached)
	require.False(t, byEmail["bea@acme.com"].Breached)
}

func TestRuleStatusDerivation(t *testing.T) {
	t.Parallel()

	usage := func(spend, limit float64) spendrules.ActorUsage {
		pct := 0.0
		if limit > 0 {
			pct = spend / limit * 100
		}
		return spendrules.ActorUsage{
			Actor:    spendrules.Actor{Email: "x@acme.com"},
			SpendUSD: spend,
			LimitUSD: limit,
			UsedPct:  pct,
			Breached: spend >= limit,
		}
	}

	// No actor near the threshold: healthy.
	require.Equal(t, spendrules.StatusHealthy,
		spendrules.RuleStatus(spendrules.ActionFlag, 80, []spendrules.ActorUsage{usage(10, 100)}))

	// Past the warn threshold but under the limit: approaching.
	require.Equal(t, spendrules.StatusApproaching,
		spendrules.RuleStatus(spendrules.ActionBlock, 80, []spendrules.ActorUsage{usage(85, 100)}))

	// At or past the limit: flagging or blocking by action.
	require.Equal(t, spendrules.StatusFlagging,
		spendrules.RuleStatus(spendrules.ActionFlag, 80, []spendrules.ActorUsage{usage(100, 100)}))
	require.Equal(t, spendrules.StatusBlocking,
		spendrules.RuleStatus(spendrules.ActionBlock, 80, []spendrules.ActorUsage{usage(150, 100)}))

	// The worst actor wins even when others are healthy.
	require.Equal(t, spendrules.StatusBlocking,
		spendrules.RuleStatus(spendrules.ActionBlock, 80, []spendrules.ActorUsage{
			usage(5, 100),
			usage(120, 100),
		}))

	// No matched actors: healthy.
	require.Equal(t, spendrules.StatusHealthy,
		spendrules.RuleStatus(spendrules.ActionBlock, 80, nil))
}
