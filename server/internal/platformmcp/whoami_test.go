package platformmcp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

func TestWhoamiIsAMemberReadForBothAudiencesWithoutInputs(t *testing.T) {
	t.Parallel()

	live := newRegistrar(newTestMCPServer())
	registerWhoamiTool(live, NewWhoamiService(nil))
	descriptor := descriptorByName(t, live, "whoami")
	require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization)
	require.Equal(t, bothAudiences, descriptor.Meta.Audiences)
	require.Equal(t, ProjectScopeNone, descriptor.Meta.ProjectScope)
	require.True(t, descriptor.Annotations.ReadOnlyHint)
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema))
	require.Empty(t, schema.Properties, "whoami answers for the caller and takes no selector")

	stub := newRegistrar(newTestMCPServer())
	registerWhoamiTool(stub, nil)
	unavailable := descriptorByName(t, stub, "whoami")
	require.Equal(t, descriptor.Meta, unavailable.Meta)
	require.JSONEq(t, string(descriptor.InputSchema), string(unavailable.InputSchema))
}

func TestWhoamiCardShowsUserOrganizationAndRoles(t *testing.T) {
	t.Parallel()

	rendered := renderCard(t, whoamiCard(WhoamiOutput{
		User:         WhoamiUser{Email: "ada@example.test", DisplayName: "Ada Lovelace"},
		Organization: WhoamiOrganization{ID: "org-1", Name: "Analytical <Engines>", Slug: "analytical"},
		Roles:        []WhoamiRole{{Name: "Admin", Slug: "admin"}, {Name: "Billing", Slug: "billing"}, {Name: "Member", Slug: "member"}, {Name: "Operators", Slug: "operators"}},
	}))
	require.Contains(t, rendered, "Connected as")
	require.Contains(t, rendered, "<h1>Ada Lovelace</h1>")
	require.Contains(t, rendered, ">AL<")
	require.Contains(t, rendered, "Organization <code>Analytical &lt;Engines&gt;</code>")
	require.Contains(t, rendered, "ada@example.test")
	require.Contains(t, rendered, "<li>Admin</li><li>Billing</li><li>Member</li><li>&#43;1</li>")
	require.NotContains(t, rendered, "Operators")
	require.NotContains(t, rendered, `class="button"`)

	emailOnly := renderCard(t, whoamiCard(WhoamiOutput{User: WhoamiUser{Email: "ada@example.test"}, Roles: []WhoamiRole{}}))
	require.Equal(t, 1, strings.Count(emailOnly, "ada@example.test"), "the email is not repeated under itself")
}

func TestWhoamiReportsCallerOrganizationRolesClientAndOtherOrganizations(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_whoami")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	seedAccessMember(t, ctx, conn, principal.OrganizationID, principal.UserID, "caller@example.test")
	seedAccessRole(t, ctx, conn, principal.OrganizationID, "operators", "Operators")
	_, err = accessrepo.New(conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID: principal.OrganizationID, WorkosUserID: "workos-" + principal.UserID, UserID: conv.ToPGText(principal.UserID),
		WorkosMembershipID: conv.ToPGText("membership-" + principal.UserID), WorkosUpdatedAt: conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID: conv.ToPGTextEmpty(""), WorkosRoleSlug: "operators",
	})
	require.NoError(t, err)

	client, err := platformrepo.New(conn).CreatePlatformMCPOAuthClient(ctx, platformrepo.CreatePlatformMCPOAuthClientParams{
		ClientID: "client-" + uuid.NewString(), ClientSecretHash: pgtype.Text{}, ClientName: "Whoami test client",
		RedirectUris: []string{"https://client.example.test/callback"}, ClientSecretExpiresAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)
	principal.ClientID = client.ClientID

	otherID := "org_" + uuid.NewString()
	otherSlug := "other-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: otherID, Name: "Other organization", Slug: otherSlug, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	require.NoError(t, orgrepo.New(conn).AttachWorkOSUserToOrg(ctx, orgrepo.AttachWorkOSUserToOrgParams{
		OrganizationID: otherID, UserID: conv.ToPGText(principal.UserID), WorkosMembershipID: conv.PtrToPGText(conv.PtrEmpty("membership-other")),
	}))

	service := NewWhoamiService(conn)
	external, err := service.Whoami(ctx, principal)
	require.NoError(t, err)
	require.Equal(t, WhoamiUser{Email: "caller@example.test", DisplayName: principal.UserID}, external.User)
	require.Equal(t, principal.OrganizationID, external.Organization.ID)
	require.Equal(t, "Platform MCP test organization", external.Organization.Name)
	require.NotEmpty(t, external.Organization.Slug)
	require.Equal(t, []WhoamiRole{{Name: "Operators", Slug: "operators"}}, external.Roles)
	require.Equal(t, "Whoami test client", external.ClientName)
	require.Equal(t, []WhoamiOrganization{{ID: otherID, Name: "Other organization", Slug: otherSlug}}, external.OtherOrganizations,
		"the active organization is not offered as a place to switch to")
	require.False(t, external.OtherOrganizationsTruncated)

	assistant := principal
	assistant.ConnectionID, assistant.Generation = "", ""
	assistant.ClientID = AssistantClientID
	assistant.Surface = SurfaceProjectAssistant
	fromAssistant, err := service.Whoami(ctx, assistant)
	require.NoError(t, err)
	require.Equal(t, external.User, fromAssistant.User)
	require.Equal(t, external.Organization, fromAssistant.Organization)
	require.Equal(t, external.Roles, fromAssistant.Roles)
	require.Empty(t, fromAssistant.ClientName)
	require.Empty(t, fromAssistant.OtherOrganizations, "the assistant transcript belongs to the active organization")
}
