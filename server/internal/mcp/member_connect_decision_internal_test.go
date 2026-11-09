package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// Every caller of memberConnectDecision makes the same decision, but only a
// call that names one server records an authorization check. Gateway
// admission and the consent page filter every server the caller could reach,
// so a caller is not shown a denial for each server it simply cannot see.
func TestMemberConnectDecision_RecordsOnlyNamedServers(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	granted, denied := uuid.New(), uuid.New()
	ctx := sessionContext(t, shadowOrg, authz.NewGrant(authz.ScopeMCPConnect, granted.String()))

	for _, tc := range []struct {
		name string
		// decide runs one caller over the granted and the denied server, and
		// returns the servers it allowed.
		decide   func(t *testing.T, s *Service) []uuid.UUID
		recorded int
	}{
		{
			name: "gateway admission",
			decide: func(t *testing.T, s *Service) []uuid.UUID {
				t.Helper()
				candidate := func(id uuid.UUID, visibility string) metaMemberCandidate {
					return metaMemberCandidate{serverID: id, projectID: project, slug: "s-" + id.String()[:8], visibility: visibility, remoteServerID: uuid.NullUUID{UUID: id, Valid: true}}
				}
				public := uuid.New()
				_, members, turnedAway, err := s.admitMetaMembers(ctx, testenv.NewLogger(t), mandatorySurfaceGatewayMember, shadowOrg, []metaMemberCandidate{
					candidate(granted, mcpservers.VisibilityPrivate), candidate(denied, mcpservers.VisibilityPrivate), candidate(public, mcpservers.VisibilityPublic),
				})
				require.NoError(t, err)
				require.Len(t, turnedAway, 1)
				require.Equal(t, denied, turnedAway[0].serverID, "a refused private member is kept aside for a call that names it")
				allowed := make([]uuid.UUID, 0, len(members))
				for _, member := range members {
					allowed = append(allowed, member.serverID)
				}
				return allowed
			},
			recorded: 0,
		},
		{
			name: "consent candidates",
			decide: func(t *testing.T, s *Service) []uuid.UUID {
				t.Helper()
				row := func(id uuid.UUID, visibility string) metamcprepo.ListMetaMCPMembersForRemoteSessionClientRow {
					return metamcprepo.ListMetaMCPMembersForRemoteSessionClientRow{McpServerID: id, ProjectID: project, McpServerVisibility: visibility}
				}
				rows, err := s.authorizedMetaMembers(ctx, &ResolvedMcpEndpoint{OrganizationID: shadowOrg}, []metamcprepo.ListMetaMCPMembersForRemoteSessionClientRow{
					row(granted, mcpservers.VisibilityPrivate), row(denied, mcpservers.VisibilityPrivate),
				})
				require.NoError(t, err)
				allowed := make([]uuid.UUID, 0, len(rows))
				for _, row := range rows {
					allowed = append(allowed, row.McpServerID)
				}
				return allowed
			},
			recorded: 0,
		},
		{
			name: "named server",
			decide: func(t *testing.T, s *Service) []uuid.UUID {
				t.Helper()
				shadow := s.newMandatoryShadow(ctx, shadowOrg)
				allowed := []uuid.UUID{}
				for _, id := range []uuid.UUID{granted, denied} {
					refusal, err := s.memberConnectDecision(ctx, shadow, mandatorySurfaceGatewayMember, mcpservers.VisibilityPrivate, authz.MCPCheck(authz.ScopeMCPConnect, id.String(), project.String()))
					require.NoError(t, err)
					if refusal == nil {
						allowed = append(allowed, id)
					}
				}
				return allowed
			},
			recorded: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newShadowHarness(t, false)
			recorded := 0
			// The challenge-logging flag is read only for a check that would
			// be recorded, so counting reads counts recorded checks.
			h.service.authz = authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) {
				recorded++
				return false, nil
			}, workos.NewStubClient())

			allowed := tc.decide(t, h.service)
			require.Contains(t, allowed, granted)
			require.NotContains(t, allowed, denied)
			require.Equal(t, tc.recorded, recorded)
		})
	}
}
