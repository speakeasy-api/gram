package legacypolicyscope

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

func TestWithDetectionScopesPreservesUnknownKeys(t *testing.T) {
	t.Parallel()

	base := []byte(`{"presidio":{"score_threshold":0.4},"future_scanner":{"mode":"strict"}}`)
	out, err := withDetectionScopes(base, []ra.DetectionScopeConfig{
		{Category: "secrets", ScopeInclude: `kind == "user_message"`, ScopeExempt: ""},
	})
	require.NoError(t, err)

	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &got))
	require.JSONEq(t, `{"mode":"strict"}`, string(got["future_scanner"]),
		"a member written by a newer server survives the fold")
	require.JSONEq(t, `{"score_threshold":0.4}`, string(got["presidio"]))
	require.Equal(t, []ra.DetectionScopeConfig{
		{Category: "secrets", ScopeInclude: `kind == "user_message"`, ScopeExempt: ""},
	}, ra.DetectionScopesFromConfig(out))
}

func TestWithDetectionScopesClearsWithoutDroppingSiblings(t *testing.T) {
	t.Parallel()

	base := []byte(`{"detection_scopes":[{"category":"secrets"}],"future_scanner":{"mode":"strict"}}`)
	out, err := withDetectionScopes(base, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"future_scanner":{"mode":"strict"}}`, string(out))
}

func TestWithDetectionScopesAcceptsEmptyBase(t *testing.T) {
	t.Parallel()

	for _, base := range [][]byte{nil, []byte(""), []byte("null"), []byte("{}")} {
		out, err := withDetectionScopes(base, nil)
		require.NoError(t, err)
		require.JSONEq(t, `{}`, string(out))
	}
}

func TestWithDetectionScopesRefusesUnreadableBase(t *testing.T) {
	t.Parallel()

	_, err := withDetectionScopes([]byte(`["not","an","object"]`), nil)
	require.Error(t, err, "rewriting a config we cannot read would drop its members")
}
