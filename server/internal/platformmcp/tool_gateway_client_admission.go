package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// gatewayClientAdmissionURLLimit is how many custom document URLs one read
// returns, the same first page the gateway's Debug tab shows.
const gatewayClientAdmissionURLLimit = 10

const gatewayClientAdmissionToolName = "get_gateway_client_admission"

// GatewayClientAdmissionInput names one gateway in one exact project.
type GatewayClientAdmissionInput struct {
	// ProjectID is the exact project the gateway belongs to; it is required.
	ProjectID string `json:"project_id" jsonschema:"exact project ID"`

	// GatewayID is the exact gateway whose setting is read.
	GatewayID string `json:"gateway_id" jsonschema:"exact gateway ID"`
}

// GatewayClientAdmissionOutput is one gateway's current admission setting.
type GatewayClientAdmissionOutput struct {
	// ProjectID is the project the setting was read in.
	ProjectID string `json:"project_id"`

	// GatewayID is the gateway the setting was read for.
	GatewayID string `json:"gateway_id"`

	// SignIn is false for a gateway with no sign-in server, which reports no
	// mode or URLs.
	SignIn bool `json:"sign_in" jsonschema:"whether the gateway has a sign-in server; false means no mode or URLs apply"`

	// Mode is the effective mode the gateway's sign-in server enforces now.
	Mode string `json:"mode,omitempty" jsonschema:"presets, open, disabled, or reporting (admits exactly what open admits); the setting in force now"`

	// Meaning says in plain words which apps the mode admits.
	Meaning string `json:"meaning,omitempty"`

	// CustomClientURLs are the first page of custom document URLs allowed on
	// top of the reviewed list; they apply only in presets.
	CustomClientURLs []string `json:"custom_client_urls" jsonschema:"custom client ID metadata URLs allowed on top of the reviewed list, at most 10 in a stable order; they apply only in presets"`

	// MoreCustomURLs reports that more custom URLs exist than are listed.
	MoreCustomURLs bool `json:"more_custom_client_urls" jsonschema:"more custom URLs are configured than are listed"`
}

// GetGatewayClientAdmission reads one gateway's current client ID metadata
// admission setting: its sign-in server's effective mode and the first custom
// document URLs. A gateway the caller may not read is not found, alike with a
// missing one.
func (r *PostgresReader) GetGatewayClientAdmission(ctx context.Context, principal Principal, input GatewayClientAdmissionInput) (GatewayClientAdmissionOutput, error) {
	var none GatewayClientAdmissionOutput
	if r == nil || r.db == nil || r.authz == nil {
		return none, ErrUnavailable
	}
	// Checked here, not only in the schema: an empty selector would resolve to
	// the organization's Default project.
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return none, &ToolRefusalError{Code: "invalid_request", Payload: "project_id is required."}
	}
	project, err := r.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: projectID, ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return none, err
	}
	gatewayID, err := uuid.Parse(input.GatewayID)
	if err != nil {
		return none, &ToolRefusalError{Code: "invalid_request", Payload: "gateway_id must be a UUID."}
	}
	notFound := connectionHealthNotFound("No gateway with that ID in this project is visible to you. Check the gateway ID and project.")
	gateway, err := metamcprepo.New(r.db).GetMetaMCPServerByIDAndProjectID(ctx, metamcprepo.GetMetaMCPServerByIDAndProjectIDParams{ID: gatewayID, ProjectID: project.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return none, notFound
	}
	if err != nil {
		return none, fmt.Errorf("load gateway: %w", err)
	}
	if err := r.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, gatewayID.String(), project.ID.String())); err != nil {
		if isAuthorizationDenied(err) {
			return none, notFound
		}
		return none, fmt.Errorf("authorize gateway read: %w", err)
	}

	out := GatewayClientAdmissionOutput{ProjectID: project.ID.String(), GatewayID: gatewayID.String(), SignIn: false, Mode: "", Meaning: "", CustomClientURLs: []string{}, MoreCustomURLs: false}
	if !gateway.UserSessionIssuerID.Valid {
		return out, nil
	}
	q := usersessionsrepo.New(r.db)
	issuer, err := q.GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{ID: gateway.UserSessionIssuerID.UUID, ProjectID: project.ID, OrganizationID: principal.OrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		// The gateway names a sign-in server that is gone or out of scope,
		// so its setting is unknown, never reported as none.
		return none, connectionHealthNotFound("The gateway's sign-in server is not visible to you, so its admission setting is unknown.")
	}
	if err != nil {
		return none, fmt.Errorf("load gateway sign-in server: %w", err)
	}
	// One row past the limit says whether more are configured.
	rows, err := q.ListUserSessionIssuerCimdClientsByIssuerID(ctx, usersessionsrepo.ListUserSessionIssuerCimdClientsByIssuerIDParams{
		ProjectID:           project.ID,
		OrganizationID:      principal.OrganizationID,
		UserSessionIssuerID: issuer.ID,
		Cursor:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:          gatewayClientAdmissionURLLimit + 1,
	})
	if err != nil {
		return none, fmt.Errorf("list gateway custom client urls: %w", err)
	}
	rows, out.MoreCustomURLs = boundedRows(rows, gatewayClientAdmissionURLLimit)
	for _, row := range rows {
		out.CustomClientURLs = append(out.CustomClientURLs, row.ClientIDMetadataUri)
	}
	mode, recognized := admission.ResolveMode(issuer.ClientIDMetadataAdmissionMode.String, issuer.ClientIDMetadataAdmissionMode.Valid)
	if !recognized {
		// Enforcement fails closed on an unknown stored value, so it reads as
		// the disabled it enforces.
		r.logger.ErrorContext(ctx, "unrecognized cimd admission mode stored on gateway sign-in server, reading as disabled",
			attr.SlogUserSessionIssuerID(issuer.ID.String()),
			attr.SlogCIMDAdmissionMode(mode),
		)
	}
	out.SignIn, out.Mode, out.Meaning = true, string(mode), modeGuidance(string(mode))
	return out, nil
}

