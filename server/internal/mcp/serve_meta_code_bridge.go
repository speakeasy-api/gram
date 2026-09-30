package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	metavisibility "github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// metaCodeBackend retains admission identity; the runner cannot choose routing
// or credentials. Current membership and authorization are loaded per operation.
type metaCodeBackend struct {
	service  *Service
	gate     metaGateContext
	admitted map[string]uuid.UUID
	meta     *mcprequests.WireMeta
	boundary authz.AdmissionBoundary
	refresh  metaCodeGateRefresher
	endpoint endpointrepo.McpEndpoint
	issuer   uuid.NullUUID
	surface  networkaccess.Surface
	revoked  atomic.Bool
}

// metaCodeGateRefresher must re-run endpoint binding and issuer admission using
// the original request credentials, without writing a public HTTP response.
// Session revocation, expiry, policy, and upstream tokens are checked here.
type metaCodeGateRefresher func(context.Context, *metamcprepo.MetaMcpServer) (context.Context, *metaGateContext, error)

var _ codemode.Backend = (*metaCodeBackend)(nil)

func newMetaCodeBackend(ctx context.Context, service *Service, gate *metaGateContext, members []metaMember, endpoint endpointrepo.McpEndpoint, issuer uuid.NullUUID, meta *mcprequests.WireMeta, refresh metaCodeGateRefresher) (*metaCodeBackend, error) {
	admitted := make(map[string]uuid.UUID, len(members))
	for _, member := range members {
		admitted[member.slug] = member.serverID
	}
	var boundary authz.AdmissionBoundary
	// Public gateway callers have no RBAC identity. Their authority is the
	// public member snapshot and the unchanged anonymous gate, not user grants.
	if auth, ok := contextvalues.GetAuthContext(ctx); ok && auth != nil {
		var err error
		boundary, err = service.authz.CaptureAdmissionBoundary(ctx)
		if err != nil {
			return nil, fmt.Errorf("capture code admission: %w", err)
		}
	}
	surface := networkaccess.SurfacePublic
	if origin, ok := requestorigin.FromContext(ctx); ok && origin.Surface == requestorigin.SurfacePrivateNetwork {
		surface = networkaccess.SurfacePrivate
	}
	return &metaCodeBackend{service: service, gate: *gate, admitted: admitted, meta: meta, boundary: boundary, refresh: refresh, endpoint: endpoint, issuer: issuer, surface: surface, revoked: atomic.Bool{}}, nil
}

func (b *metaCodeBackend) current(ctx context.Context) (context.Context, *metaGateContext, []metaMember, error) {
	if err := ctx.Err(); err != nil {
		return ctx, nil, nil, fmt.Errorf("callback cancelled: %w", err)
	}
	if b.revoked.Load() {
		return ctx, nil, nil, codemode.ErrExecutionRevoked
	}
	if b.gate.agentID != uuid.Nil || b.refresh == nil {
		return ctx, nil, nil, b.revoke()
	}
	endpoint, err := endpointrepo.New(b.service.db).GetMCPEndpointByID(ctx, endpointrepo.GetMCPEndpointByIDParams{ID: b.endpoint.ID, ProjectID: b.gate.projectID})
	if err != nil {
		return ctx, nil, nil, b.refreshFailure(ctx, err)
	}
	if endpoint.MetaMcpServerID != b.endpoint.MetaMcpServerID || endpoint.Slug != b.endpoint.Slug || endpoint.CustomDomainID != b.endpoint.CustomDomainID {
		return ctx, nil, nil, b.revoke()
	}
	server, err := metamcprepo.New(b.service.db).GetMetaMCPServerByIDAndProjectID(ctx, metamcprepo.GetMetaMCPServerByIDAndProjectIDParams{ID: b.gate.metaServerID, ProjectID: b.gate.projectID})
	if err != nil {
		return ctx, nil, nil, b.refreshFailure(ctx, err)
	}
	if server.OrganizationID != b.gate.organizationID || server.Visibility == metavisibility.Disabled || server.UserSessionIssuerID != b.issuer {
		return ctx, nil, nil, b.revoke()
	}
	mode, err := networkaccess.Effective(server.NetworkAccessMode)
	if err != nil || !mode.Allows(b.surface) {
		return ctx, nil, nil, b.revoke()
	}
	freshCtx, gate, err := b.refresh(ctx, &server)
	if err != nil {
		return ctx, nil, nil, b.refreshFailure(ctx, err)
	}
	if gate == nil || freshCtx == nil {
		return ctx, nil, nil, b.revoke()
	}
	ctx = freshCtx
	if gate.projectID != b.gate.projectID || gate.metaServerID != b.gate.metaServerID || gate.organizationID != b.gate.organizationID || gate.userID != b.gate.userID || gate.externalUserID != b.gate.externalUserID || gate.apiKeyID != b.gate.apiKeyID || gate.authenticated != b.gate.authenticated || gate.discoveryMode != b.gate.discoveryMode {
		return ctx, nil, nil, b.revoke()
	}
	// Policy changes end the run instead of widening a previously admitted job.
	before, err := json.Marshal(struct {
		Selection *toolfilter.SessionSelection `json:"selection"`
		Frozen    *toolfilter.FrozenToolset    `json:"frozen"`
	}{Selection: b.gate.toolSelection, Frozen: b.gate.frozen})
	if err != nil {
		return ctx, nil, nil, b.revoke()
	}
	after, err := json.Marshal(struct {
		Selection *toolfilter.SessionSelection `json:"selection"`
		Frozen    *toolfilter.FrozenToolset    `json:"frozen"`
	}{Selection: gate.toolSelection, Frozen: gate.frozen})
	if err != nil || !bytes.Equal(before, after) {
		return ctx, nil, nil, b.revoke()
	}
	ctx = b.boundary.Apply(ctx)
	freshCtx, err = b.service.authz.RefreshContext(ctx)
	if err != nil {
		return ctx, nil, nil, b.refreshFailure(ctx, err)
	}
	ctx = freshCtx
	freshCtx, members, err := b.service.resolveMetaMemberSnapshot(ctx, b.service.logger, gate.metaServerID, gate.projectID)
	if err != nil {
		return ctx, nil, nil, b.refreshFailure(ctx, err)
	}
	ctx = freshCtx
	permitted := make([]metaMember, 0, len(members))
	for _, member := range members {
		if b.admitted[member.slug] != member.serverID {
			continue
		}
		if gate.frozen != nil && !frozenIncludesMember(gate.frozen, member.serverID) {
			continue
		}
		permitted = append(permitted, member)
	}
	return ctx, gate, permitted, nil
}

