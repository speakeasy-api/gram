package assistantidentity_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/stretchr/testify/require"
)

func policyExecution(t *testing.T, version runtimepolicy.DelegatedPolicyVersion, grants []authz.Grant) assistantidentity.Execution {
	t.Helper()
	policy, err := runtimepolicy.NewDelegatedPolicy(version, grants)
	require.NoError(t, err)
	raw, err := runtimepolicy.EncodeDelegatedPolicy(version, policy)
	require.NoError(t, err)
	digest := sha256.Sum256(raw)
	return assistantidentity.Execution{Version: 1, Identity: assistantidentity.Identity{OrganizationID: "org-test", ProjectID: uuid.MustParse("10000000-0000-0000-0000-000000000001"), AssistantID: uuid.MustParse("10000000-0000-0000-0000-000000000002"), AgentID: uuid.New(), TriggerID: uuid.New(), IssuerID: uuid.New(), Subject: "test", AssistantGeneration: 1, TriggerGeneration: 1}, Issuer: "https://example.com", ThreadID: uuid.New(), EventID: "test", Mode: assistantidentity.ExecutionWorkload, Ceiling: assistantidentity.CeilingSnapshot{EncodingVersion: version, Policy: raw, Digest: hex.EncodeToString(digest[:])}}
}

func TestExecutionModelPolicyMatrix(t *testing.T) {
	t.Parallel()
	empty := policyExecution(t, runtimepolicy.DelegatedPolicyVersion3, nil)
	grant := assistantidentity.ExecutionGrant(empty.Identity.AssistantID, empty.Identity.ProjectID)
	for _, mode := range []assistantidentity.ExecutionMode{assistantidentity.ExecutionWorkload, assistantidentity.ExecutionWorkloadHuman} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			e := policyExecution(t, runtimepolicy.DelegatedPolicyVersion3, []authz.Grant{grant})
			e.Mode = mode
			if mode == assistantidentity.ExecutionWorkloadHuman {
				e.HumanUserID = "user-test"
			}
			require.NoError(t, assistantidentity.AdmitModelPolicy(e, []authz.Grant{grant}))
			require.ErrorIs(t, assistantidentity.AdmitModelPolicy(e, nil), assistantidentity.ErrExecutionAdmissionRequired)
			wrong := assistantidentity.ExecutionGrant(uuid.New(), e.Identity.ProjectID)
			require.Error(t, assistantidentity.AdmitModelPolicy(e, []authz.Grant{wrong}))
			wrong = assistantidentity.ExecutionGrant(e.Identity.AssistantID, uuid.New())
			require.Error(t, assistantidentity.AdmitModelPolicy(e, []authz.Grant{wrong}))
			e.Identity.ProjectID = uuid.New()
			require.Error(t, assistantidentity.AdmitModelPolicy(e, []authz.Grant{grant}))
		})
	}
	require.Error(t, assistantidentity.AdmitModelPolicy(empty, []authz.Grant{grant}), "live policy expansion cannot expand the saved ceiling")
	for _, v := range []runtimepolicy.DelegatedPolicyVersion{1, 2} {
		_, err := runtimepolicy.NewDelegatedPolicy(v, []authz.Grant{grant})
		require.Error(t, err)
		e := policyExecution(t, v, nil)
		require.Error(t, assistantidentity.AdmitModelPolicy(e, []authz.Grant{grant}))
	}
	e := policyExecution(t, 3, []authz.Grant{grant})
	e.Mode = "UNKNOWN"
	require.ErrorIs(t, assistantidentity.AdmitModelPolicy(e, []authz.Grant{grant}), assistantidentity.ErrInvalidIdentity)
	for _, scope := range []authz.Scope{authz.ScopeProjectWrite, authz.ScopeAgentWrite, authz.ScopeChatWrite, authz.ScopeMCPConnect} {
		allowed, err := authz.GrantsAuthorize([]authz.Grant{grant}, authz.Check{Scope: scope, ResourceID: e.Identity.AssistantID.String()})
		require.NoError(t, err)
		require.False(t, allowed, "execution implies no management/chat/business capability")
	}
}