const gatewayClientAdmissionDescription = "Show which apps one exact gateway admits when they identify themselves with a client ID metadata document: the setting its sign-in server enforces now, and the custom document URLs it allows on top of Speakeasy's reviewed list. " +
	"presets admits only the reviewed list plus those URLs; open admits any valid document; disabled accepts none and stops advertising them, so apps register dynamically instead; reporting admits exactly what open admits. " +
	"The setting applies to new client metadata admission only: it does not revoke existing connections or govern dynamic client registration, and existing connections may have been admitted under an earlier setting. " +
	"custom_client_urls lists a first page of at most 10 in a stable order, not by date; more_custom_client_urls says more are configured. sign_in false means the gateway has no sign-in server. " +
	"Read-only; it reports no counts of refused sign-ins (use get_gateway_connection_health for observed attempts). Requires project:read on the project and mcp:read on the gateway."

func registerGatewayClientAdmissionTool(reg *Registrar, reader *PostgresReader) {
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name: gatewayClientAdmissionToolName, Title: "Which Apps Can Sign In to a Gateway",
		Description: gatewayClientAdmissionDescription,
		Annotations: readOnlyAnnotations(),
	}, gatewayClientAdmissionMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input GatewayClientAdmissionInput) (*mcp.CallToolResult, GatewayClientAdmissionOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GatewayClientAdmissionOutput{}, err
		}
		output, err := reader.GetGatewayClientAdmission(ctx, principal, input)
		return nil, output, err
	})
}

func registerUnavailableGatewayClientAdmissionTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name: gatewayClientAdmissionToolName, Title: "Which Apps Can Sign In to a Gateway",
		Description: "Show which apps one gateway admits by client ID metadata document. Not available yet.",
		Annotations: readOnlyAnnotations(),
	}, gatewayClientAdmissionMeta, func(_ context.Context, _ *mcp.CallToolRequest, _ GatewayClientAdmissionInput) (*mcp.CallToolResult, GatewayClientAdmissionOutput, error) {
		return nil, GatewayClientAdmissionOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: "Gateway client admission is not available yet."}
	})
}

var gatewayClientAdmissionMeta = ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}
