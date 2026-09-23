package hooks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	redisCache "github.com/go-redis/cache/v9"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var errAgentHookUnscoped = errors.New("agent hook request has no project scope")

// errAgentHooksDenied marks an authenticated agent key that failed admission,
// its hooks grant, or project access; it is rejected, never downgraded.
var errAgentHooksDenied = errors.New("agent key denied hooks ingest")

// clearAgentAccountIdentity drops the AI-account identity a batch self-reports
// so agent rows never carry a human's account or device attribution.
func clearAgentAccountIdentity(meta *SessionMetadata) {
	meta.ExternalOrgID = ""
	meta.ExternalAccountUUID = ""
	meta.ExternalAccountID = ""
	meta.DeviceID = ""
	meta.UserAccountID = ""
	meta.ObservedUserEmail = ""
}

// isAgentActor reports whether an agent-principal credential authenticated the
// request. The agent is then the actor: self-reported emails and cached session
// identity never re-attribute its events to a human.
func isAgentActor(ctx context.Context) bool {
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	return ok && actor.Type == urn.PrincipalTypeAgent
}

// requireAgentHooksIngest admits an agent actor only when it holds
// org:hooks_ingest on its organization. Other callers pass unchanged.
func (s *Service) requireAgentHooksIngest(ctx context.Context) error {
	if !isAgentActor(ctx) {
		return nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgHooksIngest, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return fmt.Errorf("authorize agent hooks ingest: %w", err)
	}
	return nil
}

// agentSessionView keeps only the surface fields of a cached session, and only
// when orgID+projectID seeded it, so an agent event never inherits a human's
// identity, account attribution, or another tenant's metadata.
func agentSessionView(cached SessionMetadata, orgID, projectID string) SessionMetadata {
	view := SessionMetadata{
		SessionID:           cached.SessionID,
		ServiceName:         "",
		UserEmail:           "",
		UserID:              "",
		Provider:            "",
		ExternalOrgID:       "",
		ExternalAccountUUID: "",
		ExternalAccountID:   "",
		DeviceID:            "",
		Hostname:            "",
		Cwd:                 "",
		AccountType:         "",
		BillingMode:         "",
		UserAccountID:       "",
		ObservedUserEmail:   "",
		GramOrgID:           orgID,
		ProjectID:           projectID,
	}
	if cached.GramOrgID != orgID || cached.ProjectID != projectID {
		return view
	}
	view.ServiceName = cached.ServiceName
	view.Provider = cached.Provider
	view.Hostname = cached.Hostname
	view.Cwd = cached.Cwd
	return view
}

// selfReportedIdentityKeys are the user and AI-account identity attributes a
// client can report. telemetry_logs materializes user_id, user_email and
// external_user_id from the first three, so agent rows never persist any.
var selfReportedIdentityKeys = []attr.Key{
	attr.UserIDKey,
	attr.UserEmailKey,
	attr.ExternalUserIDKey,
	attr.Key("user.name"),
	attr.Key("user.full_name"),
	attr.Key("user.account_uuid"),
	attr.Key("user.account_id"),
	attr.Key("organization.id"),
	attr.Key("enduser.id"),
	attr.AuthUserIDKey,
	attr.AuthUserEmailKey,
	attr.AuthUserExternalIDKey,
	attr.AccountEmailKey,
	attr.ExternalOrgIDKey,
	attr.DeviceIDKey,
	attr.AccountTypeKey,
	attr.BillingModeKey,
}

// mcpListProjectID returns the project a session's MCP inventory keys are
// scoped to, or "" when none can be established. The session id is
// client-reported, so without this scope any tenant holding a hooks key could
// write the snapshot or read status that another tenant's guard reads back.
//
// An authenticated request uses its own project. An unauthenticated Claude
// hook — the optional-auth path that OTEL attributes later — uses the project
// its cached session metadata names. Before that metadata lands it has no
// scope, and callers neither read nor write the guard's keys.
func (s *Service) mcpListProjectID(ctx context.Context, sessionID string) string {
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ProjectID != nil {
		return authCtx.ProjectID.String()
	}
	if sessionID == "" || isAgentActor(ctx) {
		return ""
	}
	metadata, err := s.getSessionMetadata(ctx, sessionID)
	if err != nil {
		return ""
	}
	return metadata.ProjectID
}

// mcpListOwner binds a session's MCP-list snapshot to the actor that wrote it.
// The keys are already scoped by project; the owner keeps an agent's snapshot
// its own within that project.
type mcpListOwner struct {
	Actor string `json:"actor"`
}

