package identityproviderconnections_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	idpc "github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

const checklistJWKSURL = "https://example.test/jwks.json"

var agentKeys = []string{
	idpc.ChecklistKeyRegisterAIAgent,
	idpc.ChecklistKeyLinkAgentApp,
	idpc.ChecklistKeyRecordAIAgent,
	idpc.ChecklistKeyAddAgentPublicKey,
	idpc.ChecklistKeyActivateAgentApp,
	idpc.ChecklistKeyFirstResourceConnection,
}

func checklist(t *testing.T, signal idpc.ChecklistSignal) map[string]idpc.ChecklistItem {
	t.Helper()
	items := idpc.OktaChecklist(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, signal)
	byKey := make(map[string]idpc.ChecklistItem, len(items))
	for _, item := range items {
		byKey[item.Key] = item
	}
	require.Len(t, byKey, len(items), "keys are unique")
	return byKey
}

func itemText(item idpc.ChecklistItem) string {
	return strings.Join(append([]string{item.Title, item.Description}, item.Details...), "\n")
}

func TestOktaChecklist_ConnectStepsThenAgentSteps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode    remotesessions.TokenEndpointAuthMethod
		connect []string
	}{
		{mode: remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, connect: []string{
			idpc.ChecklistKeyCreateAPIServicesApp,
			idpc.ChecklistKeyPublicKeyAuth,
			idpc.ChecklistKeyDPoP,
			idpc.ChecklistKeyGrantScopes,
			idpc.ChecklistKeyAssignAdminRoles,
			idpc.ChecklistKeySubmitClientID,
		}},
		{mode: remotesessions.TokenEndpointAuthMethodBasic, connect: []string{
			idpc.ChecklistKeyAddOINApp,
			idpc.ChecklistKeyGrantScopes,
			idpc.ChecklistKeyAssignAdminRoles,
			idpc.ChecklistKeySubmitClientID,
		}},
	}
	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			t.Parallel()
			items := idpc.OktaChecklist(tt.mode, idpc.ChecklistSignal{})
			keys := make([]string, 0, len(items))
			for _, item := range items {
				keys = append(keys, item.Key)
				want := idpc.ChecklistGroupConnect
				if len(keys) > len(items)-len(agentKeys) {
					want = idpc.ChecklistGroupCrossAppAccess
				}
				require.Equal(t, want, item.Group, item.Key)
			}
			require.Equal(t, append(append([]string{}, tt.connect...), agentKeys...), keys)
		})
	}
}

func TestOktaChecklist_OINSecretRejectedLeavesAppUnobserved(t *testing.T) {
	t.Parallel()
	items := idpc.OktaChecklist(remotesessions.TokenEndpointAuthMethodBasic, idpc.ChecklistSignal{
		Checked:           false,
		ClientIDSubmitted: true,
		Reasons:           []string{idpc.ReasonSecretRejected},
		MissingScopes:     []string{},
	})
	completed := make(map[string]*bool, len(items))
	for _, item := range items {
		completed[item.Key] = item.Completed
	}
	require.Contains(t, completed, idpc.ChecklistKeyAddOINApp)
	require.Nil(t, completed[idpc.ChecklistKeyAddOINApp])
	require.Contains(t, completed, idpc.ChecklistKeyGrantScopes)
	require.Nil(t, completed[idpc.ChecklistKeyGrantScopes])
	require.Equal(t, new(true), completed[idpc.ChecklistKeySubmitClientID])
}

