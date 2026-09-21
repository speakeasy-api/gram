package identityproviderconnections_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	idpc "github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
)

const checklistJWKSURL = "https://example.test/jwks.json"

var agentKeys = []string{
	idpc.ChecklistKeyRegisterAIAgent,
	idpc.ChecklistKeyLinkAgentApp,
	idpc.ChecklistKeyActivateAgentApp,
	idpc.ChecklistKeyRecordAIAgent,
	idpc.ChecklistKeyFirstResourceConnection,
}

func checklist(t *testing.T, signal idpc.ChecklistSignal) map[string]idpc.ChecklistItem {
	t.Helper()
	items := idpc.OktaChecklist(idpc.ListingModeCustomApp, checklistJWKSURL, signal)
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
	for mode, appKey := range map[string]string{
		idpc.ListingModeCustomApp: idpc.ChecklistKeyCreateAPIServicesApp,
		idpc.ListingModeOIN:       idpc.ChecklistKeyAddOINApp,
	} {
		items := idpc.OktaChecklist(mode, checklistJWKSURL, idpc.ChecklistSignal{})
		keys := make([]string, 0, len(items))
		for _, item := range items {
			keys = append(keys, item.Key)
			want := idpc.ChecklistGroupConnect
			if len(keys) > len(items)-len(agentKeys) {
				want = idpc.ChecklistGroupCrossAppAccess
			}
			require.Equal(t, want, item.Group, item.Key)
		}
		require.Equal(t, append([]string{
			appKey,
			idpc.ChecklistKeyPublicKeyAuth,
			idpc.ChecklistKeyDPoP,
			idpc.ChecklistKeyGrantScopes,
			idpc.ChecklistKeyAssignAdminRoles,
			idpc.ChecklistKeySubmitClientID,
		}, agentKeys...), keys, mode)
	}
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
		offersKeyURL := strings.Contains(itemText(item), checklistJWKSURL)
		require.Equal(t, key == idpc.ChecklistKeyPublicKeyAuth, offersKeyURL, "%s: the management JWKS is only offered to the service app", key)
	}
	for _, scope := range idpc.RequiredOktaScopes {
		require.Contains(t, items[idpc.ChecklistKeyGrantScopes].Description, scope)
	}
	for _, key := range agentKeys {
		require.NotContains(t, strings.ToLower(itemText(items[key])), "optional", key)
	}
}
