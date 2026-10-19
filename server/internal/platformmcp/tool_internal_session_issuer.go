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
	State          internalsessionissuers.State `json:"state" jsonschema:"one of unavailable, verified_domain_required, setup_required, enabled, disabled, signing_paused, keyless"`
	Issuer         string                       `json:"issuer,omitempty" jsonschema:"issuer URI to register with a supported service; it never changes"`
	CanEnable      bool                         `json:"can_enable" jsonschema:"whether an organization administrator can enable the issuer in this deployment"`
	NextStep       string                       `json:"next_step"`
}

var internalSessionIssuerNextSteps = map[internalsessionissuers.State]string{
	internalsessionissuers.StateUnavailable:            "The issuer is not available to this organization yet.",
	internalsessionissuers.StateVerifiedDomainRequired: "Verify at least one email domain for this organization on IDP and SSO first; Speakeasy only vouches for emails on verified domains.",
	internalsessionissuers.StateSetupRequired:          "An organization administrator can enable Enterprise Managed Authorization in the dashboard (IDP and SSO > Identity providers > Speakeasy).",
	internalsessionissuers.StateEnabled:                "Register this URI in each supported service's enterprise-managed authorization settings; registering alone changes nothing. Servers whose sign-in trusts no external identity provider then use Speakeasy wherever the service accepts it, which by default is every service; servers whose sign-in trusts one can be vouched for only by that identity provider, and only where the service accepts it. An organization administrator chooses which identity providers a service accepts on IDP and SSO > Identity providers > Services. Each user needs a company sign-in to Speakeasy (SSO, company-managed Google or Microsoft account, magic link, or password) with an email on a verified domain; personal Google or Microsoft accounts don't qualify. Configuration alone does not verify access.",
	internalsessionissuers.StateDisabled:               "Signing is disabled. An organization administrator can re-enable Enterprise Managed Authorization in the dashboard (IDP and SSO > Identity providers > Speakeasy).",
	internalsessionissuers.StateSigningPaused:          "Speakeasy has paused issuer signing. Contact your Speakeasy administrator to resume it.",
	internalsessionissuers.StateKeyless:                "No signing key is active, so nothing is signed. An organization administrator can choose Finish setting up a new key in the dashboard (IDP and SSO > Identity providers > Speakeasy); the issuer URI stays the same. This read-only tool can't do it.",
}

func internalSessionIssuerNextStep(state internalsessionissuers.State, canEnable bool) string {
	switch {
	case !canEnable && state == internalsessionissuers.StateSetupRequired:
		return "Signing is unavailable on this deployment. Ask a Speakeasy administrator to check its signing configuration before enabling Enterprise Managed Authorization."
	case !canEnable && state == internalsessionissuers.StateDisabled:
		return "Signing is disabled and can't be re-enabled right now. Check that the organization still has a verified email domain on IDP and SSO; if it does, ask a Speakeasy administrator to check this deployment's signing configuration."
	}
	return internalSessionIssuerNextSteps[state]
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
		status.CanEnable = postgres.issuerGate.CanEnable(principal.OrganizationID, current)
		status.NextStep = internalSessionIssuerNextStep(current.State, status.CanEnable)
		// Rolling back the issuer hides it, whatever the organization's own state.
		if current.Issuer != nil && current.RolledOut && current.State != internalsessionissuers.StateUnavailable {
			status.Issuer = current.Issuer.Issuer
		}
		return nil, status, nil
	})
}
