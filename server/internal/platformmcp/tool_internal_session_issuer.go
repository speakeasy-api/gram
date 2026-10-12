//nolint:exhaustruct // MCP manifests use optional zero values.
package platformmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
)

// WithInternalSessionIssuer supplies the issuer state the management API reads.
func (r *PostgresReader) WithInternalSessionIssuer(gate *internalsessionissuers.Gate) *PostgresReader {
	r.issuerGate = gate
	return r
}

// InternalSessionIssuerStatus is public setup information, never signing material.
type InternalSessionIssuerStatus struct {
	OrganizationID string                       `json:"organization_id"`
	State          internalsessionissuers.State `json:"state" jsonschema:"one of unavailable, entitlement_required, setup_required, enabled, disabled, signing_paused, keyless"`
	Issuer         string                       `json:"issuer,omitempty" jsonschema:"issuer URI to register with a supported service; it never changes"`
	NextStep       string                       `json:"next_step"`
}

var internalSessionIssuerNextSteps = map[internalsessionissuers.State]string{
	internalsessionissuers.StateUnavailable:         "The issuer is not available to this organization yet.",
	internalsessionissuers.StateEntitlementRequired: "Ask support to enable single sign-on for this organization.",
	internalsessionissuers.StateSetupRequired:       "An administrator can enable the issuer with the internalSessionIssuers.setEnabled management API.",
	internalsessionissuers.StateEnabled:             "Register this URI in each supported service's enterprise-managed authorization settings. Configuration alone does not verify access.",
	internalsessionissuers.StateDisabled:            "Signing is disabled. An administrator can re-enable it with the internalSessionIssuers.setEnabled management API.",
	internalsessionissuers.StateSigningPaused:       "Speakeasy has paused issuer signing. Connections use other identity providers until it resumes.",
	internalsessionissuers.StateKeyless:             "Every key was revoked after a compromise and a replacement is not active yet. An administrator can retry with internalSessionIssuers.rotate and compromised set.",
}

func registerInternalSessionIssuerTool(reg *Registrar, reader Reader) {
	addTool(reg, &mcp.Tool{
		Name:        "get_internal_session_issuer",
		Title:       "Get Speakeasy Issuer",
		Description: "Read this organization's Speakeasy issuer URI for enterprise-managed authorization. An administrator registers that URI with a supported service so Speakeasy can sign in to it for users. Reports the issuer's state and the next step. Read-only: it never signs anything and does not prove the service trusts the issuer. Requires an external organization administrator.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, DiscoveryScopes: nil, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, InternalSessionIssuerStatus, error) {
		status := InternalSessionIssuerStatus{State: internalsessionissuers.StateUnavailable, NextStep: internalSessionIssuerNextSteps[internalsessionissuers.StateUnavailable]}
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, status, err
		}
		status.OrganizationID = principal.OrganizationID
		postgres, ok := reader.(*PostgresReader)
		if !ok || postgres.issuerGate == nil {
			return nil, status, ErrUnavailable
		}
		current, err := postgres.issuerGate.Status(ctx, principal.OrganizationID)
		if err != nil {
			return nil, status, fmt.Errorf("read issuer status: %w", err)
		}
		status.State = current.State
		status.NextStep = internalSessionIssuerNextSteps[current.State]
		// Rolling back the issuer hides it, whatever the organization's own state.
		if current.Issuer != nil && current.RolledOut && current.State != internalsessionissuers.StateUnavailable {
			status.Issuer = current.Issuer.Issuer
		}
		return nil, status, nil
	})
}
