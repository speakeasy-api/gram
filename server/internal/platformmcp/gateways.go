//nolint:exhaustruct // Refusals, results and repository literals only set fields applicable to the outcome.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

const (
	gatewaysFeature = "gateways"

	// maxGatewayNameLength is the limit the gateway name column enforces.
	maxGatewayNameLength = 100
	// maxGatewayMembers bounds one request's member list.
	maxGatewayMembers = 50
	// maxPlatformEndpointSlugLength is the platform address limit, including
	// the organization prefix, that the dashboard holds a slug to.
	maxPlatformEndpointSlugLength = 40

	gatewayOutcomeCreated   = "created"
	gatewayOutcomeUpdated   = "updated"
	gatewayOutcomeUnchanged = "unchanged"
)

var (
	ErrGatewayInvalid  = errors.New("invalid platform mcp gateway request")
	ErrGatewayMissing  = errors.New("platform mcp gateway target not found")
	ErrGatewayRefused  = errors.New("platform mcp gateway member refused")
	ErrGatewayConflict = errors.New("platform mcp gateway conflict")
)

// GatewayError is safe to map into a tool refusal: Message is written for the
// administrator and never carries database detail.
type GatewayError struct {
	Code    string
	Message string
	// Members names every requested server that cannot join, so the caller
	// can drop or fix them all in one retry.
	Members []GatewayMemberRefusal
	Cause   error
}

func (e *GatewayError) Error() string { return e.Message }
func (e *GatewayError) Unwrap() error { return e.Cause }

// GatewayMemberRefusal is one server that cannot be a member of the gateway.
type GatewayMemberRefusal struct {
	MCPID string `json:"mcp_id"`
	// Reason is one of not_in_project, no_address, unproxied,
	// hosted_toolset_server, duplicate_backend, shadow_review_required, or
	// approval_check_unavailable.
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type CreateGatewayInput struct {
	ProjectID string   `json:"project_id" jsonschema:"ID of the project the gateway belongs to; every member must be in it"`
	Name      string   `json:"name" jsonschema:"display name for the gateway, at most 100 characters"`
	MCPIDs    []string `json:"mcp_ids" jsonschema:"exact IDs of the MCP servers to put behind the gateway, from find_mcp in this project, in the order the gateway lists them; at most 50"`
}

type SetGatewayMembersInput struct {
	ProjectID string   `json:"project_id" jsonschema:"ID of the project the gateway belongs to"`
	GatewayID string   `json:"gateway_id" jsonschema:"exact ID of the gateway to change"`
	MCPIDs    []string `json:"mcp_ids" jsonschema:"the complete list of MCP servers the gateway should front afterwards, in order; servers left out are removed and an empty list removes them all; at most 50"`
}

// GatewayOutput reports one committed gateway write. A refusal is returned as
// an error result and never as this struct.
type GatewayOutput struct {
	// Outcome is "created", "updated", or "unchanged" when the gateway already
	// had exactly the requested members in that order.
	Outcome string `json:"outcome"`
	// Gateway is a fresh read taken after the commit when SnapshotScope is
	// "fresh_read_after_commit".
	Gateway       *Gateway `json:"gateway,omitempty"`
	SnapshotScope string   `json:"snapshot_scope"`
	// GatewayID is set even when the fresh read failed.
	GatewayID string   `json:"gateway_id"`
	Added     []string `json:"added_mcp_ids,omitempty"`
	Removed   []string `json:"removed_mcp_ids,omitempty"`
}

// Gateway is one gateway as stored.
type Gateway struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
	// NetworkAccess is public_only, dual, or private_only.
	NetworkAccess string `json:"network_access"`
	// AddressSlugs are the gateway's platform addresses; it is served at
	// /mcp/<slug> on the organization's MCP host. Custom-domain addresses are
	// listed by get_mcp_connection_settings.
	AddressSlugs []string        `json:"address_slugs"`
	Members      []GatewayMember `json:"members"`
}

// GatewayMember is one server behind a gateway. Its tools are served as
// <slug>--<tool name>.
type GatewayMember struct {
	MCPID string `json:"mcp_id"`
	Name  string `json:"name,omitempty"`
	Slug  string `json:"slug,omitempty"`
}