func mcpListOwnerCacheKey(projectID, sessionID string) string {
	return fmt.Sprintf("session:mcp-list-owner:v2:%s:%s", projectID, sessionID)
}

func mcpListOwnerFromContext(ctx context.Context) mcpListOwner {
	owner := mcpListOwner{Actor: ""}
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok {
		owner.Actor = actor.String()
	}
	return owner
}

func (o mcpListOwner) isAgent() bool {
	principal, err := urn.ParsePrincipal(o.Actor)
	return err == nil && principal.Type == urn.PrincipalTypeAgent
}

// shares reports whether o and other may use one session's snapshot, where o
// is always the recorded owner: an agent-owned snapshot belongs to that agent
// alone, and an agent never shares a human's.
func (o mcpListOwner) shares(other mcpListOwner) bool {
	if o.isAgent() || other.isAgent() {
		return o.Actor == other.Actor
	}
	return true
}

// claimMCPListSnapshot records ctx as the owner of the session's snapshot in
// projectID and reports whether ctx may write it. Nothing is written without a
// project. An agent may not take over an unowned snapshot.
func (s *Service) claimMCPListSnapshot(ctx context.Context, projectID, sessionID string) bool {
	if projectID == "" {
		return false
	}
	writer := mcpListOwnerFromContext(ctx)
	var existing mcpListOwner
	err := s.cache.Get(ctx, mcpListOwnerCacheKey(projectID, sessionID), &existing)
	switch {
	case err != nil && !errors.Is(err, redisCache.ErrCacheMiss):
		s.logger.WarnContext(ctx, "failed to read MCP list snapshot owner; skipping write",
			attr.SlogError(err),
			attr.SlogGenAIConversationID(sessionID),
		)
		return false
	case err == nil && !existing.shares(writer):
		s.logger.WarnContext(ctx, "refusing MCP list snapshot write from a different owner",
			attr.SlogEvent("mcp_list_snapshot_owner_mismatch"),
			attr.SlogGenAIConversationID(sessionID),
		)
		return false
	case err == nil:
		if writer.Actor == "" {
			writer.Actor = existing.Actor
		}
	case writer.isAgent():
		var entries []MCPServerEntry
		if getErr := s.cache.Get(ctx, sessionMCPListCacheKey(projectID, sessionID), &entries); !errors.Is(getErr, redisCache.ErrCacheMiss) {
			s.logger.WarnContext(ctx, "refusing agent takeover of an unowned MCP list snapshot",
				attr.SlogEvent("mcp_list_snapshot_owner_mismatch"),
				attr.SlogGenAIConversationID(sessionID),
			)
			return false
		}
	}
	// A snapshot without its owner binding would reopen cross-actor reuse.
	if err := s.cache.Set(ctx, mcpListOwnerCacheKey(projectID, sessionID), writer, sessionMCPListTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to record MCP list snapshot owner; skipping write",
			attr.SlogError(err),
			attr.SlogGenAIConversationID(sessionID),
		)
		return false
	}
	return true
}

// agentSessionID namespaces a client-reported session id under the agent
// actor, so an agent can never address a human's or another agent's session.
// The agent: prefix is reserved: a non-agent's id carrying it is escaped so
// it can never equal an agent's namespaced id.
func agentSessionID(ctx context.Context, sessionID string) string {
	if sessionID == "" {
		return sessionID
	}
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type == urn.PrincipalTypeAgent {
		return actor.String() + ":" + sessionID
	}
	if strings.HasPrefix(sessionID, string(urn.PrincipalTypeAgent)+":") {
		return "user:" + sessionID
	}
	return sessionID
}

// scopeSessionAttrs applies agentSessionID to the session ids in a row's attrs.
func scopeSessionAttrs(ctx context.Context, attrs map[attr.Key]any) {
	for _, key := range otelSessionKeys {
		if value, ok := attrs[attr.Key(key)].(string); ok && value != "" {
			attrs[attr.Key(key)] = agentSessionID(ctx, value)
		}
	}
}

// scopedSessionPtr returns a fresh pointer to sessionID's agentSessionID and
// whether it changed. Callers copy their payload before storing it: payloads
// and their pointers may be shared with the caller or in-flight goroutines.
func scopedSessionPtr(ctx context.Context, sessionID *string) (*string, bool) {
	if sessionID == nil {
		return nil, false
	}
	scoped := agentSessionID(ctx, *sessionID)
	if scoped == *sessionID {
		return sessionID, false
	}
	return &scoped, true
}

