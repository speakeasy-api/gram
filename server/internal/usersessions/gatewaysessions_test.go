package usersessions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformusersessions "github.com/speakeasy-api/gram/server/internal/platformtools/usersessions"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func seedRecordedSession(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, subject string, identity string, toolSelection []byte) string {
	t.Helper()

	now := time.Now()
	row, err := repo.New(ti.conn).CreateUserSession(ctx, repo.CreateUserSessionParams{
		UserSessionIssuerID:    issuerID,
		UserSessionClientID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SubjectUrn:             urn.NewUserSubject(subject),
		AuthorizerUserID:       pgtype.Text{String: "", Valid: false},
		DelegatedGrants:        nil,
		DelegatedGrantsVersion: pgtype.Int4{Int32: 0, Valid: false},
		Jti:                    "jti-" + uuid.NewString(),
		RefreshTokenHash:       conv.ToPGText("hash-" + uuid.NewString()),
		RefreshExpiresAt:       pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: 0, Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: 0, Valid: true},
		ToolSelection:          toolSelection,
		Resource:               pgtype.Text{String: "", Valid: false},
		ResourceIdentity:       conv.ToPGTextEmpty(identity),
		CredentialID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SignInID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SignInUserID:           pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return row.ID.String()
}

func seedGateway(t *testing.T, ctx context.Context, ti *testInstance, name string, issuerID uuid.NullUUID) metamcprepo.MetaMcpServer {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	gateway, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Name:                name,
		UserSessionIssuerID: issuerID,
	})
	require.NoError(t, err)
	return gateway
}

func gatewayIdentity(gateway metamcprepo.MetaMcpServer) string {
	return "meta_mcp_server:" + gateway.ID.String()
}

