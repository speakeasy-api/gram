package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcprequests"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	metavisibility "github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// metaCodeBackend retains admission identity; the runner cannot choose routing
// or credentials. Current membership and authorization are loaded per operation.
type metaCodeBackend struct {
	service  *Service
	gate     metaGateContext
	admitted map[string]string
	meta     *mcprequests.WireMeta
	boundary authz.AdmissionBoundary
	refresh  metaCodeGateRefresher
	endpoint endpointrepo.McpEndpoint
	issuer   uuid.NullUUID
	surface  networkaccess.Surface
	revoked  atomic.Bool
	sessions *codeSessionScope
}

// metaCodeGateRefresher must re-run endpoint binding and issuer admission using
// the original request credentials, without writing a public HTTP response.
// Session revocation, expiry, policy, and upstream tokens are checked here.
type metaCodeGateRefresher func(context.Context, *metamcprepo.MetaMcpServer) (context.Context, *metaGateContext, error)

var _ codemode.Backend = (*metaCodeBackend)(nil)

func newMetaCodeBackend(ctx context.Context, service *Service, gate *metaGateContext, members []metaMember, endpoint endpointrepo.McpEndpoint, issuer uuid.NullUUID, meta *mcprequests.WireMeta, refresh metaCodeGateRefresher) (*metaCodeBackend, error) {
	admitted := make(map[string]string, len(members))
	for _, member := range members {
		admitted[member.slug] = codeMemberDestination(member)
	}
	var boundary authz.AdmissionBoundary
	// A caller outside the organization has no RBAC grants here. Its authority
	// is the public member snapshot and the unchanged gate, not user grants.
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
	backend := &metaCodeBackend{service: service, gate: *gate, admitted: admitted, meta: meta, boundary: boundary, refresh: refresh, endpoint: endpoint, issuer: issuer, surface: surface, revoked: atomic.Bool{}, sessions: nil}
	backend.gate.consentDiagnostics = &consentDiagnostics{seen: sync.Map{}}

	recheck := func(ctx context.Context, slug string) (context.Context, *metaGateContext, metaMember, error) {
		ctx, gate, members, err := backend.current(ctx, true)
		if err != nil {
			return ctx, nil, metaMember{}, err
		}
		member, ok := findMetaMember(members, slug)
		if !ok {
			return ctx, nil, metaMember{}, codemode.ErrToolUnavailable
		}
		return ctx, gate, member, nil
	}
	// Only a member of the gateway's organization holding a verified
	// credential may reuse sessions across executions, and only its own: the
	// scope holds its identity, credential, consent and admitted grants.
	// Other callers keep sessions to one execution.
	if credential, _ := ctx.Value(metaCodeCredentialKey{}).(string); credential != "" && gate.authenticated && service.codeSessions != nil {
		grants, _ := authz.GrantsFromContext(ctx)
		canonicalGrants := make([]string, 0, len(grants))
		for _, grant := range grants {
			encoded, err := json.Marshal([]any{grant.PrincipalUrn, grant.Scope, grant.Selector})
			if err != nil {
				return nil, fmt.Errorf("key code member grant: %w", err)
			}
			canonicalGrants = append(canonicalGrants, string(encoded))
		}
		slices.Sort(canonicalGrants)
		scope, err := json.Marshal([]any{gate.organizationID, gate.projectID, gate.metaServerID, gate.userID, gate.externalUserID, gate.apiKeyID, credential, gate.catalogRef, canonicalGrants})
		if err != nil {
			return nil, fmt.Errorf("key code member sessions: %w", err)
		}
		affinity := sha256.Sum256(scope)
		backend.sessions = &codeSessionScope{pool: service.codeSessions, callerScope: &codeCallerScope{key: string(scope)}, affinity: "code:" + hex.EncodeToString(affinity[:]), recheck: recheck, owned: false}
		return backend, nil
	}
	backend.sessions = &codeSessionScope{pool: newCodeMemberSessions(service.logger, 0, 0, 0, codeSessionMaxEntries), callerScope: &codeCallerScope{key: "execution:" + uuid.NewString()}, affinity: "code:" + uuid.NewString(), recheck: recheck, owned: true}
	return backend, nil
}

