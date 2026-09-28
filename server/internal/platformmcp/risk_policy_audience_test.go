package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestRiskPolicyAudienceReplacementValidation(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`null`, `{}`, `{"type":"targeted","principal_urns":[],"confirm":true}`,
		`{"type":"targeted","principal_urns":["user:one"],"confirm":false}`,
		`{"type":"targeted","principal_urns":["user:all"],"confirm":true}`,
		`{"type":"targeted","principal_urns":["agent:one"],"confirm":true}`,
		`{"type":"targeted","principal_urns":[""],"confirm":true}`,
		`{"type":"everyone","principal_urns":["user:one"],"confirm":true}`,
		`{"type":"everyone","principal_urns":null,"confirm":true}`,
		`{"type":"everyone","principal_urns":[],"confirm":true,"exclude_self":true}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			_, _, err := parseRiskPolicyAudienceReplacement(json.RawMessage(raw))
			require.Error(t, err)
		})
	}
	value, principals, err := parseRiskPolicyAudienceReplacement(json.RawMessage(`{"type":"targeted","principal_urns":["user:two","user:one","user:one"],"confirm":true}`))
	require.NoError(t, err)
	require.Equal(t, []string{"user:one", "user:two"}, value.PrincipalURNs)
	require.Len(t, principals, 2)
	_, principals, err = parseRiskPolicyAudienceReplacement(json.RawMessage(`{"type":"everyone","principal_urns":[],"confirm":true}`))
	require.NoError(t, err)
	require.Equal(t, authz.AllUsersPrincipal(), principals[0])
}
