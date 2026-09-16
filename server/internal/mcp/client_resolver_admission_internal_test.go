package mcp

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// TestInfra is the shared test environment, published by the external test
// package's TestMain so package-internal tests can clone databases too.
var TestInfra *testenv.Environment

// TestAdmitCIMDClient_PlatformAssistant: a document Gram publishes for one
// of its own assistants is admitted on any issuer that accepts CIMD, without
// a catalog entry or a custom URL row. The exemption is bound to an
// assistant that exists: a guessed id on the same path is an ordinary
// client_id for the policy, and a disabled issuer refuses it either way.
func TestAdmitCIMDClient_PlatformAssistant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := TestInfra.CloneTestDatabase(t, "mcp_cimd_platform_assistant")
	require.NoError(t, err)

	require.NoError(t, orgsrepo.New(conn).CreateOrganizationMetadata(ctx, orgsrepo.CreateOrganizationMetadataParams{
		ID:   "org-cimd",
		Name: "CIMD Org",
		Slug: "cimd-org",
	}))
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Project",
		Slug:           "project",
		OrganizationID: "org-cimd",
	})
	require.NoError(t, err)
	assistant, err := assistantrepo.New(conn).CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
		ProjectID:       project.ID,
		OrganizationID:  "org-cimd",
		CreatedByUserID: pgtype.Text{},
		Name:            "Assistant",
		Model:           "openai/gpt-4o-mini",
		Instructions:    "",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          assistants.StatusActive,
	})
	require.NoError(t, err)

	serverURL, err := url.Parse("https://gram.example.test")
	require.NoError(t, err)
	svc := &Service{
		logger:               testenv.NewLogger(t),
		db:                   conn,
		serverURL:            serverURL,
		cimdAdmissionMetrics: nil,
	}
	endpoint := func(mode admission.Mode) *ResolvedMcpEndpoint {
		return &ResolvedMcpEndpoint{
			AudienceURN:          "toolset:test",
			CIMDAdmissionModeRaw: conv.ToPGText(string(mode)),
			CustomDomainID:       uuid.NullUUID{},
			IsPublic:             true,
			McpServerID:          uuid.NullUUID{},
			OrganizationID:       "org-cimd",
			ProjectID:            project.ID,
			RouteBase:            "mcp",
			Slug:                 "test",
			ToolsetID:            uuid.NullUUID{},
			UpstreamResource:     "",
			UserSessionIssuerID:  uuid.New(),
		}
	}
	clientID := assistants.AssistantClientMetadataDocumentURL(serverURL, assistant.ID)

	require.NoError(t, svc.admitCIMDClient(ctx, svc.logger, endpoint(admission.ModePresets), clientID))
	require.NoError(t, svc.admitCIMDClient(ctx, svc.logger, endpoint(admission.ModeOpen), clientID))
	require.NoError(t, svc.admitCIMDClient(ctx, svc.logger, endpoint(admission.ModeReporting), clientID))

	// First-party admission is deployment-wide, not limited to the
	// assistant's own project or organization.
	other := endpoint(admission.ModePresets)
	other.ProjectID = uuid.New()
	other.OrganizationID = "org-other"
	require.NoError(t, svc.admitCIMDClient(ctx, svc.logger, other, clientID))

	// Lookup failures must not turn open-mode shadow measurement into
	// enforcement. Presets still fails closed on an unavailable lookup.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.NoError(t, svc.admitCIMDClient(canceled, svc.logger, endpoint(admission.ModeOpen), clientID))
	require.ErrorIs(t, svc.admitCIMDClient(canceled, svc.logger, endpoint(admission.ModePresets), clientID), context.Canceled)

	var denial *admission.DenialError
	require.ErrorAs(t, svc.admitCIMDClient(ctx, svc.logger, endpoint(admission.ModeDisabled), clientID), &denial)
	require.Equal(t, admission.DenialDisabled, denial.Reason)
	require.ErrorAs(t, svc.admitCIMDClient(ctx, svc.logger, endpoint(admission.Mode("invalid")), clientID), &denial)
	require.Equal(t, admission.DenialDisabled, denial.Reason)

	unknown := assistants.AssistantClientMetadataDocumentURL(serverURL, uuid.New())
	require.ErrorAs(t, svc.admitCIMDClient(ctx, svc.logger, endpoint(admission.ModePresets), unknown), &denial)
	require.Equal(t, admission.DenialNotListed, denial.Reason)

	for _, suffix := range []string{"?extra=1", "/", "#fragment"} {
		require.ErrorAs(t, svc.admitCIMDClient(ctx, svc.logger, endpoint(admission.ModePresets), clientID+suffix), &denial)
		require.Equal(t, admission.DenialNotListed, denial.Reason)
	}

	foreign := "https://other.example.test/.well-known/oauth-client/assistants/" + assistant.ID.String()
	require.ErrorAs(t, svc.admitCIMDClient(ctx, svc.logger, endpoint(admission.ModePresets), foreign), &denial)
	require.Equal(t, admission.DenialNotListed, denial.Reason)
	require.NoError(t, assistantrepo.New(conn).DeleteAssistant(ctx, assistantrepo.DeleteAssistantParams{
		AssistantID: assistant.ID,
		ProjectID:   project.ID,
	}))
	require.ErrorAs(t, svc.admitCIMDClient(ctx, svc.logger, endpoint(admission.ModePresets), clientID), &denial)
	require.Equal(t, admission.DenialNotListed, denial.Reason)
}
