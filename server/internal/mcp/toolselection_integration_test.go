package mcp_test

// Integration coverage for the consent-screen tool selection on the serve
// path: the policy loads for anonymous subjects, narrows tools/list and
// tools/call identically, fails closed on resource mismatch / malformed
// policies / missing session rows, and NULL still means all tools.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	deployments_repo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	tools_repo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// seedSelectionToolset builds a PUBLIC issuer-gated toolset with three HTTP
// tools: reader (readOnlyHint true), writer and eraser (unannotated). Public
// keeps RBAC out of the way and exercises the anonymous-subject path — the
// one that early-returns before AuthContext is stamped.
func seedSelectionToolset(t *testing.T, ctx context.Context, ti *testInstance) (toolsets_repo.Toolset, usersessions_repo.UserSessionIssuer) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "selection-"+uuid.NewString()[:8])

	deploymentID, err := deployments_repo.New(ti.conn).InsertDeployment(ctx, deployments_repo.InsertDeploymentParams{
		ProjectID:      toolset.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "test-user",
		IdempotencyKey: uuid.New().String(),
	})
	require.NoError(t, err)
	require.NoError(t, deployments_repo.New(ti.conn).CreateDeploymentStatus(ctx, deployments_repo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID,
		Status:       "completed",
	}))

	toolURNs := make([]urn.Tool, 0, 3)
	for _, tool := range []struct {
		name     string
		readOnly pgtype.Bool
	}{
		{name: "reader", readOnly: pgtype.Bool{Bool: true, Valid: true}},
		{name: "writer", readOnly: pgtype.Bool{}},
		{name: "eraser", readOnly: pgtype.Bool{}},
	} {
		toolURN := urn.NewTool(urn.ToolKindHTTP, tool.name, uuid.New().String()[:8])
		toolURNs = append(toolURNs, toolURN)
		require.NoError(t, tools_repo.New(ti.conn).CreateHTTPToolDefinition(ctx, tools_repo.CreateHTTPToolDefinitionParams{
			ProjectID:       toolset.ProjectID,
			DeploymentID:    deploymentID,
			ToolUrn:         toolURN,
			Name:            tool.name,
			UntruncatedName: pgtype.Text{},
			Summary:         tool.name + " summary",
			Description:     tool.name + " description",
			Tags:            []string{},
			HttpMethod:      "GET",
			Path:            "/test",
			SchemaVersion:   "3.0.0",
			Schema:          []byte(`{}`),
			ServerEnvVar:    "TEST_SERVER_URL",
			Security:        []byte(`[]`),
			HeaderSettings:  []byte(`{}`),
			QuerySettings:   []byte(`{}`),
			PathSettings:    []byte(`{}`),
			ReadOnlyHint:    tool.readOnly,
			DestructiveHint: pgtype.Bool{},
			IdempotentHint:  pgtype.Bool{},
			OpenWorldHint:   pgtype.Bool{},
		}))
	}
	_, err = toolsetsRepo.CreateToolsetVersion(ctx, toolsets_repo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      toolURNs,
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	issuer, err := usersessions_repo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          toolset.ProjectID,
		Slug:               toolset.Slug + "-issuer",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(24 * time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	toolset, err = toolsetsRepo.UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           toolset.ProjectID,
	})
	require.NoError(t, err)

	return toolset, issuer
}

// mintSelectionSession signs a session JWT with the test signer and persists
// its user_sessions row carrying the given raw tool_selection (nil = NULL).
func mintSelectionSession(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, issuer usersessions_repo.UserSessionIssuer, selection []byte) string {
	t.Helper()

	signer := usersessions.NewSigner("test-jwt-secret")
	subject := urn.NewAnonymousSubject(uuid.NewString())
	access, jti, err := signer.Mint(usersessions.MintParams{
		Subject:  subject,
		Audience: urn.NewToolset(toolset.ID).String(),
		Issuer:   "https://test.example",
		Lifetime: time.Hour,
		ClientID: "test-client",
	})
	require.NoError(t, err)

	now := time.Now()
	_, err = usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: issuer.ID,
		UserSessionClientID: uuid.NullUUID{},
		SubjectUrn:          subject,
		Jti:                 jti,
		RefreshTokenHash:    "test-selection-" + uuid.NewString(),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
		ToolSelection:       selection,
	})
	require.NoError(t, err)
	return access
}

func TestToolSelection_NullSelectionServesAllTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer := seedSelectionToolset(t, ctx, ti)
	access := mintSelectionSession(t, ctx, ti, toolset, issuer, nil)

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), access, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	names := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	require.ElementsMatch(t, []string{"reader", "writer", "eraser"}, names)
}

