package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
)

func TestGatewayTargetFromEndpoint(t *testing.T) {
	t.Parallel()
	organizationID := "org_test"
	issuerID, gatewayID, projectID := uuid.New(), uuid.New(), uuid.New()
	for _, tc := range []struct {
		name      string
		routeBase mcproute.Base
		projectID uuid.UUID
		wantKind  gatewayKind
	}{
		{name: "stored", routeBase: mcproute.MCP, projectID: projectID, wantKind: gatewayKindStored},
		{name: "project", routeBase: mcproute.OrganizationGateway, projectID: projectID, wantKind: gatewayKindProject},
		{name: "organization", routeBase: mcproute.OrganizationGateway, projectID: uuid.Nil, wantKind: gatewayKindOrganization},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target, err := gatewayTargetFromEndpoint(&ResolvedMcpEndpoint{
				OrganizationID:      organizationID,
				UserSessionIssuerID: issuerID,
				MetaMcpServerID:     uuid.NullUUID{UUID: gatewayID, Valid: true},
				ProjectID:           tc.projectID,
				RouteBase:           tc.routeBase,
			})
			require.NoError(t, err)
			require.Equal(t, gatewayTarget{
				organizationID:      organizationID,
				userSessionIssuerID: issuerID,
				kind:                tc.wantKind,
				id:                  gatewayID,
			}, target)
		})
	}
}

func TestGatewayTargetAdmission(t *testing.T) {
	t.Parallel()
	gatewayID := uuid.New()
	target := gatewayTarget{kind: gatewayKindStored, id: gatewayID}
	for _, tc := range []struct {
		name   string
		target gatewayTarget
		caller gatewayCaller
		want   bool
	}{
		{name: "person", target: target, caller: gatewayCaller{kind: gatewayCallerPerson}, want: true},
		{name: "stored refuses agent in phase one", target: target, caller: gatewayCaller{kind: gatewayCallerAgent}, want: false},
		{name: "project refuses workload in phase one", target: gatewayTarget{kind: gatewayKindProject, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerWorkload}, want: false},
		{name: "organization refuses machine", target: gatewayTarget{kind: gatewayKindOrganization, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerWorkload}, want: false},
		{name: "agent refuses machine", target: gatewayTarget{kind: gatewayKindAgent, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerAgent}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.target.admits(tc.caller)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
