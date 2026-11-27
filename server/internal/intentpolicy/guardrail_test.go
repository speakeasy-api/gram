package intentpolicy_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
)

// Conditions are display-only; this keeps them in step with the expressions.
func TestContractConditionsMatchExpressions(t *testing.T) {
	t.Parallel()
	g := intentpolicy.DefaultGuardrail()

	questions := make(map[string]intentpolicy.LabelQuestion, len(g.Questions))
	for _, q := range g.Questions {
		questions[q.Key] = q
	}

	require.Len(t, g.Statements, len(g.Contracts))
	for i, c := range g.Contracts {
		require.Equal(t, c.Statement, g.Statements[i], c.ID)
		require.NotEmpty(t, c.Conditions, c.ID)
		require.Contains(t, []intentpolicy.Decision{intentpolicy.DecisionBlock, intentpolicy.DecisionClarify}, c.Outcome, c.ID)
		require.Contains(t, c.Expression, "? "+strings.ToUpper(string(c.Outcome))+" :", c.ID)

		for j, cond := range c.Conditions {
			if j == 0 {
				require.Empty(t, cond.Join, c.ID)
			} else {
				require.Contains(t, []string{"and", "or"}, cond.Join, c.ID)
			}
			q, ok := questions[cond.Label]
			require.True(t, ok, "%s reads unknown label %q", c.ID, cond.Label)
			require.True(t,
				strings.Contains(c.Expression, "label."+cond.Label) || strings.Contains(c.Expression, "action."+cond.Label),
				"%s condition reads %s but the expression does not", c.ID, cond.Label)
			require.NotEmpty(t, cond.Values, c.ID)
			for _, v := range cond.Values {
				if q.Kind == intentpolicy.KindNoul {
					require.Equal(t, "true", v, c.ID)
					continue
				}
				require.True(t, slices.ContainsFunc(q.Options, func(o intentpolicy.Option) bool { return o.Key == v }),
					"%s compares %s to unknown option %q", c.ID, cond.Label, v)
				require.Contains(t, c.Expression, "'"+v+"'", c.ID)
			}
		}
	}
}