// otelSessionKeys are the raw OTLP log attributes that carry a session id.
var otelSessionKeys = []string{"session.id", "conversation.id", string(attr.GenAIConversationIDKey)}

// strippedTeeKey reports whether a raw OTLP attribute must be dropped from the
// event-feed copy: spoofed actor keys always, identity keys for agents.
func strippedTeeKey(key string, agent bool) bool {
	if key == string(attr.AuthorizationActorTypeKey) || key == string(attr.AuthorizationActorIDKey) {
		return true
	}
	return agent && slices.Contains(selfReportedIdentityKeys, attr.Key(key))
}

// sanitizeTeedLogsPayload applies the stored-row attribution rules to a raw
// OTLP export before it is teed or stored, so both copies match. It also
// scopes session ids the way the hook paths do.
func sanitizeTeedLogsPayload(ctx context.Context, payload *gen.LogsPayload) {
	if payload == nil {
		return
	}
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	agent := ok && actor.Type == urn.PrincipalTypeAgent
	for _, resourceLog := range payload.ResourceLogs {
		if resourceLog == nil {
			continue
		}
		if resourceLog.Resource != nil {
			if kept := slices.DeleteFunc(resourceLog.Resource.Attributes, func(a *gen.OTELResourceAttribute) bool {
				return a != nil && strippedTeeKey(a.Key, agent)
			}); len(kept) != len(resourceLog.Resource.Attributes) {
				resourceLog.Resource.Attributes = kept
			}
		}
		for _, scopeLog := range resourceLog.ScopeLogs {
			if scopeLog == nil {
				continue
			}
			for _, record := range scopeLog.LogRecords {
				if record == nil {
					continue
				}
				if kept := slices.DeleteFunc(record.Attributes, func(a *gen.OTELAttribute) bool {
					return a != nil && strippedTeeKey(a.Key, agent)
				}); len(kept) != len(record.Attributes) {
					record.Attributes = kept
				}
				for _, a := range record.Attributes {
					if a != nil && a.Value != nil && a.Value.StringValue != nil && slices.Contains(otelSessionKeys, a.Key) {
						if scoped := agentSessionID(ctx, *a.Value.StringValue); scoped != *a.Value.StringValue {
							a.Value.StringValue = new(scoped)
						}
					}
				}
				if agent {
					record.Attributes = append(record.Attributes,
						&gen.OTELAttribute{Key: string(attr.AuthorizationActorTypeKey), Value: &gen.OTELAttributeValue{StringValue: new(string(actor.Type)), IntValue: nil, BoolValue: nil, DoubleValue: nil, ArrayValue: nil, KvlistValue: nil, BytesValue: nil}},
						&gen.OTELAttribute{Key: string(attr.AuthorizationActorIDKey), Value: &gen.OTELAttributeValue{StringValue: new(actor.ID), IntValue: nil, BoolValue: nil, DoubleValue: nil, ArrayValue: nil, KvlistValue: nil, BytesValue: nil}},
					)
				}
			}
		}
	}
}

// stripAgentIdentity removes self-reported identity from attrs when an agent
// actor authenticated the request.
func stripAgentIdentity(ctx context.Context, attrs map[attr.Key]any) {
	if !isAgentActor(ctx) {
		return
	}
	for _, key := range selfReportedIdentityKeys {
		delete(attrs, key)
	}
}

// sanitizeResourceAttrs applies the row rules to an OTLP resource's
// attributes: client-supplied actor keys are always dropped, along with an
// agent's self-reported identity. The trusted actor is stamped per record, so
// it is not repeated here.
func sanitizeResourceAttrs(ctx context.Context, attrs map[attr.Key]any) {
	delete(attrs, attr.AuthorizationActorTypeKey)
	delete(attrs, attr.AuthorizationActorIDKey)
	stripAgentIdentity(ctx, attrs)
}

// withAgentActor strips client-supplied actor attributes (and, for agents,
// self-reported identity) from a telemetry row, then stamps the trusted agent
// actor when one authenticated the request.
func withAgentActor(ctx context.Context, attrs map[attr.Key]any) map[attr.Key]any {
	delete(attrs, attr.AuthorizationActorTypeKey)
	delete(attrs, attr.AuthorizationActorIDKey)
	stripAgentIdentity(ctx, attrs)
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type == urn.PrincipalTypeAgent {
		attrs[attr.AuthorizationActorTypeKey] = string(actor.Type)
		attrs[attr.AuthorizationActorIDKey] = actor.ID
	}
	return attrs
}
