// Platform MCP reaches a person's own MCP servers through their organization
// gateway, so one Speakeasy connection serves both. The gateway runtime is
// reused wholesale; what is particular here is that Platform MCP has already
// authenticated the caller, so no issuer gate runs and no consent catalog
// narrows the members: the caller reaches exactly what their live grants reach.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// platformGatewayTools are the organization gateway tools Platform MCP may
// call: Progressive discovery without connection_status, whose directions
// describe the gateway's own consent.
var platformGatewayTools = []string{metamcp.ToolListServers, metamcp.ToolDescribeServer, metamcp.ToolDescribeTools, metamcp.ToolExecuteTool, PlatformGatewaySearchTools}

// PlatformGatewaySearchTools ranks the tools of every server the caller
// reaches in one call. The gateway serves no such tool; Platform MCP has no
// Code Mode runner to search with, so the search is answered here with the
// same ranking Code Mode uses.
const PlatformGatewaySearchTools = "search_tools"

// errPlatformGatewayIdentity refuses a context that does not carry exactly
// the signed-in user Platform MCP admitted.
var errPlatformGatewayIdentity = errors.New("platform gateway call requires the admitted user's own context")

// CallPlatformGatewayTool answers one organization gateway tool for a Platform
// MCP caller and returns the tools/call result object. A refusal the gateway
// would show its own caller, such as an unknown server or a member that is not
// connected, is returned as an isError result; only failures are errors.
//
// Identity comes from ctx alone, as Platform MCP admission stamped it: the
// user acting in organizationID, with their live grants. Upstream credentials
// resolve for that user's own subject. A context carrying a delegated
// credential or a non-user actor is refused, because either would resolve a
// credential that is not the caller's own.
func (s *Service) CallPlatformGatewayTool(ctx context.Context, organizationID, userID, tool string, arguments json.RawMessage) (json.RawMessage, error) {
	if !slices.Contains(platformGatewayTools, tool) {
		return nil, fmt.Errorf("unsupported gateway tool %q", tool)
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || userID == "" || authCtx.UserID != userID || authCtx.ActiveOrganizationID != organizationID {
		return nil, errPlatformGatewayIdentity
	}
	if actor, ok := contextvalues.AuthenticatedActor(ctx); !ok || actor.Type != urn.PrincipalTypeUser || actor.ID != userID {
		return nil, errPlatformGatewayIdentity
	}
	if _, delegated := principalcredential.FromContext(ctx); delegated {
		return nil, errPlatformGatewayIdentity
	}

	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	organization, err := s.orgsRepo.GetOrganizationMetadata(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errPlatformGatewayIdentity
	}
	if err != nil {
		return nil, fmt.Errorf("load organization: %w", err)
	}
	metaServer, endpoint, unavailable, err := s.organizationGatewayRows(ctx, organization, orggateway.PlatformAvailability)
	if err != nil {
		return nil, fmt.Errorf("resolve organization gateway: %w", err)
	}
	id := mcpjsonrpc.StringID(uuid.NewString())
	if unavailable != orggateway.UnavailabilityNone {
		return platformGatewayResult(marshalToolErrorResult(ctx, logger, id, "Your organization's gateway is not available ("+string(unavailable)+"); call get_my_organization_gateway for the next step.", serverInfoMetaServer))
	}
	resolved := NewResolvedMcpEndpointFromMetaMcpServer(endpoint, metaServer, organization.ID, mcproute.OrganizationGateway)
	if err := s.RequireUserSessionIssuer(ctx, resolved); err != nil {
		return nil, fmt.Errorf("load organization gateway issuer: %w", err)
	}

	connectRoot, connectURL := "", ""
	if root, err := resolved.RootURL(requestorigin.BaseURL(ctx, s.serverURL.String())); err != nil {
		logger.WarnContext(ctx, "build gateway connect url", attr.SlogError(err))
	} else {
		connectRoot, connectURL = root, root+"/connect/first-party"
	}
	gate := &metaGateContext{
		projectID:           uuid.Nil,
		metaServerID:        metaServer.ID,
		agentID:             uuid.Nil,
		organizationGateway: true,
		organizationID:      organization.ID,
		tokens:              nil,
		projectTokens:       s.newOrganizationGatewayTokens(resolved, urn.NewUserSubject(userID)),
		// Credential choices belong to one gateway connection; Platform MCP
		// holds none, so each member routes through its project's clients.
		memberChoices:       nil,
		userSessionIssuerID: metaServer.UserSessionIssuerID.UUID,
		connectRoot:         connectRoot,
		chainUpstream:       nil,
		catalog:             nil,
		catalogRef:          nil,
		authentication:      nil,
		unapprovedMembers:   nil,
		hiddenMembers:       nil,
		execute:             false,
		discoveryMode:       metamcp.DiscoveryModeProgressive,
		authenticated:       true,
		sessionID:           "",
		chatID:              "",
		userID:              userID,
		externalUserID:      "",
		apiKeyID:            "",
		protocolVersion:     mcpversions.Resolution{Declared: "", InEffect: mcpversions.DefaultInEffect},
		connectURL:          connectURL,
	}
	params, err := json.Marshal(toolsCallParams{Name: tool, Arguments: arguments, Meta: nil})
	if err != nil {
		return nil, fmt.Errorf("encode gateway tool call: %w", err)
	}
	req := &rawRequest{JSONRPC: "2.0", ID: id, Method: mcpversions.MethodToolsCall, Params: params}
	var body json.RawMessage
	if tool == PlatformGatewaySearchTools {
		body, err = s.searchPlatformGatewayTools(ctx, logger, gate, req, arguments)
	} else {
		body, err = s.callMetaServerTool(ctx, logger, endpoint, metaServer, gate, req)
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code != oops.CodeUnexpected {
		return platformGatewayResult(marshalToolErrorResult(ctx, logger, id, shareable.Error(), serverInfoMetaServer))
	}
	return platformGatewayResult(body, err)
}

// platformGatewayResult unwraps the tools/call result from a JSON-RPC response.
func platformGatewayResult(body json.RawMessage, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode gateway tool result: %w", err)
	}
	if len(envelope.Result) == 0 {
		return nil, errors.New("gateway tool returned no result")
	}
	return envelope.Result, nil
}

// PlatformGatewaySearchResult is the structuredContent of a search_tools call.
type PlatformGatewaySearchResult struct {
	Items         []PlatformGatewaySearchItem `json:"items"`
	NextCursor    string                      `json:"next_cursor,omitempty"`
	Incomplete    bool                        `json:"incomplete"`
	FailedMembers []codemode.FailedMember     `json:"failed_members,omitempty"`
}

// PlatformGatewaySearchItem is one ranked tool, with the input schema needed
// to call it.
type PlatformGatewaySearchItem struct {
	Server      string          `json:"server"`
	Tool        string          `json:"tool"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// searchPlatformGatewayTools ranks the caller's reachable member tools. Each
// member's catalog is read live, a few at a time and each under its own
// deadline, so one slow or failing server degrades only itself.
func (s *Service) searchPlatformGatewayTools(ctx context.Context, logger *slog.Logger, gate *metaGateContext, req *rawRequest, argsRaw json.RawMessage) (json.RawMessage, error) {
	var args codemode.SearchArgs
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid search_tools arguments").LogWarn(ctx, logger)
		}
	}
	ctx, members, err := s.resolveGateMemberSnapshot(ctx, logger, gate, gate.metaServerID, uuid.Nil)
	if err != nil {
		return nil, err
	}
	if args.Server != "" {
		member, ok := findMetaMember(members, args.Server)
		if !ok {
			return nil, unknownMetaMemberError(ctx, logger, gate, args.Server)
		}
		members = []metaMember{member}
	}

	var mu sync.Mutex
	documents := make([]codemode.Document, 0)
	schemas := make(map[string]json.RawMessage)
	failed := make([]codemode.FailedMember, 0)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(platformSearchConcurrency)
	for _, member := range members {
		group.Go(func() error {
			memberCtx, cancel := context.WithTimeout(groupCtx, platformSearchMemberDeadline)
			defer cancel()
			// Each member reads its own copy: dispatch may record per-call
			// state on the gate. Upstream tokens stay shared and locked.
			memberGate := *gate
			catalog, err := s.describeMetaMember(memberCtx, logger, &memberGate, member)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, codemode.FailedMember{Server: member.slug, Reason: "catalog_unavailable"})
				return nil
			}
			if catalog.incomplete {
				failed = append(failed, codemode.FailedMember{Server: member.slug, Reason: "catalog_incomplete"})
			}
			for _, entry := range catalog.entries {
				definition, err := json.Marshal(entry)
				if err != nil {
					return fmt.Errorf("encode member tool definition: %w", err)
				}
				path := metamcp.QualifyName(member.slug, entry.Name)
				schemas[path] = entry.InputSchema
				documents = append(documents, codemode.Document{
					Candidate:   codemode.Candidate{Path: path, Description: entry.Description},
					Name:        entry.Name,
					ServerName:  member.name,
					Fingerprint: codeToolFingerprint(member, definition),
				})
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "search member tools").LogError(ctx, logger)
	}

	// Scoped to the caller: a cursor names a ranking over one person's access.
	page, err := codemode.Rank(gate.metaServerID.String()+"/"+gate.userID, args, documents, failed)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "%s", err.Error()).LogWarn(ctx, logger)
	}
	result := PlatformGatewaySearchResult{Items: make([]PlatformGatewaySearchItem, 0, len(page.Items)), NextCursor: page.NextCursor, Incomplete: page.Incomplete, FailedMembers: page.FailedMembers}
	for _, candidate := range page.Items {
		server, tool, err := metamcp.SplitQualifiedName(candidate.Path)
		if err != nil {
			continue
		}
		result.Items = append(result.Items, PlatformGatewaySearchItem{Server: server, Tool: tool, Description: candidate.Description, InputSchema: schemas[candidate.Path]})
	}
	structured, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize search_tools result").LogError(ctx, logger)
	}
	return marshalMetaToolCallResult(ctx, logger, req.ID, structured)
}

const (
	// platformSearchConcurrency bounds upstream catalog reads per search.
	platformSearchConcurrency = 4
	// platformSearchMemberDeadline bounds one member's catalog read.
	platformSearchMemberDeadline = 5 * time.Second
)
