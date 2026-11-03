package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// ResolveStoredGatewayMemberSlugsForTest resolves a stored gateway's served
// and hidden member slugs for userID the way a serving request does.
func ResolveStoredGatewayMemberSlugsForTest(t *testing.T, ctx context.Context, service *Service, organizationID string, projectID, gatewayID uuid.UUID, userID string) ([]string, []string) {
	t.Helper()
	gate := &metaGateContext{projectID: projectID, metaServerID: gatewayID, organizationID: organizationID, authenticated: true, userID: userID}
	_, members, err := service.resolveGateMemberSnapshot(ctx, service.logger, gate, gatewayID, projectID)
	require.NoError(t, err)
	served := make([]string, 0, len(members))
	for _, member := range members {
		served = append(served, member.slug)
	}
	hidden := make([]string, 0, len(gate.hiddenMembers))
	for _, member := range gate.hiddenMembers {
		hidden = append(hidden, member.slug)
	}
	return served, hidden
}