func TestOktaChecklist_Completion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		signal idpc.ChecklistSignal
		// want lists the observed steps; every other step must be nil.
		want map[string]bool
	}{
		{
			name:   "nothing observed before verification",
			signal: idpc.ChecklistSignal{},
			want:   map[string]bool{idpc.ChecklistKeySubmitClientID: false},
		},
		{
			name:   "verified ticks everything observable",
			signal: idpc.ChecklistSignal{Checked: true, ClientIDSubmitted: true, DPoPBound: true, MissingScopes: []string{}},
			want: map[string]bool{
				idpc.ChecklistKeyCreateAPIServicesApp: true,
				idpc.ChecklistKeyPublicKeyAuth:        true,
				idpc.ChecklistKeyDPoP:                 true,
				idpc.ChecklistKeyGrantScopes:          true,
				idpc.ChecklistKeyAssignAdminRoles:     true,
				idpc.ChecklistKeySubmitClientID:       true,
			},
		},
		{
			name:   "degraded reports what verification saw",
			signal: idpc.ChecklistSignal{Checked: true, ClientIDSubmitted: true, MissingScopes: []string{"okta.users.read"}},
			want: map[string]bool{
				idpc.ChecklistKeyCreateAPIServicesApp: true,
				idpc.ChecklistKeyPublicKeyAuth:        true,
				idpc.ChecklistKeyDPoP:                 false,
				idpc.ChecklistKeyGrantScopes:          false,
				idpc.ChecklistKeySubmitClientID:       true,
			},
		},
		{
			name: "scope refusal does not prove a token was issued",
			signal: idpc.ChecklistSignal{
				Checked:           true,
				ClientIDSubmitted: true,
				MissingScopes:     idpc.RequiredOktaScopes,
				Reasons:           []string{idpc.ReasonMissingScope, idpc.ReasonDPoPNotBound},
			},
			want: map[string]bool{idpc.ChecklistKeyGrantScopes: false, idpc.ChecklistKeySubmitClientID: true},
		},
		{
			name:   "a recorded agent id implies a registered agent",
			signal: idpc.ChecklistSignal{AgentRecorded: true},
			want: map[string]bool{
				idpc.ChecklistKeySubmitClientID:  false,
				idpc.ChecklistKeyRegisterAIAgent: true,
				idpc.ChecklistKeyRecordAIAgent:   true,
			},
		},
		{
			name:   "a recorded resource connection ticks the first connection step",
			signal: idpc.ChecklistSignal{Agent: idpc.AgentObservation{ConnectionRecorded: true}},
			want:   map[string]bool{idpc.ChecklistKeySubmitClientID: false, idpc.ChecklistKeyFirstResourceConnection: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for key, item := range checklist(t, tt.signal) {
				want, observed := tt.want[key]
				if !observed {
					require.Nil(t, item.Completed, key)
					continue
				}
				require.Equal(t, new(want), item.Completed, key)
			}
		})
	}
}

func TestOktaChecklist_LinkedAppStateDrivesTheAppSteps(t *testing.T) {
	t.Parallel()
	base := checklist(t, idpc.ChecklistSignal{})
	tests := []struct {
		name   string
		app    idpc.AgentAppSignal
		linked bool
		ready  *bool
		// says is a fragment of the activate step's status sentence.
		says string
	}{
		{name: "not in the sync", app: idpc.AgentAppSignal{}, linked: false, ready: nil},
		{name: "inactive", app: idpc.AgentAppSignal{Found: true, Assigned: true}, linked: true, ready: new(false), says: "is not Active"},
		{name: "unassigned", app: idpc.AgentAppSignal{Found: true, Active: true}, linked: true, ready: new(false), says: "no users or groups assigned"},
		{name: "ready", app: idpc.AgentAppSignal{Found: true, Active: true, Assigned: true}, linked: true, ready: new(true), says: "is Active"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			items := checklist(t, idpc.ChecklistSignal{AgentRecorded: true, Agent: idpc.AgentObservation{App: &tt.app}})
			link, activate := items[idpc.ChecklistKeyLinkAgentApp], items[idpc.ChecklistKeyActivateAgentApp]

			require.Equal(t, new(tt.linked), link.Completed)
			require.Equal(t, tt.ready, activate.Completed)
			require.Equal(t, tt.linked, link.Description == base[idpc.ChecklistKeyLinkAgentApp].Description, "only a missing app changes the link step")
			if tt.says == "" {
				require.Equal(t, base[idpc.ChecklistKeyActivateAgentApp].Description, activate.Description)
				return
			}
			require.Contains(t, activate.Description, tt.says)
			// A fault keeps the instruction; done replaces it.
			keeps := strings.HasSuffix(activate.Description, base[idpc.ChecklistKeyActivateAgentApp].Description)
			require.Equal(t, !*tt.ready, keeps)
		})
	}
}

