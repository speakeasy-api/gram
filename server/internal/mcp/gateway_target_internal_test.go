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

func TestSharedGatewayTargetCallerMatrix(t *testing.T) {
	t.Parallel()

	gatewayID, projectID := uuid.New(), uuid.New()
	matchingWorkload := gatewayCaller{
		kind: gatewayCallerWorkload,
		ceiling: []authz.Grant{{
			Scope: authz.ScopeMCPConnect,
			Selector: authz.Selector{
				authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
				authz.SelectorKeyResourceID:   gatewayID.String(),
				authz.SelectorKeyProjectID:    projectID.String(),
			},
		}},
	}

	for _, gateway := range []struct {
		name string
		kind gatewayKind
	}{
		{name: "stored", kind: gatewayKindStored},
		{name: "project", kind: gatewayKindProject},
		{name: "organization", kind: gatewayKindOrganization},
	} {
		t.Run(gateway.name, func(t *testing.T) {
			t.Parallel()

			for _, caller := range []struct {
				name   string
				caller gatewayCaller
				want   bool
			}{
				{name: "person", caller: gatewayCaller{kind: gatewayCallerPerson}, want: true},
				{name: "agent", caller: gatewayCaller{kind: gatewayCallerAgent}, want: gateway.kind == gatewayKindStored || gateway.kind == gatewayKindProject},
				{name: "workload", caller: matchingWorkload, want: gateway.kind == gatewayKindStored || gateway.kind == gatewayKindProject},
			} {
				t.Run(caller.name, func(t *testing.T) {
					t.Parallel()

					got, err := (gatewayTarget{kind: gateway.kind, id: gatewayID}).admits(caller.caller, projectID, true)
					require.NoError(t, err)
					require.Equal(t, caller.want, got.admitted)
				})
			}
		})
	}
}

func TestGatewayTargetAdmission(t *testing.T) {
	t.Parallel()
	gatewayID, projectID, otherProjectID := uuid.New(), uuid.New(), uuid.New()
	target := gatewayTarget{kind: gatewayKindStored, id: gatewayID}
	for _, tc := range []struct {
		name          string
		target        gatewayTarget
		caller        gatewayCaller
		projectID     uuid.UUID
		machineAccess bool
		want          bool
	}{
		{name: "person", target: target, caller: gatewayCaller{kind: gatewayCallerPerson}, projectID: projectID, want: true},
		{name: "stored agent flag off", target: target, caller: gatewayCaller{kind: gatewayCallerAgent}, projectID: projectID, want: false},
		{name: "stored agent flag on", target: target, caller: gatewayCaller{kind: gatewayCallerAgent}, projectID: projectID, machineAccess: true, want: true},
		{name: "stored agent wrong project", target: target, caller: gatewayCaller{kind: gatewayCallerAgent, projectScope: &otherProjectID}, projectID: projectID, machineAccess: true, want: false},
		{name: "stored issuer workload", target: target, caller: gatewayCaller{kind: gatewayCallerWorkload, ceiling: []authz.Grant{{Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: authz.WildcardResource, authz.SelectorKeyProjectID: projectID.String()}}}}, projectID: projectID, machineAccess: true, want: true},
		{name: "stored gateway workload", target: target, caller: gatewayCaller{kind: gatewayCallerWorkload, ceiling: []authz.Grant{{Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: gatewayID.String(), authz.SelectorKeyProjectID: projectID.String()}}}}, projectID: projectID, machineAccess: true, want: true},
		{name: "stored workload another gateway", target: target, caller: gatewayCaller{kind: gatewayCallerWorkload, ceiling: []authz.Grant{{Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: uuid.NewString(), authz.SelectorKeyProjectID: projectID.String()}}}}, projectID: projectID, machineAccess: true, want: false},
		{name: "project agent flag off", target: gatewayTarget{kind: gatewayKindProject, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerAgent, projectScope: &projectID}, projectID: projectID, want: false},
		{name: "project agent flag on", target: gatewayTarget{kind: gatewayKindProject, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerAgent, projectScope: &projectID}, projectID: projectID, machineAccess: true, want: true},
		{name: "project agent wrong project", target: gatewayTarget{kind: gatewayKindProject, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerAgent, projectScope: &otherProjectID}, projectID: projectID, machineAccess: true, want: false},
		{name: "project workload", target: gatewayTarget{kind: gatewayKindProject, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerWorkload, ceiling: []authz.Grant{{Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: gatewayID.String(), authz.SelectorKeyProjectID: projectID.String()}}}}, projectID: projectID, machineAccess: true, want: true},
		{name: "project refuses workload without target ceiling", target: gatewayTarget{kind: gatewayKindProject, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerWorkload}, projectID: projectID, machineAccess: true, want: false},
		{name: "organization refuses machine", target: gatewayTarget{kind: gatewayKindOrganization, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerWorkload}, machineAccess: true, want: false},
		{name: "agent gateway refuses machine", target: gatewayTarget{kind: gatewayKindAgent, id: gatewayID}, caller: gatewayCaller{kind: gatewayCallerAgent}, projectID: projectID, machineAccess: true, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.target.admits(tc.caller, tc.projectID, tc.machineAccess)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.admitted)
		})
	}
}

func TestGatewayTargetRefusalReasons(t *testing.T) {
	t.Parallel()
	projectID, otherProjectID, gatewayID := uuid.New(), uuid.New(), uuid.New()
	target := gatewayTarget{kind: gatewayKindStored, id: gatewayID}
	for _, tc := range []struct {
		name          string
		target        gatewayTarget
		caller        gatewayCaller
		projectID     uuid.UUID
		machineAccess bool
		want          gatewayRefusalReason
	}{
		{name: "machine access disabled", target: target, caller: gatewayCaller{kind: gatewayCallerAgent}, projectID: projectID, want: gatewayRefusalMachineAccessDisabled},
		{name: "unsupported gateway", target: gatewayTarget{kind: gatewayKindOrganization}, caller: gatewayCaller{kind: gatewayCallerAgent}, projectID: projectID, want: gatewayRefusalUnsupportedGatewayType},
		{name: "missing project", target: target, caller: gatewayCaller{kind: gatewayCallerAgent}, machineAccess: true, want: gatewayRefusalMissingProject},
		{name: "project mismatch", target: target, caller: gatewayCaller{kind: gatewayCallerAgent, projectScope: &otherProjectID}, projectID: projectID, machineAccess: true, want: gatewayRefusalProjectScopeMismatch},
		{name: "credential ceiling", target: target, caller: gatewayCaller{kind: gatewayCallerWorkload}, projectID: projectID, machineAccess: true, want: gatewayRefusalCredentialCeiling},
		{name: "unsupported caller", target: target, caller: gatewayCaller{}, projectID: projectID, machineAccess: true, want: gatewayRefusalUnsupportedCaller},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.target.admits(tc.caller, tc.projectID, tc.machineAccess)
			require.NoError(t, err)
			require.False(t, got.admitted)
			require.Equal(t, tc.want, got.refusalReason)
		})
	}
}