func TestToolSelection_NameSnapshotNarrowsListAndCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer := seedSelectionToolset(t, ctx, ti)
	selection := fmt.Appendf(nil, `{"resource":"toolset:%s","grant_id":"%s","allow":[{"type":"tool","name":"writer"}]}`, toolset.ID, uuid.NewString())
	access := mintSelectionSession(t, ctx, ti, toolset, issuer, selection)

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), access, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	names := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	require.Equal(t, []string{"writer"}, names)

	// tools/call parity: a deselected tool is method-not-found; the selected
	// one resolves past the filter (its execution then fails on the missing
	// upstream env, which is fine — it must NOT be "tool not found").
	w, err = servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsCallBody("reader"), access, nil)
	if err != nil {
		require.Contains(t, err.Error(), "tool not found")
	} else {
		require.Contains(t, w.Body.String(), "tool not found")
	}

	w, err = servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsCallBody("writer"), access, nil)
	combined := w.Body.String()
	if err != nil {
		combined += err.Error()
	}
	require.NotContains(t, combined, "tool not found")
}

func TestToolSelection_LegacyFlatDocumentFailsClosed(t *testing.T) {
	t.Parallel()

	// A pre-allow-model document (flat tools/annotations fields) is
	// server-authored corruption under the current schema: the session
	// fails closed into reauthorization, never into all tools.
	ctx, ti := newTestMCPService(t)
	toolset, issuer := seedSelectionToolset(t, ctx, ti)
	selection := fmt.Appendf(nil, `{"annotations":["read_only"],"tools":["eraser"],"resource":"toolset:%s"}`, toolset.ID)
	access := mintSelectionSession(t, ctx, ti, toolset, issuer, selection)

	_, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), access, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired or invalid access token")
}

func TestToolSelection_LiveAnnotationGrantTracksHints(t *testing.T) {
	t.Parallel()

	// A live annotation grant matches by the tools' CURRENT hints — the
	// grant document names no tools at all.
	ctx, ti := newTestMCPService(t)
	toolset, issuer := seedSelectionToolset(t, ctx, ti)
	selection := fmt.Appendf(nil, `{"resource":"toolset:%s","grant_id":"%s","allow":[{"type":"annotation","name":"read_only","mode":"live"}]}`, toolset.ID, uuid.NewString())
	access := mintSelectionSession(t, ctx, ti, toolset, issuer, selection)

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), access, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	names := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	require.Equal(t, []string{"reader"}, names)
}

func TestToolSelection_SnapshotAnnotationGrantIsFrozen(t *testing.T) {
	t.Parallel()

	// A snapshot annotation grant is its embedded expansion: the annotated
	// reader stays out unless its NAME is in the frozen list.
	ctx, ti := newTestMCPService(t)
	toolset, issuer := seedSelectionToolset(t, ctx, ti)
	selection := fmt.Appendf(nil, `{"resource":"toolset:%s","grant_id":"%s","allow":[{"type":"annotation","name":"read_only","mode":"snapshot","tools":["eraser"]}]}`, toolset.ID, uuid.NewString())
	access := mintSelectionSession(t, ctx, ti, toolset, issuer, selection)

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), access, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	names := toolNames(parseToolsListResponse(t, w.Body.Bytes()))
	require.Equal(t, []string{"eraser"}, names)
}

func TestToolSelection_EmptyPolicyMeansZeroTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer := seedSelectionToolset(t, ctx, ti)
	selection := fmt.Appendf(nil, `{"resource":"toolset:%s","grant_id":"%s","allow":[]}`, toolset.ID, uuid.NewString())
	access := mintSelectionSession(t, ctx, ti, toolset, issuer, selection)

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), access, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, toolNames(parseToolsListResponse(t, w.Body.Bytes())))
}

func TestToolSelection_ResourceMismatchRejects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer := seedSelectionToolset(t, ctx, ti)
	// Bound to a different endpoint sharing the issuer: must 401 into
	// reauth, never be reinterpreted against this endpoint's inventory.
	selection := fmt.Appendf(nil, `{"resource":"toolset:%s","grant_id":"%s","allow":[{"type":"tool","name":"writer"}]}`, uuid.NewString(), uuid.NewString())
	access := mintSelectionSession(t, ctx, ti, toolset, issuer, selection)

	_, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), access, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired or invalid access token")
}

func TestToolSelection_MalformedStoredPolicyRejects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer := seedSelectionToolset(t, ctx, ti)
	// Missing resource binding — fail closed, never "all tools".
	access := mintSelectionSession(t, ctx, ti, toolset, issuer, []byte(`{"annotations":["read_only"]}`))

	_, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), access, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired or invalid access token")
}

func TestToolSelection_MissingSessionRowRejects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _ := seedSelectionToolset(t, ctx, ti)

	// Valid signature, but no user_sessions row for the jti: the policy
	// lookup fails closed rather than serving all tools.
	signer := usersessions.NewSigner("test-jwt-secret")
	access, _, err := signer.Mint(usersessions.MintParams{
		Subject:  urn.NewAnonymousSubject(uuid.NewString()),
		Audience: urn.NewToolset(toolset.ID).String(),
		Issuer:   "https://test.example",
		Lifetime: time.Hour,
		ClientID: "test-client",
	})
	require.NoError(t, err)

	_, serveErr := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsListBody(), access, nil)
	require.Error(t, serveErr)
	require.Contains(t, serveErr.Error(), "expired or invalid access token")
}
