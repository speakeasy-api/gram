package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/adminmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

func writeContext(t *testing.T, f writeFixture) context.Context {
	t.Helper()
	staff := &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "staff@example.test"}
	principal := Principal{Subject: f.owner.SubjectURN, Email: staff.Email, ClientID: "test-client", ClientRowID: f.owner.ClientRowID.String(), ConnectionID: f.owner.ConnectionID.String(), Generation: f.owner.Generation.String(), Scopes: []string{ScopeRead, ScopeWrite}, staff: staff}
	return contextvalues.SetAdminAuthContext(context.WithValue(t.Context(), principalKey{}, principal), staff)
}

// renameOrganization stands in for a business mutator.
func renameOrganization(id, name string, fail error) func(context.Context, pgx.Tx) (bool, error) {
	return func(ctx context.Context, tx pgx.Tx) (bool, error) {
		renamed, err := repo.New(tx).RenameOrganizationFixture(ctx, repo.RenameOrganizationFixtureParams{Name: name, ID: id})
		if err != nil {
			return false, err //nolint:wrapcheck // Test mutator.
		}
		if fail != nil {
			return false, fail
		}
		return renamed == 1, nil
	}
}

func fixtureAuthority(ctx context.Context, t *testing.T) writeAuthority {
	t.Helper()
	authority, err := requireWriteIdentity(ctx)
	require.NoError(t, err)
	authority.Operation = OperationSetOrganizationFeature
	return authority
}

func TestRunWriteRecordsExecutedEventWithTarget(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_run_write")
	ctx := writeContext(t, f)
	renamed, err := runWrite(ctx, f.runner, fixtureAuthority(ctx, t), writeTarget{OrganizationID: f.orgA}, renameOrganization(f.orgA, "Renamed A", nil))
	require.NoError(t, err)
	require.True(t, renamed)
	require.Equal(t, "Renamed A", organizationName(t, f.db, f.orgA))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetOrganizationFeature, f.orgA, writeEventExecuted))
}

func TestRunWriteRecordsGlobalWritesWithoutOrganization(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_run_write_global")
	ctx := writeContext(t, f)
	_, err := runWrite(ctx, f.runner, fixtureAuthority(ctx, t), writeTarget{ResourceKind: "registry_entry", ResourceID: "io.example/fixture"}, renameOrganization(f.orgA, "Renamed A", nil))
	require.NoError(t, err)
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetOrganizationFeature, "", writeEventExecuted))
}

func TestRunWriteFailureRollsBackAndLeavesFailureEvent(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_run_write_failure")
	ctx := writeContext(t, f)
	failure := errors.New("synthetic failure")
	_, err := runWrite(ctx, f.runner, fixtureAuthority(ctx, t), writeTarget{OrganizationID: f.orgA}, renameOrganization(f.orgA, "Renamed A", failure))
	require.ErrorIs(t, err, failure)
	require.Equal(t, "Synthetic A", organizationName(t, f.db, f.orgA))
	require.Zero(t, countWriteEvents(t, f.db, OperationSetOrganizationFeature, f.orgA, writeEventExecuted))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetOrganizationFeature, f.orgA, writeEventFailed))
}

func TestRunWriteRefusesRevokedAndReauthorizedConnections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*testing.T, writeFixture)
	}{
		{"reconsent", func(t *testing.T, f writeFixture) { t.Helper(); f.reauth() }},
		{"revoked client", func(t *testing.T, f writeFixture) {
			t.Helper()
			require.NoError(t, repo.New(f.db).RevokeOAuthClientFixture(t.Context(), f.owner.ClientRowID))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newWriteFixture(t, "admin_mcp_run_write_connection")
			ctx := writeContext(t, f)
			tc.change(t, f)
			_, err := runWrite(ctx, f.runner, fixtureAuthority(ctx, t), writeTarget{OrganizationID: f.orgA}, renameOrganization(f.orgA, "Renamed A", nil))
			require.ErrorIs(t, err, ErrConnectionChanged)
			require.Equal(t, "Synthetic A", organizationName(t, f.db, f.orgA))
		})
	}
}

func TestRunWriteOnRunsOnTheCallersConnection(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_run_write_on")
	ctx := writeContext(t, f)
	conn, err := f.db.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	var callerPID, writePID uint32
	callerPID = conn.Conn().PgConn().PID()
	_, err = runWriteOn(ctx, f.runner, conn, fixtureAuthority(ctx, t), writeTarget{OrganizationID: f.orgA}, func(ctx context.Context, tx pgx.Tx) (bool, error) {
		writePID = tx.Conn().PgConn().PID()
		return renameOrganization(f.orgA, "Renamed A", nil)(ctx, tx)
	})
	require.NoError(t, err)
	require.Equal(t, callerPID, writePID)
}