// GatewayService creates gateways and sets their members through the same
// core the gateway management API uses, so an agent can make exactly the
// gateways the dashboard can.
type GatewayService struct {
	db      *pgxpool.Pool
	queries *platformrepo.Queries
	logger  *slog.Logger
	// engine checks the mcp:write grants the management API checks.
	engine *authz.Engine
	// admin re-checks org:admin live, like every other Platform MCP write
	// that changes what servers people can reach.
	admin       Authorizer
	writer      metamcp.GatewayWriter
	endpoints   MCPConnectionEndpointWriter
	publication plugins.PublicationRequests
	publisher   plugins.PluginPublishSignaler
	changes     OperationBudget
}

func NewGatewayService(logger *slog.Logger, db *pgxpool.Pool, auditLogger *audit.Logger, engine *authz.Engine, admin Authorizer, guard *admission.Guard, endpoints MCPConnectionEndpointWriter, publication plugins.PublicationRequests, publisher plugins.PluginPublishSignaler, changes OperationBudget) (*GatewayService, error) {
	if logger == nil || db == nil || auditLogger == nil || engine == nil || admin == nil || guard == nil || endpoints == nil || !changes.valid() {
		return nil, ErrGatewayInvalid
	}
	return &GatewayService{
		db: db, queries: platformrepo.New(db), logger: logger, engine: engine, admin: admin,
		writer:    metamcp.GatewayWriter{Logger: logger, Audit: auditLogger, Authz: engine, Admission: guard},
		endpoints: endpoints, publication: publication, publisher: publisher, changes: changes,
	}, nil
}

func (s *GatewayService) valid() bool {
	return s != nil && s.db != nil && s.queries != nil && s.logger != nil && s.engine != nil && s.admin != nil &&
		s.writer.Audit != nil && s.writer.Admission != nil && s.endpoints != nil && s.changes.valid()
}

// CreateGateway creates a private gateway with a default platform address and
// the requested members, all in one transaction. If any requested server
// cannot join, nothing is created and every such server is named.
func (s *GatewayService) CreateGateway(ctx context.Context, principal Principal, input CreateGatewayInput) (GatewayOutput, error) {
	output, err := s.createGateway(ctx, principal, input)
	return output, s.sanitize(ctx, err)
}