func TestOktaChecklist_CopyInvariants(t *testing.T) {
	t.Parallel()
	items := checklist(t, idpc.ChecklistSignal{})

	for key, item := range items {
		require.NotContains(t, itemText(item), checklistJWKSURL, "%s: the management JWKS is offered through the connection, never in step copy", key)
	}
	require.Equal(t, idpc.RequiredOktaScopes, items[idpc.ChecklistKeyGrantScopes].Details)
	require.Equal(t, idpc.RequiredOktaAdminRoles, items[idpc.ChecklistKeyAssignAdminRoles].Details)
	verified := checklist(t, idpc.ChecklistSignal{Checked: true, ClientIDSubmitted: true, DPoPBound: true, MissingScopes: []string{}})
	require.Empty(t, verified[idpc.ChecklistKeyAssignAdminRoles].Details, "roles drop out once access is observed")
	for _, key := range agentKeys {
		require.NotContains(t, strings.ToLower(itemText(items[key])), "optional", key)
	}
}

func detailIndex(t *testing.T, item idpc.ChecklistItem, substr string) int {
	t.Helper()
	for i, d := range item.Details {
		if strings.Contains(d, substr) {
			return i
		}
	}
	require.Failf(t, "missing detail", "%s has no detail mentioning %q", item.Key, substr)
	return -1
}

func TestOktaChecklist_LinkedAppIsTheSignInClient(t *testing.T) {
	t.Parallel()
	items := checklist(t, idpc.ChecklistSignal{})

	require.Contains(t, itemText(items[idpc.ChecklistKeyLinkAgentApp]), "sign in to Speakeasy")
	key := itemText(items[idpc.ChecklistKeyAddAgentPublicKey])
	for _, want := range []string{"Set up Okta sign-in", "paste this key in the agent's Credentials and click Activate", "pasting the new public key in Okta again", "Google Cloud KMS", "customer-managed encryption keys"} {
		require.Contains(t, key, want)
	}
	require.NotContains(t, key, "JWKS URI", "Okta takes a pasted key, not a key URL")
	activate := itemText(items[idpc.ChecklistKeyActivateAgentApp])
	for _, want := range []string{"Authorization Code", "Refresh Token", "redirect URI", "Okta sign-in section"} {
		require.Contains(t, activate, want)
	}
	for _, key := range agentKeys {
		require.NotContains(t, strings.ToLower(itemText(items[key])), "upcoming release", key)
	}
}

func TestOktaChecklist_AgentPublicKeyBeforeLinkedAppEdits(t *testing.T) {
	t.Parallel()
	items := idpc.OktaChecklist(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT, idpc.ChecklistSignal{AgentRecorded: true})
	position := make(map[string]int, len(items))
	for i, item := range items {
		position[item.Key] = i
	}
	require.Less(t, position[idpc.ChecklistKeyLinkAgentApp], position[idpc.ChecklistKeyRecordAIAgent], "the agent ID exists once the agent is registered")
	require.Less(t, position[idpc.ChecklistKeyRecordAIAgent], position[idpc.ChecklistKeyAddAgentPublicKey], "Set up Okta sign-in needs the recorded agent")
	require.Less(t, position[idpc.ChecklistKeyAddAgentPublicKey], position[idpc.ChecklistKeyActivateAgentApp], "Okta rejects linked app edits until the agent has a public key")
	require.Nil(t, items[position[idpc.ChecklistKeyAddAgentPublicKey]].Completed, "Speakeasy cannot observe the agent's Okta credentials")
}

func TestOktaChecklist_RecordAgentSignInFollowUpsAreUnmonitored(t *testing.T) {
	t.Parallel()
	record := checklist(t, idpc.ChecklistSignal{AgentRecorded: true})[idpc.ChecklistKeyRecordAIAgent]
	require.NotNil(t, record.Completed)
	require.True(t, *record.Completed)
	note := record.Details[detailIndex(t, record, "does not check")]
	require.Contains(t, note, "agent ID is recorded")
}

func TestOktaChecklist_AssignedUsersMustAlreadyExist(t *testing.T) {
	t.Parallel()
	activate := checklist(t, idpc.ChecklistSignal{})[idpc.ChecklistKeyActivateAgentApp]
	require.Contains(t, activate.Description, "SCIM")
}

func TestOktaChecklist_FirstResourceConnectionNamesVendorTrustStep(t *testing.T) {
	t.Parallel()
	first := checklist(t, idpc.ChecklistSignal{})[idpc.ChecklistKeyFirstResourceConnection]
	trust := first.Details[detailIndex(t, first, "enterprise-managed authorization")]
	for _, want := range []string{"Okta issuer", "Linear", "Speakeasy cannot"} {
		require.Contains(t, trust, want)
	}
}
