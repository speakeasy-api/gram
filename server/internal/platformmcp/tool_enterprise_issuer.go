//nolint:exhaustruct // MCP manifests use optional zero values.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/emaissuer"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// WithEnterpriseIssuer supplies the pinned issuer origin and the same setup
// prerequisites the dashboard checks.
func (r *PostgresReader) WithEnterpriseIssuer(origin *url.URL, features *productfeatures.Client, provisioningAvailable bool) *PostgresReader {
	r.enterpriseIssuerOrigin = origin
	r.enterpriseIssuerFeatures = features
	r.enterpriseIssuerProvisioning = provisioningAvailable
	return r
}

// EnterpriseIssuerState identifies the administrator's next setup action.
type EnterpriseIssuerState string

const (
	EnterpriseIssuerSetupRequired           EnterpriseIssuerState = "setup_required"
	EnterpriseIssuerEnabled                 EnterpriseIssuerState = "enabled"
	EnterpriseIssuerDisabled                EnterpriseIssuerState = "disabled"
	EnterpriseIssuerEntitlementRequired     EnterpriseIssuerState = "entitlement_required"
	EnterpriseIssuerProvisioningUnavailable EnterpriseIssuerState = "provisioning_unavailable"
)

// EnterpriseIssuerStatus is public setup information, never signing material.
type EnterpriseIssuerStatus struct {
	OrganizationID string                `json:"organization_id"`
	State          EnterpriseIssuerState `json:"state" jsonschema:"one of setup_required, enabled, disabled, entitlement_required, provisioning_unavailable"`
	Issuer         string                `json:"issuer,omitempty" jsonschema:"issuer URI to register with a supported service; it never changes"`
	NextStep       string                `json:"next_step"`
}

func registerEnterpriseIssuerTool(reg *Registrar, reader Reader) {
	addTool(reg, &mcp.Tool{
		Name:        "get_enterprise_issuer",
		Title:       "Get Enterprise Authorization Issuer",
		Description: "Read this organization's Speakeasy issuer URI for enterprise-managed authorization. An administrator registers that URI with a supported service so Speakeasy can sign in to it for users. Reports whether the issuer is set up, enabled or disabled, and the next step. Read-only: it never signs anything and does not prove the service trusts the issuer. Enabling, disabling and rotating keys are dashboard-only. Requires an external organization administrator.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, DiscoveryScopes: nil, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, EnterpriseIssuerStatus, error) {
		status := EnterpriseIssuerStatus{State: EnterpriseIssuerProvisioningUnavailable, NextStep: "Contact support to make enterprise authorization available on this deployment."}
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, status, err
		}
		status.OrganizationID = principal.OrganizationID
		postgres, ok := reader.(*PostgresReader)
		if !ok || postgres.db == nil || postgres.enterpriseIssuerOrigin == nil || postgres.enterpriseIssuerFeatures == nil {
			return nil, status, ErrUnavailable
		}
		entitled, err := postgres.enterpriseIssuerFeatures.IsFeatureEnabled(ctx, principal.OrganizationID, productfeatures.FeatureSSO)
		if err != nil {
			return nil, status, fmt.Errorf("read enterprise authorization availability: %w", err)
		}
		switch {
		case !entitled:
			status.State = EnterpriseIssuerEntitlementRequired
			status.NextStep = "Ask support to enable single sign-on for this organization."
		case postgres.enterpriseIssuerProvisioning:
			status.State = EnterpriseIssuerSetupRequired
			status.NextStep = "Enable enterprise authorization from the dashboard's identity settings."
		}
		row, err := jwksrepo.New(postgres.db).GetOrganizationInternalSessionIssuer(ctx, principal.OrganizationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status, nil
		}
		if err != nil {
			return nil, status, fmt.Errorf("read enterprise issuer: %w", err)
		}
		issuer, err := emaissuer.URL(postgres.enterpriseIssuerOrigin, principal.OrganizationID)
		if err != nil {
			return nil, status, fmt.Errorf("build enterprise issuer URL: %w", err)
		}
		status.Issuer = issuer.String()
		if row.DisabledAt.Valid {
			status.State = EnterpriseIssuerDisabled
			status.NextStep = "Signing is disabled. Re-enable enterprise authorization from the dashboard to resume."
			return nil, status, nil
		}
		status.State = EnterpriseIssuerEnabled
		status.NextStep = "Register this URI in each supported service's enterprise authorization settings. Configuration alone does not verify access."
		return nil, status, nil
	})
}
