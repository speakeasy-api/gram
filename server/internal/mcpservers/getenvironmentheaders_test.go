package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/environments"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Synthetic entry values; the preview must never return them.
const (
	previewSecretValue = "synthetic-preview-secret"
	previewPlainValue  = "synthetic-preview-plain"
)

func addEntries(t *testing.T, f linkFixture, environmentID string, names []string, values []string, secrets []bool) {
	t.Helper()
	_, err := environments.NewEnvironmentEntries(testenv.NewLogger(t), f.ti.conn, f.ti.enc, nil).CreateEnvironmentEntries(f.ctx, environmentsrepo.CreateEnvironmentEntriesParams{
		EnvironmentID: uuid.MustParse(environmentID),
		Names:         names,
		Values:        values,
		IsSecrets:     secrets,
	})
	require.NoError(t, err)
}

func previewPayload(serverID, selection string, environmentID *string) *gen.GetEnvironmentHeadersPayload {
	return &gen.GetEnvironmentHeadersPayload{
		ID:               serverID,
		Selection:        selection,
		EnvironmentID:    environmentID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

func previewStatuses(result *gen.McpServerEnvironmentHeaders) map[string]string {
	out := make(map[string]string, len(result.Entries))
	for _, e := range result.Entries {
		out[e.EntryName] = e.Status
	}
	return out
}

func requireNoValues(t *testing.T, result *gen.McpServerEnvironmentHeaders) {
	t.Helper()
	for _, e := range result.Entries {
		for _, field := range []string{e.EntryName, e.Status, deref(e.HeaderName)} {
			require.NotContains(t, field, previewSecretValue)
			require.NotContains(t, field, previewPlainValue)
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// remoteServerWithSourceHeader creates a remote-backed server whose source
// configures X-Instance-Url.
func remoteServerWithSourceHeader(t *testing.T, f linkFixture, environmentID *string) string {
	t.Helper()
	server := f.remoteServer(t, environmentID)
	_, err := remotemcprepo.New(f.ti.conn).CreateServerHeader(f.ctx, remotemcprepo.CreateServerHeaderParams{
		Name: "X-Instance-Url", Description: pgtype.Text{String: "", Valid: false}, IsRequired: true, IsSecret: false,
		Value: pgtype.Text{String: "source", Valid: true}, ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
		RemoteMcpServerID: uuid.MustParse(*server.RemoteMcpServerID), ProjectID: f.projectID,
	})
	require.NoError(t, err)
	return server.ID
}

func TestGetEnvironmentHeaders_LinkedEnvironmentStatuses(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	addEntries(t, f, f.envID,
		[]string{"MCP_HEADER_X-Instance-Url", "MCP_HEADER_X-Tenant", "MCP_HEADER_Gram-Key", "MCP_HEADER_X-Empty", "mcp_header_X-Typo", "HEADER_X-Short", "API_KEY"},
		[]string{previewPlainValue, previewSecretValue, previewSecretValue, " ", previewSecretValue, previewSecretValue, previewSecretValue},
		[]bool{false, true, true, false, true, true, true},
	)
	serverID := remoteServerWithSourceHeader(t, f, &f.envID)

	result, err := f.ti.service.GetEnvironmentHeaders(f.ctx, previewPayload(serverID, "linked", nil))
	require.NoError(t, err)
	require.Equal(t, "ok", result.EnvironmentStatus)
	require.NotNil(t, result.Environment)
	require.Equal(t, f.envID, result.Environment.ID)
	require.True(t, result.EnvironmentConfigurationInvalid)
	require.Equal(t, map[string]string{
		"MCP_HEADER_X-Instance-Url": "overrides_source",
		"MCP_HEADER_X-Tenant":       "mapped",
		"MCP_HEADER_Gram-Key":       "reserved",
		"MCP_HEADER_X-Empty":        "empty_value",
		"mcp_header_X-Typo":         "not_mapped",
		"HEADER_X-Short":            "not_mapped",
	}, previewStatuses(result))
	requireNoValues(t, result)
	require.Len(t, result.Environments, 2)
}

func TestGetEnvironmentHeaders_ValidEnvironmentIsNotInvalid(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	addEntries(t, f, f.envID, []string{"MCP_HEADER_X-Tenant"}, []string{previewSecretValue}, []bool{true})
	server := f.remoteServer(t, &f.envID)

	result, err := f.ti.service.GetEnvironmentHeaders(f.ctx, previewPayload(server.ID, "linked", nil))
	require.NoError(t, err)
	require.False(t, result.EnvironmentConfigurationInvalid)
	require.Equal(t, map[string]string{"MCP_HEADER_X-Tenant": "mapped"}, previewStatuses(result))
	require.Equal(t, "X-Tenant", deref(result.Entries[0].HeaderName))
}

// A candidate previews without changing the link, and none previews no
// environment even while the current link is broken, so an operator can
// choose the repair.
func TestGetEnvironmentHeaders_SelectionModes(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	addEntries(t, f, f.envID2, []string{"MCP_HEADER_X-Tenant"}, []string{previewSecretValue}, []bool{true})
	server := f.remoteServer(t, &f.envID)

	candidate, err := f.ti.service.GetEnvironmentHeaders(f.ctx, previewPayload(server.ID, "environment", &f.envID2))
	require.NoError(t, err)
	require.Equal(t, f.envID2, candidate.Environment.ID)
	require.Equal(t, map[string]string{"MCP_HEADER_X-Tenant": "mapped"}, previewStatuses(candidate))
	require.Equal(t, uuid.NullUUID{UUID: uuid.MustParse(f.envID), Valid: true}, storedEnvironmentID(t, f.ctx, f.ti.conn, server.ID))

	envSlug := ""
	for _, env := range candidate.Environments {
		if env.ID == f.envID {
			envSlug = env.Slug
		}
	}
	_, err = environmentsrepo.New(f.ti.conn).DeleteEnvironment(f.ctx, environmentsrepo.DeleteEnvironmentParams{Slug: envSlug, ProjectID: f.projectID})
	require.NoError(t, err)

	linked, err := f.ti.service.GetEnvironmentHeaders(f.ctx, previewPayload(server.ID, "linked", nil))
	require.NoError(t, err)
	require.Equal(t, "unavailable", linked.EnvironmentStatus)
	require.Nil(t, linked.Environment)
	require.True(t, linked.EnvironmentConfigurationInvalid)
	require.Len(t, linked.Environments, 1, "a deleted environment is not offered")

	none, err := f.ti.service.GetEnvironmentHeaders(f.ctx, previewPayload(server.ID, "none", nil))
	require.NoError(t, err)
	require.Equal(t, "none", none.EnvironmentStatus)
	require.Nil(t, none.Environment)
	require.False(t, none.EnvironmentConfigurationInvalid)
	require.Empty(t, none.Entries)
}

func TestGetEnvironmentHeaders_ConflictingSelectionIsRejected(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	server := f.remoteServer(t, nil)

	for name, payload := range map[string]*gen.GetEnvironmentHeadersPayload{
		"environment without id": previewPayload(server.ID, "environment", nil),
		"linked with id":         previewPayload(server.ID, "linked", &f.envID),
		"none with id":           previewPayload(server.ID, "none", &f.envID),
		"invalid id":             previewPayload(server.ID, "environment", new("not-a-uuid")),
		"unknown selection":      previewPayload(server.ID, "current", nil),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := f.ti.service.GetEnvironmentHeaders(f.ctx, payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}
}

// A candidate from another project, or one that does not exist, is reported
// unavailable without naming it.
func TestGetEnvironmentHeaders_ForeignCandidateIsUnavailable(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	otherProject := seedOtherProject(t, f.ctx, f.ti.conn, f.orgID)
	foreign := seedEnvironment(t, f.ctx, f.ti.conn, f.orgID, otherProject).String()
	addEntries(t, f, foreign, []string{"MCP_HEADER_X-Foreign"}, []string{previewSecretValue}, []bool{true})
	server := f.remoteServer(t, nil)

	for _, candidate := range []string{foreign, uuid.NewString()} {
		result, err := f.ti.service.GetEnvironmentHeaders(f.ctx, previewPayload(server.ID, "environment", &candidate))
		require.NoError(t, err)
		require.Equal(t, "unavailable", result.EnvironmentStatus)
		require.Nil(t, result.Environment)
		require.Empty(t, result.Entries)
		for _, env := range result.Environments {
			require.NotEqual(t, foreign, env.ID)
		}
	}
}

func TestGetEnvironmentHeaders_UndecryptableEntryIsReported(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	_, err := environmentsrepo.New(f.ti.conn).CreateEnvironmentEntries(f.ctx, environmentsrepo.CreateEnvironmentEntriesParams{
		EnvironmentID: uuid.MustParse(f.envID),
		Names:         []string{"MCP_HEADER_X-Broken"},
		Values:        []string{"not-ciphertext"},
		IsSecrets:     []bool{true},
	})
	require.NoError(t, err)
	addEntries(t, f, f.envID, []string{"MCP_HEADER_X-Ok"}, []string{previewPlainValue}, []bool{false})
	server := f.remoteServer(t, &f.envID)

	result, err := f.ti.service.GetEnvironmentHeaders(f.ctx, previewPayload(server.ID, "linked", nil))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"MCP_HEADER_X-Broken": "undecryptable", "MCP_HEADER_X-Ok": "mapped"}, previewStatuses(result))
	require.True(t, result.EnvironmentConfigurationInvalid)
	for _, e := range result.Entries {
		require.NotContains(t, e.Status, "ciphertext")
	}
}

func TestGetEnvironmentHeaders_TunneledServer(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	addEntries(t, f, f.envID, []string{"MCP_HEADER_X-Instance-Url"}, []string{previewPlainValue}, []bool{false})
	tunnelID := seedTunneledMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()
	created, err := f.ti.service.CreateMcpServer(f.ctx, &gen.CreateMcpServerPayload{
		Name:                "tunneled " + uuid.NewString()[:8],
		EnvironmentID:       &f.envID,
		TunneledMcpServerID: &tunnelID,
		Visibility:          "private",
	})
	require.NoError(t, err)

	result, err := f.ti.service.GetEnvironmentHeaders(f.ctx, previewPayload(created.ID, "linked", nil))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"MCP_HEADER_X-Instance-Url": "mapped"}, previewStatuses(result))
}

func TestGetEnvironmentHeaders_RejectsNonProxiedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	serverID := createToolsetBackedMcpServer(t, ctx, ti)

	_, err := ti.service.GetEnvironmentHeaders(ctx, previewPayload(serverID, "none", nil))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// The preview needs the same project-wide environment authority linking
// does: MCP access alone, or a grant on a single environment or another
// project, is not enough.
func TestGetEnvironmentHeaders_RequiresEnvironmentAuthority(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	addEntries(t, f, f.envID, []string{"MCP_HEADER_X-Tenant"}, []string{previewSecretValue}, []bool{true})
	server := f.remoteServer(t, &f.envID)
	otherProject := seedOtherProject(t, f.ctx, f.ti.conn, f.orgID)

	denied := map[string]context.Context{
		"mcp write only": f.mcpWriteOnly(t),
		"single environment": withExactAuthzGrants(t, f.ctx, f.ti.conn, projectMCPWriteGrant(f.projectID), authz.NewGrantWithSelector(authz.ScopeEnvironmentRead, authz.Selector{
			"resource_kind": "environment", "resource_id": f.envID, "project_id": f.projectID.String(),
		})),
		"other project environment": withExactAuthzGrants(t, f.ctx, f.ti.conn, projectMCPWriteGrant(f.projectID), projectEnvironmentGrant(authz.ScopeEnvironmentRead, otherProject)),
		"environment without mcp":   withExactAuthzGrants(t, f.ctx, f.ti.conn, projectEnvironmentGrant(authz.ScopeEnvironmentRead, f.projectID)),
	}
	for name, ctx := range denied {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, payload := range []*gen.GetEnvironmentHeadersPayload{
				previewPayload(server.ID, "linked", nil),
				previewPayload(server.ID, "environment", &f.envID2),
				previewPayload(server.ID, "none", nil),
			} {
				_, err := f.ti.service.GetEnvironmentHeaders(ctx, payload)
				requireOopsCode(t, err, oops.CodeForbidden)
			}
		})
	}

	allowed, err := f.ti.service.GetEnvironmentHeaders(f.withEnvironmentAuthority(t, authz.ScopeEnvironmentRead), previewPayload(server.ID, "linked", nil))
	require.NoError(t, err)
	require.Equal(t, "ok", allowed.EnvironmentStatus)
}