func (b *metaCodeBackend) current(ctx context.Context, execute bool) (context.Context, *metaGateContext, []metaMember, error) {
	if err := ctx.Err(); err != nil {
		return ctx, nil, nil, fmt.Errorf("callback cancelled: %w", err)
	}
	if b.revoked.Load() {
		return ctx, nil, nil, codemode.ErrExecutionRevoked
	}
	if b.gate.agentID != uuid.Nil || b.refresh == nil {
		return ctx, nil, nil, b.revoke()
	}
	var (
		freshCtx context.Context
		gate     *metaGateContext
		err      error
	)
	if b.gate.organizationGateway {
		// No stored endpoint or server rows exist; the refresher reloads the
		// gateway, and members carry their own network access modes.
		freshCtx, gate, err = b.refresh(ctx, nil)
	} else {
		endpoint, readErr := endpointrepo.New(b.service.db).GetMCPEndpointByID(ctx, endpointrepo.GetMCPEndpointByIDParams{ID: b.endpoint.ID, ProjectID: b.gate.projectID})
		if readErr != nil {
			return ctx, nil, nil, b.refreshFailure(ctx, readErr)
		}
		if endpoint.MetaMcpServerID != b.endpoint.MetaMcpServerID || endpoint.Slug != b.endpoint.Slug || endpoint.CustomDomainID != b.endpoint.CustomDomainID {
			return ctx, nil, nil, b.revoke()
		}
		server, readErr := metamcprepo.New(b.service.db).GetMetaMCPServerByIDAndProjectID(ctx, metamcprepo.GetMetaMCPServerByIDAndProjectIDParams{ID: b.gate.metaServerID, ProjectID: b.gate.projectID})
		if readErr != nil {
			return ctx, nil, nil, b.refreshFailure(ctx, readErr)
		}
		if server.OrganizationID != b.gate.organizationID || server.Visibility == metavisibility.Disabled || server.UserSessionIssuerID != b.issuer {
			return ctx, nil, nil, b.revoke()
		}
		mode, modeErr := networkaccess.Effective(server.NetworkAccessMode)
		if modeErr != nil || !mode.Allows(b.surface) {
			return ctx, nil, nil, b.revoke()
		}
		freshCtx, gate, err = b.refresh(ctx, &server)
	}
	if err != nil {
		return ctx, nil, nil, b.refreshFailure(ctx, err)
	}
	if gate == nil || freshCtx == nil {
		return ctx, nil, nil, b.revoke()
	}
	ctx = freshCtx
	copyGate := *gate
	gate = &copyGate
	if gate.projectID != b.gate.projectID || gate.metaServerID != b.gate.metaServerID || gate.organizationID != b.gate.organizationID || gate.userID != b.gate.userID || gate.externalUserID != b.gate.externalUserID || gate.apiKeyID != b.gate.apiKeyID || gate.authenticated != b.gate.authenticated || gate.discoveryMode != b.gate.discoveryMode {
		return ctx, nil, nil, b.revoke()
	}
	// Policy changes end the run instead of widening a previously admitted job.
	if (b.gate.catalogRef == nil) != (gate.catalogRef == nil) ||
		(b.gate.catalogRef != nil && *b.gate.catalogRef != *gate.catalogRef) {
		return ctx, nil, nil, b.revoke()
	}
	// The authenticated reference is unchanged, so reuse this execution's
	// immutable consent record without another Redis read or upstream discovery.
	if b.gate.catalog != nil && !time.Now().Before(b.gate.catalog.ExpiresAt) {
		return ctx, nil, nil, b.revoke()
	}
	gate.catalog = b.gate.catalog
	gate.consentDiagnostics = b.gate.consentDiagnostics
	gate.execute = execute
	ctx = b.boundary.Apply(ctx)
	freshCtx, err = b.service.authz.RefreshContext(ctx)
	if err != nil {
		return ctx, nil, nil, b.refreshFailure(ctx, err)
	}
	ctx = freshCtx
	freshCtx, members, err := b.service.resolveGateMemberSnapshot(ctx, b.service.logger, gate, gate.metaServerID, gate.projectID)
	if err != nil {
		return ctx, nil, nil, b.refreshFailure(ctx, err)
	}
	ctx = freshCtx
	permitted := make([]metaMember, 0, len(members))
	for _, member := range members {
		// Calls no longer re-read a member's catalog, so a member that now
		// routes elsewhere is dropped here rather than at a fingerprint check.
		if b.admitted[member.slug] != codeMemberDestination(member) {
			continue
		}
		permitted = append(permitted, member)
	}
	permitted = b.service.codeCallableMembers(ctx, gate, permitted, true)
	return ctx, gate, permitted, nil
}

