package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
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
	gatewayID, projectID, otherProjectID := uuid.New(), uuid.New(), uuid.New()
	target := gatewayTarget{kind: gatewayKindStored, id: gatewayID}
	for _, tc := range []struct {
		name                string
		target              gatewayTarget
		caller              gatewayCaller
		projectID           uuid.UUID
		storedMachineAccess bool
		want                bool
	}{
		{name: "person", target: target, caller: gatewayCaller{kind: gatewayCallerPerson}, projectID: projectID, want: true},
		{name: "stored agent flag off", target: target, caller: gatewayCaller{kind: gatewayCallerAgent}, projectID: projectID, want: false},
		{name: "stored agent flag on", target: target, caller: gatewayCaller{kind: gatewayCallerAgent}, projectID: projectID, storedMachineAccess: true, want: true},
		{name: "stored agent wrong project", target: target, caller: gatewayCaller{kind: gatewayCallerAgent, projectScope: &otherProjectID}, projectID: projectID, storedMachineAccess: true, want: false},
		{name: "stored issuer workload", target: target, caller: gatewayCaller{kind: gatewayCallerWorkload, ceiling: []authz.Grant{{Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: authz.WildcardResource, authz.SelectorKeyProjectID: projectID.String()}}}}, projectID: projectID, storedMachineAccess: true, want: true},
		{name: "stored gateway workload", target: target, caller: gatewayCaller{kind: gatewayCallerWorkload, ceiling: []authz.Grant{{Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: gatewayID.String(), authz.SelectorKeyProjectID: projectID.String()}}}}, projectID: projectID, storedMachineAccess: true, want: true},
		{name: "stored workload another gateway", target: target, caller: gatewayCaller{kind: gatewayCallerWorkload, ceiling: []authz.Grant{{Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: uuid.NewString(), authz.SelectorKeyProjectID: projectID.String()}}}}, projectID: projectID, storedMachineAccess: true, want: false},
		{name: "project refuses workload", target: gatewayTarget{kind: gatewayKindProject, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerWorkload}, projectID: projectID, storedMachineAccess: true, want: false},
		{name: "organization refuses machine", target: gatewayTarget{kind: gatewayKindOrganization, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerWorkload}, storedMachineAccess: true, want: false},
		{name: "agent gateway refuses machine", target: gatewayTarget{kind: gatewayKindAgent, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerAgent}, projectID: projectID, storedMachineAccess: true, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.target.admits(tc.caller, tc.projectID, tc.storedMachineAccess)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