func TestListUserSessions_GatewayScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	shared := seedIssuer(t, ctx, ti, "gateway-scope-shared")
	former := seedIssuer(t, ctx, ti, "gateway-scope-former")
	g1 := seedGateway(t, ctx, ti, "Gateway one", uuid.NullUUID{UUID: shared, Valid: true})
	g3 := seedGateway(t, ctx, ti, "Gateway three", uuid.NullUUID{UUID: shared, Valid: true})
	g4 := seedGateway(t, ctx, ti, "Gateway without issuer", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	fresh := seedGateway(t, ctx, ti, "Fresh gateway", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	catalog, err := json.Marshal(map[string]string{"resource": gatewayIdentity(g1)})
	require.NoError(t, err)
	otherCatalog, err := json.Marshal(map[string]string{"resource": "mcp_server:" + uuid.NewString()})
	require.NoError(t, err)

	recordedG1 := seedRecordedSession(t, ctx, ti, shared, "scope-recorded-g1", gatewayIdentity(g1), nil)
	recordedG3 := seedRecordedSession(t, ctx, ti, shared, "scope-recorded-g3", gatewayIdentity(g3), nil)
	recordedG3CatalogG1 := seedRecordedSession(t, ctx, ti, shared, "scope-recorded-g3-catalog-g1", gatewayIdentity(g3), catalog)
	legacyCatalogG1 := seedRecordedSession(t, ctx, ti, shared, "scope-catalog-g1", "", catalog)
	seedRecordedSession(t, ctx, ti, shared, "scope-unrecorded", "", nil)
	seedRecordedSession(t, ctx, ti, shared, "scope-other-catalog", "", otherCatalog)
	seedRecordedSession(t, ctx, ti, shared, "scope-member-server", "mcp_server:"+uuid.NewString(), nil)
	formerIssuerG1 := seedRecordedSession(t, ctx, ti, former, "scope-former-g1", gatewayIdentity(g1), nil)
	historyG4 := seedRecordedSession(t, ctx, ti, former, "scope-history-g4", gatewayIdentity(g4), nil)

	sibling := seedSiblingProject(t, ctx, ti, "gateway-scope-sibling")
	foreign := seedRecordedSession(t, ctx, ti, sibling.issuerID, "scope-sibling-names-g1", gatewayIdentity(g1), nil)

	list := func(payload gen.ListUserSessionsPayload) []string {
		t.Helper()
		return searchSessionIDs(t, ctx, ti, &payload)
	}
	g1ID, g3ID, g4ID, freshID := g1.ID.String(), g3.ID.String(), g4.ID.String(), fresh.ID.String()

	require.ElementsMatch(t, []string{recordedG1, legacyCatalogG1, formerIssuerG1}, list(gen.ListUserSessionsPayload{MetaMcpServerID: &g1ID}),
		"recorded and catalogued sessions, including those on a former issuer, but nothing unrecorded or recorded for another resource")
	require.NotContains(t, list(gen.ListUserSessionsPayload{MetaMcpServerID: &g1ID}), foreign)
	require.ElementsMatch(t, []string{recordedG3, recordedG3CatalogG1}, list(gen.ListUserSessionsPayload{MetaMcpServerID: &g3ID}),
		"a gateway sharing the issuer sees only its own, and a recorded identity outranks a conflicting catalog")
	require.Equal(t, []string{historyG4}, list(gen.ListUserSessionsPayload{MetaMcpServerID: &g4ID}), "a gateway with no current issuer keeps its recorded history")
	require.Empty(t, list(gen.ListUserSessionsPayload{MetaMcpServerID: &freshID}))

	require.Equal(t, []string{formerIssuerG1}, list(gen.ListUserSessionsPayload{MetaMcpServerID: &g1ID, Search: new("scope-former")}))
	sharedID := shared.String()
	require.ElementsMatch(t, []string{recordedG1, legacyCatalogG1}, list(gen.ListUserSessionsPayload{MetaMcpServerID: &g1ID, UserSessionIssuerID: &sharedID}))
	otherIssuer := seedIssuer(t, ctx, ti, "gateway-scope-other").String()
	require.Empty(t, list(gen.ListUserSessionsPayload{MetaMcpServerID: &g1ID, UserSessionIssuerID: &otherIssuer}), "other filters narrow the gateway scope, never widen it")
}

func TestListUserSessions_GatewayScopeRejectsUnknownGateways(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	deleted := seedGateway(t, ctx, ti, "Deleted gateway", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	_, err := metamcprepo.New(ti.conn).DeleteMetaMCPServer(ctx, metamcprepo.DeleteMetaMCPServerParams{ID: deleted.ID, OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	siblingProject := createSiblingProject(t, ctx, ti.conn, "gateway-scope-foreign")
	foreign, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           siblingProject,
		Name:                "Sibling gateway",
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		id   string
		code oops.Code
	}{
		"deleted":         {id: deleted.ID.String(), code: oops.CodeNotFound},
		"another project": {id: foreign.ID.String(), code: oops.CodeNotFound},
		"missing":         {id: uuid.NewString(), code: oops.CodeNotFound},
		"not a uuid":      {id: "gateway", code: oops.CodeBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{MetaMcpServerID: &tc.id})
			requireOopsCode(t, err, tc.code)
		})
	}
}

func TestPlatformListUserSessions_GatewayScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedIssuer(t, ctx, ti, "platform-gateway-scope")
	g1 := seedGateway(t, ctx, ti, "Platform gateway one", uuid.NullUUID{UUID: issuerID, Valid: true})
	g2 := seedGateway(t, ctx, ti, "Platform gateway two", uuid.NullUUID{UUID: issuerID, Valid: true})
	mine := seedRecordedSession(t, ctx, ti, issuerID, "platform-scope-g1", gatewayIdentity(g1), nil)
	seedRecordedSession(t, ctx, ti, issuerID, "platform-scope-g2", gatewayIdentity(g2), nil)

	call := func(input map[string]any) ([]string, error) {
		t.Helper()
		body, err := json.Marshal(input)
		require.NoError(t, err)
		var out bytes.Buffer
		if err := platformusersessions.NewListUserSessionsTool(ti.conn).Call(ctx, toolconfig.ToolCallEnv{}, bytes.NewReader(body), &out); err != nil {
			return nil, fmt.Errorf("call list tool: %w", err)
		}
		var result struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal(out.Bytes(), &result))
		ids := []string{}
		for _, item := range result.Items {
			ids = append(ids, item.ID)
		}
		return ids, nil
	}

	got, err := call(map[string]any{"meta_mcp_server_id": g1.ID.String()})
	require.NoError(t, err)
	require.Equal(t, []string{mine}, got)

	_, err = call(map[string]any{"meta_mcp_server_id": uuid.NewString()})
	requireOopsCode(t, err, oops.CodeNotFound)
}