// codeHelperFailure keeps a member that needs a new consent distinguishable
// from one that is merely unavailable.
func codeHelperFailure(err error) error {
	if _, consent := errors.AsType[catalogConsentError](err); consent {
		return codemode.ErrConsentRequired
	}
	return codemode.ErrToolUnavailable
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

func (b *metaCodeBackend) Members(ctx context.Context) ([]codemode.Server, codemode.Lister, error) {
	ctx, gate, members, err := b.current(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	servers := make([]codemode.Server, 0, len(members))
	for _, member := range members {
		servers = append(servers, codemode.Server{Slug: member.slug, Name: member.name})
	}
	list := func(callCtx context.Context, slug string) (*codemode.Catalog, error) {
		bound, cancel := boundToCallback(ctx, callCtx)
		defer cancel()
		return b.catalog(bound, gate, members, slug)
	}
	return servers, list, nil
}

// boundToCallback carries a refreshed context's authorization into a
// narrower callback context, ending when either does.
func boundToCallback(refreshed, callback context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(refreshed)
	stop := context.AfterFunc(callback, func() { cancel(context.Cause(callback)) })
	return ctx, func() { stop(); cancel(context.Canceled) }
}

func (b *metaCodeBackend) List(ctx context.Context, slug string) (*codemode.Catalog, error) {
	ctx, gate, members, err := b.current(ctx, false)
	if err != nil {
		return nil, err
	}
	return b.catalog(ctx, gate, members, slug)
}

func (b *metaCodeBackend) catalog(ctx context.Context, gate *metaGateContext, members []metaMember, slug string) (*codemode.Catalog, error) {
	member, ok := findMetaMember(members, slug)
	if !ok {
		return nil, codemode.ErrToolUnavailable
	}
	catalog, err := b.service.describeMetaMember(context.WithValue(ctx, codeMemberSessionsKey{}, b.sessions), b.service.logger, gate, member)
	if err != nil {
		return nil, codeHelperFailure(err)
	}
	result := &codemode.Catalog{Tools: make([]codemode.Tool, 0, len(catalog.entries)), Incomplete: catalog.incomplete}
	for _, entry := range catalog.entries {
		path := metamcp.QualifyName(member.slug, entry.Name)
		definition, err := json.Marshal(entry)
		if err != nil || len(path) > 1024 {
			result.Incomplete = true
			continue
		}
		result.Tools = append(result.Tools, codemode.Tool{Path: path, Name: entry.Name, Description: entry.Description, Definition: definition, Fingerprint: codeToolFingerprint(member, definition)})
	}
	return result, nil
}

func (b *metaCodeBackend) Describe(ctx context.Context, path string) (*codemode.Tool, error) {
	ctx, gate, members, err := b.current(ctx, false)
	if err != nil {
		return nil, err
	}
	slug, name, err := metamcp.SplitQualifiedName(path)
	if err != nil {
		return nil, codemode.ErrToolUnavailable
	}
	member, ok := findMetaMember(members, slug)
	if !ok {
		return nil, codemode.ErrToolUnavailable
	}
	catalog, err := b.service.describeMetaMember(context.WithValue(ctx, codeMemberSessionsKey{}, b.sessions), b.service.logger, gate, member)
	if err != nil {
		return nil, codeHelperFailure(err)
	}
	entry, ok := catalog.byName[name]
	if !ok {
		return nil, codemode.ErrToolUnavailable
	}
	definition, err := json.Marshal(entry)
	if err != nil {
		return nil, codemode.ErrToolUnavailable
	}
	return &codemode.Tool{Path: path, Name: entry.Name, Description: entry.Description, Definition: definition, Fingerprint: codeToolFingerprint(member, definition)}, nil
}

func (b *metaCodeBackend) Call(ctx context.Context, path string, arguments json.RawMessage) (json.RawMessage, error) {
	// Each member call gets its own trace, linked to the execution's, so
	// Tool Logs shows one correctly attributed row per call, as it does for
	// Progressive execute_tool, instead of folding an execution's calls to
	// different members into one row.
	ctx, span := b.service.tracer.Start(ctx, "gateway.code.tool_call", trace.WithNewRoot(), trace.WithLinks(trace.LinkFromContext(ctx)), trace.WithAttributes(attr.ToolName(path)))
	defer span.End()
	result, err := b.call(ctx, path, arguments)
	if err != nil {
		span.SetStatus(codes.Error, "code mode tool call failed")
		return nil, err
	}
	span.SetStatus(codes.Ok, "")
	return result, nil
}

func (b *metaCodeBackend) call(ctx context.Context, path string, arguments json.RawMessage) (json.RawMessage, error) {
	ctx, gate, members, err := b.current(ctx, true)
	if err != nil {
		return nil, err
	}
	callGate := *gate
	if slug, _, splitErr := metamcp.SplitQualifiedName(path); splitErr == nil {
		if member, ok := findMetaMember(members, slug); ok {
			if _, consent := errors.AsType[catalogConsentError](b.service.validateCatalogMember(ctx, &callGate, member)); consent {
				return nil, codemode.ErrConsentRequired
			}
		}
	}
	req := &rawRequest{JSONRPC: "2.0", ID: mcpjsonrpc.StringID(uuid.NewString()), Method: "tools/call", Params: nil}
	invocation := &metaCodeInvocation{dispatched: false, rejected: false, message: ""}
	ctx = context.WithValue(ctx, metaCodeInvocationKey{}, invocation)
	ctx = context.WithValue(ctx, codeMemberSessionsKey{}, b.sessions)
	raw, err := b.service.executeMetaMemberTool(ctx, b.service.logger, &callGate, members, req, path, arguments, b.meta)
	if err != nil {
		failure := invocation.failure()
		if refusal, ok := errors.AsType[*codemode.DispatchError](err); ok {
			failure = refusal
		}
		if invocation.message != "" {
			level := slog.LevelInfo
			if refusal, ok := errors.AsType[*codemode.DispatchError](failure); ok && !refusal.BeforeInvoke && refusal.Rejection == "" {
				level = slog.LevelWarn
			}
			b.service.logger.Log(ctx, level, "code mode tool call failed", attr.SlogToolName(path), attr.SlogReason(invocation.message))
		}
		return nil, failure
	}
	// The member refused the arguments before running the tool.
	if invocation.rejected {
		return nil, invocation.failure()
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	// The member completed: a reply without a usable result is a completed
	// call whose result cannot be exposed, which the host reports as such.
	_ = json.Unmarshal(raw, &envelope)
	return envelope.Result, nil
}

type metaCodeInvocationKey struct{}

// One invocation owns this state; it is never shared across parallel callbacks.
type metaCodeInvocation struct {
	dispatched bool
	// rejected marks a call refused unexecuted for a reason the program can
	// fix: the member's own unknown-tool or invalid-params error, a hosted
	// lookup refusal before dispatch, or hosted input-schema validation.
	rejected bool
	// message is the member-scoped failure the program would otherwise lose.
	message string
}

func (i *metaCodeInvocation) failure() error {
	switch {
	case i.rejected:
		return &codemode.DispatchError{BeforeInvoke: false, Code: "tool_call_rejected", Rejection: i.message}
	case !i.dispatched:
		return &codemode.DispatchError{BeforeInvoke: true, Code: "tool_unavailable", Rejection: ""}
	default:
		return &codemode.DispatchError{BeforeInvoke: false, Code: "tool_call_outcome_unknown", Rejection: ""}
	}
}

// codeToolFingerprint binds a member tool's complete definition to its member
// and dispatch destination, so search cursors detect a changed catalog.
func codeToolFingerprint(member metaMember, definition json.RawMessage) string {
	sum := sha256.Sum256(append(append([]byte(codeMemberDestination(member)), 0), definition...))
	return hex.EncodeToString(sum[:])
}

// codeMemberDestination identifies where a member's calls are routed.
func codeMemberDestination(member metaMember) string {
	destination, _ := json.Marshal([]any{
		member.serverID,
		member.projectID,
		int(member.backend),
		member.toolsetID,
		member.remoteServerID,
		member.tunneledServerID,
		member.tunneledResourceIdentifier,
		member.environmentID,
		member.toolVariationsGroupID,
	})
	return string(destination)
}
