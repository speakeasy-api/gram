package identityproviderconnections_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
	readinessrepo "github.com/speakeasy-api/gram/server/internal/xaareadiness/repo"
)

func TestRecordAgent_InvalidatesReadinessOnlyOnChange(t *testing.T) {
	t.Parallel()
	const agentID = "0oaagent000000000001"
	const agentAppID = "0oassoapp00000000001"
	for _, tt := range []struct {
		name       string
		agentID    string
		agentAppID string
		preserve   bool
	}{
		{name: "same agent", agentID: agentID, agentAppID: agentAppID, preserve: true},
		{name: "normalized same agent", agentID: " " + agentID + " ", agentAppID: " " + agentAppID + " ", preserve: true},
		{name: "changed agent", agentID: "0oaagent000000000002", agentAppID: agentAppID},
		{name: "changed app", agentID: agentID, agentAppID: "0oassoapp00000000002"},
		{name: "cleared agent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, si := newTestService(t)
			created := createConnection(t, ctx, si, fullOrgURL)
			id := uuid.MustParse(created.ID)
			_, err := si.svc.RecordAgent(ctx, &gen.RecordAgentPayload{ID: created.ID, AgentID: conv.PtrEmpty(agentID), AgentAppID: conv.PtrEmpty(agentAppID)})
			require.NoError(t, err)
			managed, err := si.provisioner.GetManagedClient(ctx, si.orgID, id)
			require.NoError(t, err)
			q := readinessrepo.New(si.conn.conn)
			for _, resource := range []string{"https://resource.example.com/one", "https://resource.example.com/two"} {
				_, err := q.ConfirmConnection(ctx, readinessrepo.ConfirmConnectionParams{
					OrganizationID: si.orgID, IdentityProviderConnectionID: id, RemoteSessionIssuerID: managed.IssuerID,
					Resource: resource, Audience: "https://audience.example.com",
				})
				require.NoError(t, err)
			}
			params := readinessrepo.ListReadinessRowsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id}
			before, err := q.ListReadinessRows(ctx, params)
			require.NoError(t, err)
			require.Len(t, before, 2)
			_, err = si.svc.RecordAgent(ctx, &gen.RecordAgentPayload{ID: created.ID, AgentID: conv.PtrEmpty(tt.agentID), AgentAppID: conv.PtrEmpty(tt.agentAppID)})
			require.NoError(t, err)
			after, err := q.ListReadinessRows(ctx, params)
			require.NoError(t, err)
			if tt.preserve {
				require.Equal(t, before, after, "a no-op must preserve confirmations without rewriting them")
			} else {
				require.Empty(t, after, "all confirmations for the previous agent must be invalidated")
			}
		})
	}
}