func (b *metaCodeBackend) revoke() error {
	b.revoked.Store(true)
	return codemode.ErrExecutionRevoked
}

func (b *metaCodeBackend) refreshFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("refresh cancelled: %w", ctx.Err())
	}
	var denied *oops.ShareableError
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &denied) && (denied.Code == oops.CodeForbidden || denied.Code == oops.CodeUnauthorized || denied.Code == oops.CodeNotFound)) {
		return b.revoke()
	}
	return codemode.ErrToolUnavailable
}

func (b *metaCodeBackend) Members(ctx context.Context) ([]codemode.Server, error) {
	_, _, members, err := b.current(ctx)
	if err != nil {
		return nil, err
	}
	servers := make([]codemode.Server, 0, len(members))
	for _, member := range members {
		servers = append(servers, codemode.Server{Slug: member.slug, Name: member.name})
	}
	return servers, nil
}

func (b *metaCodeBackend) List(ctx context.Context, slug string) (*codemode.Catalog, error) {
	ctx, gate, members, err := b.current(ctx)
	if err != nil {
		return nil, err
	}
	member, ok := findMetaMember(members, slug)
	if !ok {
		return nil, codemode.ErrToolUnavailable
	}
	catalog, err := b.service.describeMetaMember(ctx, b.service.logger, gate, member)
	if err != nil {
		return nil, codemode.ErrToolUnavailable
	}
	result := &codemode.Catalog{Tools: make([]codemode.Tool, 0, len(catalog.entries)), Incomplete: catalog.incomplete}
	for _, entry := range catalog.entries {
		frozen, err := frozenMemberTool(member, catalog.routingIdentity, entry)
		if err != nil || len(frozen.Name) > 1024 {
			result.Incomplete = true
			continue
		}
		result.Tools = append(result.Tools, codemode.Tool{Path: frozen.Name, Name: entry.Name, Description: entry.Description, Definition: frozen.Definition, Fingerprint: frozen.Fingerprint})
	}
	return result, nil
}

func (b *metaCodeBackend) Invoke(ctx context.Context, path, fingerprint string, arguments json.RawMessage) (json.RawMessage, error) {
	ctx, gate, members, err := b.current(ctx)
	if err != nil {
		if errors.Is(err, codemode.ErrExecutionRevoked) {
			return nil, err
		}
		return nil, &codemode.DispatchError{BeforeInvoke: true, Code: "tool_unavailable"}
	}
	slug, _, err := metamcp.SplitQualifiedName(path)
	if err != nil {
		return nil, &codemode.DispatchError{BeforeInvoke: true, Code: "tool_unavailable"}
	}
	member, ok := findMetaMember(members, slug)
	if !ok {
		return nil, &codemode.DispatchError{BeforeInvoke: true, Code: "tool_unavailable"}
	}
	pinned := toolfilter.FrozenTool{Name: path, MemberID: member.serverID, Fingerprint: fingerprint, RoutingIdentity: "", Definition: nil}
	if gate.frozen != nil && !gate.frozen.Allows(pinned) {
		return nil, &codemode.DispatchError{BeforeInvoke: true, Code: "tool_changed"}
	}
	// Only the expected identity crosses this boundary. The existing dispatcher
	// reconstructs the actual identity at its authoritative pre-invocation check.
	callGate := *gate
	callGate.frozen = &toolfilter.FrozenToolset{Tools: []toolfilter.FrozenTool{pinned}}
	req := &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.StringID(uuid.NewString()), Method: "tools/call", Params: nil}
	invocation := &metaCodeInvocation{dispatched: false}
	ctx = context.WithValue(ctx, metaCodeInvocationKey{}, invocation)
	raw, err := b.service.executeMetaMemberTool(ctx, b.service.logger, &callGate, members, req, path, arguments, b.meta)
	if err != nil {
		if refusal, ok := errors.AsType[*codemode.DispatchError](err); ok {
			return nil, refusal
		}
		return nil, invocation.failure()
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Result) == 0 {
		return nil, fmt.Errorf("invalid member execution result")
	}
	return envelope.Result, nil
}

type metaCodeInvocationKey struct{}

// One invocation owns this state; it is never shared across parallel callbacks.
type metaCodeInvocation struct {
	dispatched bool
}

func (i *metaCodeInvocation) failure() error {
	if !i.dispatched {
		return &codemode.DispatchError{BeforeInvoke: true, Code: "tool_unavailable"}
	}
	return &codemode.DispatchError{BeforeInvoke: false, Code: "tool_call_outcome_unknown"}
}