func TestGetEnvironmentHeaders_OtherProjectServerIsNotFound(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	_, err := f.ti.service.GetEnvironmentHeaders(f.ctx, previewPayload(uuid.NewString(), "none", nil))
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Distinct environments do not make two servers on one tunnel distinct
// backends: a gateway still refuses to hold both, so moving a member onto a
// sibling's tunnel is refused even when each carries its own environment.
func TestUpdateMcpServer_TunnelSharedWithMetaMcpSiblingRefusedDespiteDistinctEnvironments(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	sharedTunnel := seedTunneledMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()
	ownTunnel := seedTunneledMcpServer(t, f.ctx, f.ti.conn, f.projectID).String()
	create := func(tunnelID, environmentID string) *types.McpServer {
		created, err := f.ti.service.CreateMcpServer(f.ctx, &gen.CreateMcpServerPayload{
			Name:                "tunnel wrapper " + uuid.NewString()[:8],
			EnvironmentID:       &environmentID,
			TunneledMcpServerID: &tunnelID,
			Visibility:          "disabled",
		})
		require.NoError(t, err)
		return created
	}
	sibling := create(sharedTunnel, f.envID)
	subject := create(ownTunnel, f.envID2)

	meta, err := metamcprepo.New(f.ti.conn).CreateMetaMCPServer(f.ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      f.orgID,
		ProjectID:           f.projectID,
		Name:                "environment gateway",
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	for _, member := range []*types.McpServer{sibling, subject} {
		_, err = metamcprepo.New(f.ti.conn).CreateMetaMCPMember(f.ctx, metamcprepo.CreateMetaMCPMemberParams{
			ProjectID: f.projectID, MetaMcpServerID: meta.ID, McpServerID: uuid.MustParse(member.ID), SortOrder: 0,
		})
		require.NoError(t, err)
	}

	payload := updatePayload(subject, &f.envID2)
	payload.TunneledMcpServerID = &sharedTunnel
	_, err = f.ti.service.UpdateMcpServer(f.ctx, payload)
	requireOopsCode(t, err, oops.CodeConflict)
}

// An exclusion on one environment refuses previewing that environment, linked
// or as a candidate, while previews of other environments still work.
func TestGetEnvironmentHeaders_EnvironmentExclusionRefusesItsPreview(t *testing.T) {
	t.Parallel()

	f := newLinkFixture(t)
	server := f.remoteServer(t, &f.envID)
	ctx := f.excludedFrom(t, f.envID)

	_, err := f.ti.service.GetEnvironmentHeaders(ctx, previewPayload(server.ID, "linked", nil))
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = f.ti.service.GetEnvironmentHeaders(ctx, previewPayload(server.ID, "environment", &f.envID))
	requireOopsCode(t, err, oops.CodeForbidden)

	other, err := f.ti.service.GetEnvironmentHeaders(ctx, previewPayload(server.ID, "environment", &f.envID2))
	require.NoError(t, err)
	require.Equal(t, "ok", other.EnvironmentStatus)
	_, err = f.ti.service.GetEnvironmentHeaders(ctx, previewPayload(server.ID, "none", nil))
	require.NoError(t, err)
}