func (s *GatewayService) createGateway(ctx context.Context, principal Principal, input CreateGatewayInput) (GatewayOutput, error) {
	if !s.valid() {
		return GatewayOutput{}, gatewayUnavailable(errors.New("gateway service is not composed"))
	}
	projectID, err := parseGatewayProject(input.ProjectID)
	if err != nil {
		return GatewayOutput{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > maxGatewayNameLength {
		return GatewayOutput{}, gatewayInvalid(fmt.Sprintf("Provide a gateway name of 1 to %d characters.", maxGatewayNameLength))
	}
	mcpIDs, err := parseGatewayMembers(input.MCPIDs)
	if err != nil {
		return GatewayOutput{}, err
	}
	// The gateway does not exist yet, so mcp:write is checked on the project,
	// as the management API's create does.
	project, err := s.resolveProject(ctx, principal, projectID, projectID)
	if err != nil {
		return GatewayOutput{}, err
	}
	organizationSlug := projectOrganizationSlug(ctx, s.db, principal.OrganizationID)
	if organizationSlug == "" {
		return GatewayOutput{}, gatewayUnavailable(errors.New("organization slug unavailable"))
	}
	if err := s.charge(ctx, principal); err != nil {
		return GatewayOutput{}, err
	}

	actor := gatewayActor(principal, project.ID)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return GatewayOutput{}, fmt.Errorf("begin gateway creation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	// Private and public-only, exactly as the dashboard creates a gateway;
	// visibility and network access are changed afterwards with their own
	// tools. With no issuer named, the core mints the gateway its own.
	created, err := s.writer.CreateGateway(ctx, tx, actor, metamcp.CreateGatewayInput{
		Name: name, UserSessionIssuerID: uuid.NullUUID{}, Visibility: string(metamcp.VisibilityPrivate), NetworkAccessMode: networkaccess.ModePublicOnly,
	})
	if err != nil {
		return GatewayOutput{}, fmt.Errorf("create gateway: %w", err)
	}
	// The address comes before the members: endpoint writers take the
	// network-ingress lock ahead of the project's admission lock, and adding
	// members takes the admission lock.
	if err := s.createDefaultAddress(ctx, tx, principal, project.ID, organizationSlug, created); err != nil {
		return GatewayOutput{}, err
	}

	meta, err := s.writer.LockGateway(ctx, tx, actor, created.ID)
	if err != nil {
		return GatewayOutput{}, fmt.Errorf("lock new gateway: %w", err)
	}
	var refusals []GatewayMemberRefusal
	for index, mcpID := range mcpIDs {
		_, err := s.writer.AddMember(ctx, tx, actor, meta, mcpID, int32(index))
		if refusal, ok := gatewayMemberRefusal(mcpID, err); ok {
			refusals = append(refusals, refusal)
			continue
		}
		if err != nil {
			return GatewayOutput{}, fmt.Errorf("add gateway member: %w", err)
		}
	}
	if len(refusals) > 0 {
		return GatewayOutput{}, gatewayMembersRefused(refusals, "Nothing was created.")
	}
	if len(mcpIDs) > 0 {
		if _, err := s.publication.ProjectWithOutcome(ctx, tx, principal.OrganizationID, project.ID, principal.UserID); err != nil {
			return GatewayOutput{}, fmt.Errorf("request gateway publication: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return GatewayOutput{}, fmt.Errorf("commit gateway creation: %w", err)
	}
	if len(mcpIDs) > 0 {
		metamcp.AfterMembershipCommit(ctx, s.logger, s.db, s.publisher, actor, meta)
	}

	return s.finish(ctx, project.ID, principal, created.ID, gatewayOutcomeCreated, uuidStrings(mcpIDs), nil), nil
}

// SetGatewayMembers replaces a gateway's members with exactly the requested
// list, in order, in one transaction. If any newly requested server cannot
// join, nothing changes and every such server is named.
func (s *GatewayService) SetGatewayMembers(ctx context.Context, principal Principal, input SetGatewayMembersInput) (GatewayOutput, error) {
	output, err := s.setGatewayMembers(ctx, principal, input)
	return output, s.sanitize(ctx, err)
}

func (s *GatewayService) setGatewayMembers(ctx context.Context, principal Principal, input SetGatewayMembersInput) (GatewayOutput, error) {
	if !s.valid() {
		return GatewayOutput{}, gatewayUnavailable(errors.New("gateway service is not composed"))
	}
	projectID, err := parseGatewayProject(input.ProjectID)
	if err != nil {
		return GatewayOutput{}, err
	}
	gatewayID, err := uuid.Parse(strings.TrimSpace(input.GatewayID))
	if err != nil {
		return GatewayOutput{}, gatewayInvalid("Provide the exact gateway ID.")
	}
	mcpIDs, err := parseGatewayMembers(input.MCPIDs)
	if err != nil {
		return GatewayOutput{}, err
	}
	project, err := s.resolveProject(ctx, principal, projectID, gatewayID)
	if err != nil {
		return GatewayOutput{}, err
	}
	if err := s.charge(ctx, principal); err != nil {
		return GatewayOutput{}, err
	}

	actor := gatewayActor(principal, project.ID)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return GatewayOutput{}, fmt.Errorf("begin gateway member change: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	meta, err := s.writer.LockGateway(ctx, tx, actor, gatewayID)
	if errors.Is(err, metamcp.ErrGatewayNotFound) {
		return GatewayOutput{}, gatewayMissing()
	}
	if err != nil {
		return GatewayOutput{}, fmt.Errorf("lock gateway: %w", err)
	}
	current, err := metamcprepo.New(tx).ListMetaMCPMembers(ctx, metamcprepo.ListMetaMCPMembersParams{MetaMcpServerID: meta.ID, ProjectID: project.ID})
	if err != nil {
		return GatewayOutput{}, fmt.Errorf("list gateway members: %w", err)
	}
	position := make(map[uuid.UUID]int, len(mcpIDs))
	for index, mcpID := range mcpIDs {
		position[mcpID] = index
	}
	existing := make(map[uuid.UUID]metamcprepo.ListMetaMCPMembersRow, len(current))
	for _, member := range current {
		existing[member.McpServerID] = member
	}

	changed := false
	var added, removed []string
	// Removals first, so a server can be swapped for another that fronts
	// the same backend in one request.
	for _, member := range current {
		if _, keep := position[member.McpServerID]; keep {
			continue
		}
		if _, err := s.writer.RemoveMember(ctx, tx, actor, meta, member.ID); err != nil {
			return GatewayOutput{}, fmt.Errorf("remove gateway member: %w", err)
		}
		changed = true
		removed = append(removed, member.McpServerID.String())
	}
	var refusals []GatewayMemberRefusal
	for index, mcpID := range mcpIDs {
		sortOrder := int32(index)
		if member, ok := existing[mcpID]; ok {
			if member.SortOrder == sortOrder {
				continue
			}
			if _, _, err := s.writer.SetMemberSortOrder(ctx, tx, actor, meta, member.ID, sortOrder); err != nil {
				return GatewayOutput{}, fmt.Errorf("reorder gateway member: %w", err)
			}
			changed = true
			continue
		}
		_, err := s.writer.AddMember(ctx, tx, actor, meta, mcpID, sortOrder)
		if refusal, ok := gatewayMemberRefusal(mcpID, err); ok {
			refusals = append(refusals, refusal)
			continue
		}
		if err != nil {
			return GatewayOutput{}, fmt.Errorf("add gateway member: %w", err)
		}
		changed = true
		added = append(added, mcpID.String())
	}
	if len(refusals) > 0 {
		return GatewayOutput{}, gatewayMembersRefused(refusals, "Nothing was changed.")
	}
	if !changed {
		return s.finish(ctx, project.ID, principal, meta.ID, gatewayOutcomeUnchanged, nil, nil), nil
	}
	if _, err := s.publication.ProjectWithOutcome(ctx, tx, principal.OrganizationID, project.ID, principal.UserID); err != nil {
		return GatewayOutput{}, fmt.Errorf("request gateway publication: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return GatewayOutput{}, fmt.Errorf("commit gateway member change: %w", err)
	}
	metamcp.AfterMembershipCommit(ctx, s.logger, s.db, s.publisher, actor, meta)

	return s.finish(ctx, project.ID, principal, meta.ID, gatewayOutcomeUpdated, added, removed), nil
}

// resolveProject mirrors the management API's checks: live org:admin, then
// mcp:write on the target, and only then the project lookup, so a caller
// without the grant cannot tell a real project from an invented one.
func (s *GatewayService) resolveProject(ctx context.Context, principal Principal, projectID, resourceID uuid.UUID) (ResolvedProject, error) {
	if principal.OrganizationID == "" || principal.UserID == "" {
		return ResolvedProject{}, gatewayInvalid("The gateway request is missing its caller identity.")
	}
	if err := s.admin.RequireLiveOrgAdmin(ctx, principal); err != nil {
		return ResolvedProject{}, toolExposureAdminAuthorizationError(err)
	}
	if err := s.engine.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, resourceID.String(), projectID.String())); err != nil {
		mapped := toolExposureAuthorizationError(err, authz.ScopeMCPWrite)
		if _, ok := errors.AsType[*ExternalAuthorizationError](mapped); !ok {
			return ResolvedProject{}, gatewayUnavailable(err)
		}
		return ResolvedProject{}, mapped
	}
	project, err := s.queries.ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedProject{}, &GatewayError{Code: "not_found", Message: "That project is not one a gateway can be changed in from here. List the projects again and choose one exactly.", Cause: ErrGatewayMissing}
	}
	if err != nil {
		return ResolvedProject{}, fmt.Errorf("resolve gateway project: %w", err)
	}
	return ResolvedProject{ID: project.ID, Name: project.Name, Slug: project.Slug}, nil
}

func (s *GatewayService) charge(ctx context.Context, principal Principal) error {
	if err := s.changes.AllowConnectionOrOrganization(ctx, principal); err != nil {
		if errors.Is(err, ErrOperationRateLimited) {
			return &GatewayError{Code: "rate_limited", Message: "Gateways were changed too often just now. Try again shortly.", Cause: err}
		}
		return gatewayUnavailable(err)
	}
	return nil
}

// createDefaultAddress gives a new gateway the address the dashboard gives
// it: the organization slug and the gateway's name, or the gateway's own id
// when the name yields nothing usable or that address is taken.
func (s *GatewayService) createDefaultAddress(ctx context.Context, tx pgx.Tx, principal Principal, projectID uuid.UUID, organizationSlug string, gateway metamcprepo.MetaMcpServer) error {
	prefix := organizationSlug + "-"
	slug := ""
	if derived := gatewayAddressSlug(gateway.Name, len(prefix)); derived != "" {
		available, err := mcpendpoints.CheckSlugAvailable(ctx, tx, mcpendpoints.SlugAvailabilityCheck{Slug: prefix + derived, OrganizationID: principal.OrganizationID})
		if err != nil {
			return fmt.Errorf("check gateway address: %w", err)
		}
		if available {
			slug = prefix + derived
		}
	}
	if slug == "" {
		id := strings.ReplaceAll(gateway.ID.String(), "-", "")
		slug = prefix + id[len(id)-8:]
	}
	authCtx := &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: &projectID, OrganizationSlug: organizationSlug}
	if _, err := s.endpoints.CreateMcpEndpointInTransaction(ctx, tx, mcpendpoints.CreateMcpEndpointInTransactionInput{
		AuthContext: authCtx, MetaMcpServerID: uuid.NullUUID{UUID: gateway.ID, Valid: true}, Slug: slug,
	}); err != nil {
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeConflict {
			return &GatewayError{Code: "conflict", Message: "The gateway's address was taken at the same moment, so nothing was created. Try again.", Cause: errors.Join(ErrGatewayConflict, err)}
		}
		return fmt.Errorf("create gateway address: %w", err)
	}
	return nil
}

var nonSlugCharacters = regexp.MustCompile(`[^a-z0-9_-]+`)

// gatewayAddressSlug is the dashboard's derivation of a gateway's default
// address from its name, capped so the prefixed slug fits a platform address.
func gatewayAddressSlug(name string, prefixLength int) string {
	derived := strings.Trim(nonSlugCharacters.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if limit := max(0, maxPlatformEndpointSlugLength-prefixLength); len(derived) > limit {
		derived = derived[:limit]
	}
	return strings.TrimRight(derived, "-")
}

// finish re-reads the committed gateway, so a caller reports what is stored
// rather than what it asked for.
func (s *GatewayService) finish(ctx context.Context, projectID uuid.UUID, principal Principal, gatewayID uuid.UUID, outcome string, added, removed []string) GatewayOutput {
	output := GatewayOutput{Outcome: outcome, GatewayID: gatewayID.String(), Added: added, Removed: removed, SnapshotScope: "verification_unavailable"}
	gateway, err := s.readGateway(ctx, principal, projectID, gatewayID)
	if err != nil {
		s.logger.WarnContext(ctx, "read gateway after Platform MCP write", attr.SlogError(err))
		return output
	}
	output.Gateway = &gateway
	output.SnapshotScope = "fresh_read_after_commit"
	return output
}

func (s *GatewayService) readGateway(ctx context.Context, principal Principal, projectID, gatewayID uuid.UUID) (Gateway, error) {
	queries := metamcprepo.New(s.db)
	meta, err := queries.GetMetaMCPServer(ctx, metamcprepo.GetMetaMCPServerParams{ID: gatewayID, OrganizationID: principal.OrganizationID, ProjectID: projectID})
	if err != nil {
		return Gateway{}, fmt.Errorf("read gateway: %w", err)
	}
	members, err := queries.ListMetaMCPMembers(ctx, metamcprepo.ListMetaMCPMembersParams{MetaMcpServerID: gatewayID, ProjectID: projectID})
	if err != nil {
		return Gateway{}, fmt.Errorf("read gateway members: %w", err)
	}
	endpoints, err := mcpendpointsrepo.New(s.db).ListMCPEndpointsByMetaMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMetaMCPServerIDParams{ProjectID: projectID, MetaMcpServerID: gatewayID})
	if err != nil {
		return Gateway{}, fmt.Errorf("read gateway addresses: %w", err)
	}
	gateway := Gateway{
		ID: meta.ID.String(), Name: meta.Name, Visibility: meta.Visibility,
		NetworkAccess: conv.PtrValOr(conv.FromPGText[string](meta.NetworkAccessMode), string(networkaccess.ModePublicOnly)),
		AddressSlugs:  []string{}, Members: make([]GatewayMember, 0, len(members)),
	}
	for _, endpoint := range endpoints {
		if !endpoint.CustomDomainID.Valid {
			gateway.AddressSlugs = append(gateway.AddressSlugs, endpoint.Slug)
		}
	}
	for _, member := range members {
		gateway.Members = append(gateway.Members, GatewayMember{
			MCPID: member.McpServerID.String(),
			Name:  conv.PtrValOr(conv.FromPGText[string](member.McpServerName), ""),
			Slug:  conv.PtrValOr(conv.FromPGText[string](member.McpServerSlug), ""),
		})
	}
	return gateway, nil
}

// sanitize is the boundary every error crosses on its way to the caller: only
// a hand-written refusal or an authorization challenge passes through, and
// anything else becomes the generic unavailable refusal, logged here.
func (s *GatewayService) sanitize(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*ExternalAuthorizationError](err); ok {
		return err
	}
	refusal, ok := errors.AsType[*GatewayError](err)
	if !ok {
		refusal, _ = errors.AsType[*GatewayError](gatewayUnavailable(err))
	}
	if refusal.Code == unavailableCode && s != nil && s.logger != nil {
		s.logger.ErrorContext(ctx, "Platform MCP gateway write failed", attr.SlogError(err))
	}
	return refusal
}

// gatewayMemberReasons maps each membership refusal the shared core returns
// to the reason a caller sees.
var gatewayMemberReasons = []struct {
	err     error
	reason  string
	message string
}{
	{metamcp.ErrMemberServerNotInProject, "not_in_project", "This server is not a live MCP server in this project."},
	{metamcp.ErrMemberServerHasNoSlug, "no_address", "This server has no address yet. Saving its settings once in the dashboard gives it one."},
	{metamcp.ErrMemberServerUnproxied, "unproxied", "This server is reached directly rather than through Speakeasy, so a gateway cannot serve its tools."},
	{metamcp.ErrMemberServerIsHostedToolset, "hosted_toolset_server", "This is a hosted tool list's own server, which cannot sit behind a gateway. Create a separate server for that tool list and add that instead."},
	{metamcp.ErrMemberSharesBackend, "duplicate_backend", "Another server in this request or already on the gateway fronts the same backend."},
	{metamcp.ErrMemberAlreadyPresent, "duplicate_backend", "This server is already on the gateway."},
	{metamcp.ErrMemberNeedsShadowApproval, "shadow_review_required", "The gateway is shared with people this server has not been approved for in Shadow MCP. Review the server first."},
	{metamcp.ErrMemberAdmissionUnverifiable, "approval_check_unavailable", "Whether this server is approved for the gateway's audience could not be checked just now. Try again shortly."},
}

func gatewayMemberRefusal(mcpID uuid.UUID, err error) (GatewayMemberRefusal, bool) {
	if err == nil {
		return GatewayMemberRefusal{}, false
	}
	for _, known := range gatewayMemberReasons {
		if errors.Is(err, known.err) {
			return GatewayMemberRefusal{MCPID: mcpID.String(), Reason: known.reason, Message: known.message}, true
		}
	}
	return GatewayMemberRefusal{}, false
}

func gatewayMembersRefused(refusals []GatewayMemberRefusal, outcome string) error {
	return &GatewayError{
		Code:    "members_refused",
		Message: outcome + " Some of the requested servers cannot sit behind this gateway; each is listed with the reason. Drop or fix them and call again with the corrected list.",
		Members: refusals,
		Cause:   ErrGatewayRefused,
	}
}

func parseGatewayProject(value string) (uuid.UUID, error) {
	projectID, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, gatewayInvalid("Provide an explicit project ID.")
	}
	return projectID, nil
}

func parseGatewayMembers(values []string) ([]uuid.UUID, error) {
	if len(values) > maxGatewayMembers {
		return nil, gatewayInvalid(fmt.Sprintf("Name at most %d servers.", maxGatewayMembers))
	}
	seen := make(map[uuid.UUID]bool, len(values))
	ids := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return nil, gatewayInvalid(fmt.Sprintf("%q is not an MCP server ID. Use the exact IDs from find_mcp.", value))
		}
		if seen[id] {
			return nil, gatewayInvalid(fmt.Sprintf("The server %s is listed more than once.", id))
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

func gatewayActor(principal Principal, projectID uuid.UUID) metamcp.Actor {
	// Attributed to the user by id; the platform principal carries no email.
	return metamcp.Actor{OrganizationID: principal.OrganizationID, ProjectID: projectID, UserID: principal.UserID, Email: nil}
}

func uuidStrings(ids []uuid.UUID) []string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, id.String())
	}
	return values
}

func gatewayInvalid(message string) error {
	return &GatewayError{Code: "invalid_request", Message: message, Cause: ErrGatewayInvalid}
}

func gatewayMissing() error {
	return &GatewayError{Code: "not_found", Message: "That gateway is not one in this project. Check the gateway ID and project.", Cause: ErrGatewayMissing}
}

func gatewayUnavailable(cause error) error {
	return &GatewayError{Code: unavailableCode, Message: "Changing gateways is temporarily unavailable.", Cause: errors.Join(ErrUnavailable, cause)}
}
