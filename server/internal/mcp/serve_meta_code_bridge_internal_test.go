package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestCodeBridgeTemporaryFailureIsNotRevocation(t *testing.T) {
	t.Parallel()
	backend := &metaCodeBackend{}
	require.ErrorIs(t, backend.refreshFailure(t.Context(), errors.New("temporary database failure")), codemode.ErrToolUnavailable)
	require.False(t, backend.revoked.Load())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, backend.refreshFailure(ctx, ctx.Err()), context.Canceled)
	require.False(t, backend.revoked.Load())
}

// NewTestMetaCodeHost lets the external integration fixtures exercise the real
// private bridge without exposing gateway routing state in the production API.
func NewTestMetaCodeHost(t *testing.T, ctx context.Context, service *Service, gatewayID uuid.UUID, selection *toolfilter.SessionSelection) codemode.Host {
	t.Helper()
	host, err := codemode.NewHost(ctx, NewTestMetaCodeBackend(t, ctx, service, gatewayID, selection), uuid.NewString(), nil)
	require.NoError(t, err)
	return host
}

// NewTestMetaCodeBackend builds the private bridge backend over the gateway's
// current members, binding a selection to those members as consent would.
func NewTestMetaCodeBackend(t *testing.T, ctx context.Context, service *Service, gatewayID uuid.UUID, selection *toolfilter.SessionSelection) codemode.Backend {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	// Shared fixtures attach synthetic admin grants; this bridge reloads
	// storage, so seed an actual grant for subsequent operations.
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, authz.WildcardResource).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(service.db).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: auth.ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID), Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	ctx, members, err := service.resolveMetaMemberSnapshot(ctx, service.logger, gatewayID, *auth.ProjectID)
	require.NoError(t, err)
	var selectionMembers map[string]uuid.UUID
	if selection != nil {
		selectionMembers = make(map[string]uuid.UUID, len(members))
		for _, member := range members {
			selectionMembers[member.slug] = member.serverID
		}
	}
	gate := &metaGateContext{projectID: *auth.ProjectID, metaServerID: gatewayID, organizationID: auth.ActiveOrganizationID, authenticated: true, userID: auth.UserID, sessionID: uuid.NewString(), toolSelection: selection, selectionMembers: selectionMembers, protocolVersion: mcpversions.Resolution{InEffect: mcpversions.DefaultInEffect}}
	server, err := metamcprepo.New(service.db).GetMetaMCPServerByIDAndProjectID(ctx, metamcprepo.GetMetaMCPServerByIDAndProjectIDParams{ID: gatewayID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	endpoints, err := endpointrepo.New(service.db).ListMCPEndpointsByMetaMCPServerID(ctx, endpointrepo.ListMCPEndpointsByMetaMCPServerIDParams{ProjectID: *auth.ProjectID, MetaMcpServerID: gatewayID})
	require.NoError(t, err)
	require.NotEmpty(t, endpoints)
	backend, err := newMetaCodeBackend(ctx, service, gate, members, endpoints[0], server.UserSessionIssuerID, nil, func(ctx context.Context, _ *metamcprepo.MetaMcpServer) (context.Context, *metaGateContext, error) {
		return ctx, gate, nil
	})
	require.NoError(t, err)
	t.Cleanup(func() { backend.sessions.end(context.WithoutCancel(ctx)) })
	return backend
}

// NewTestSharedMetaCodeBackend builds a backend for a verified caller holding
// credential, whose member sessions outlive one execution.
func NewTestSharedMetaCodeBackend(t *testing.T, ctx context.Context, service *Service, gatewayID uuid.UUID, credential string) codemode.Backend {
	t.Helper()
	ctx = context.WithValue(ctx, metaCodeCancellableKey{}, true)
	ctx = context.WithValue(ctx, metaCodeCredentialKey{}, credential)
	return NewTestMetaCodeBackend(t, ctx, service, gatewayID, nil)
}

// SetTestCodeSessionPool replaces the service's shared member session pool.
func SetTestCodeSessionPool(service *Service, idleTTL, pingAfter time.Duration, maxEntries int) {
	service.codeSessions = newCodeMemberSessions(service.logger, idleTTL, codeSessionMaxAge, pingAfter, maxEntries)
}

// CloseTestMetaCodeSessions ends a test backend's execution-scoped member sessions.
func CloseTestMetaCodeSessions(ctx context.Context, backend codemode.Backend) {
	if b, ok := backend.(*metaCodeBackend); ok {
		b.sessions.end(ctx)
	}
}

// SetTestCodeExecutor injects the real local transport before serving integration requests.
func SetTestCodeExecutor(service *Service, executor codemode.Executor) {
	service.codeExecutor = executor
}