func TestAttachWritesRequiresDependencies(t *testing.T) {
	t.Parallel()
	deps := WriteDependencies{Features: &productfeatures.Client{}, Registry: mcpregistry.New(nil, nil)}
	require.ErrorContains(t, AttachWrites(&Runtime{}, &StaffOAuth{}, deps, WriteConfig{}), "not configured")
}

// Every write must carry the confirmation request, because instructions are
// now the only thing between an agent and a customer-visible change.
func TestEveryWriteToolAsksForConfirmation(t *testing.T) {
	t.Parallel()
	runtime := NewRuntime(nil, "")
	runtime.catalogPromotions = &catalogpromotion.Service{}
	deps := WriteDependencies{Features: &productfeatures.Client{}, Registry: mcpregistry.New(nil, nil), ProjectRedeployer: &fakeProjectRedeployer{}, ImageUploads: &fakeImageUploads{}}
	require.NoError(t, AttachWrites(runtime, &StaffOAuth{runner: &writeRunner{}}, deps, WriteConfig{Enabled: true}))

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err := runtime.server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	writeTools := 0
	for tool, err := range session.Tools(t.Context(), nil) {
		require.NoError(t, err)
		require.NotContains(t, tool.Name, "proposal")
		require.False(t, strings.HasPrefix(tool.Name, "prepare_"), tool.Name)
		if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
			continue
		}
		writeTools++
		require.True(t, strings.HasSuffix(tool.Description, writeConfirmationNotice), tool.Name)
		require.Equal(t, 1, strings.Count(tool.Description, "Requires admin:write"), tool.Name)
		require.NotNil(t, tool.Annotations.DestructiveHint, tool.Name)
		require.True(t, *tool.Annotations.DestructiveHint, tool.Name)
	}
	require.Equal(t, len(AllWriteOperations), writeTools)
}

func TestServerInstructionsRequireExplicitConfirmation(t *testing.T) {
	t.Parallel()
	require.Contains(t, adminServerInstructions, "explicitly confirmed that exact change")
	require.Contains(t, adminServerInstructions, "is not confirmation")
	require.NotContains(t, adminServerInstructions, "proposal")
}

func TestWriteContextReportsOnlyExecutableOperations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                       string
		granted, enabled, attached bool
	}{
		{name: "read only", enabled: true, attached: true},
		{name: "write granted but disabled", granted: true, attached: true},
		{name: "write dependency unavailable", granted: true, enabled: true},
		{name: "executable", granted: true, enabled: true, attached: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			principal := staffPrincipal()
			if tc.granted {
				principal.Scopes = append(principal.Scopes, ScopeWrite)
			}
			runtime := NewRuntime(&testAuthenticator{principal: principal}, "https://admin.example.test/admin-mcp")
			if tc.attached {
				oauth := &StaffOAuth{runner: &writeRunner{}}
				config := WriteConfig{Enabled: tc.enabled}
				require.NoError(t, AttachWrites(runtime, oauth, WriteDependencies{Features: &productfeatures.Client{}, Registry: mcpregistry.New(nil, nil)}, config))
			}
			request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_admin_context","arguments":{}}}`))
			request.Header.Set("Authorization", "Bearer test-token")
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			response := httptest.NewRecorder()
			runtime.Handler().ServeHTTP(response, request)
			require.Equal(t, 200, response.Code)
			body := response.Body.String()
			if tc.granted && tc.enabled && tc.attached {
				require.Contains(t, body, `"available_write_operations":["set_organization_feature",`)
				require.Contains(t, body, "set_organization_chat_analysis_settings")
				for _, op := range []WriteOperation{OperationUploadPlatformImage, OperationCurateRegistryPromotionCandidate, OperationRetryRegistryPromotionDocs, OperationPromoteRegistryCandidate} {
					require.NotContains(t, body, string(op), "a write missing its dependency is not reported")
				}
				require.Contains(t, body, `"read_only":false`)
			} else {
				require.Contains(t, body, `"available_write_operations":[]`)
				require.Contains(t, body, `"read_only":true`)
			}
		})
	}
}

func TestWriteOutputPassesSDKOutputValidation(t *testing.T) {
	t.Parallel()
	type output struct {
		Before json.RawMessage `json:"before"`
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	addWriteTool(server, &mcp.Tool{Name: "write"}, func(context.Context, struct{}) (output, error) {
		return output{Before: json.RawMessage(`{"name":"io.example/fixture"}`)}, nil
	})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "1"}, nil).Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "write", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError, "an object result must satisfy the inferred output schema")
}
