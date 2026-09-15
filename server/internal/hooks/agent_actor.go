package hooks

import (
	"context"
	"errors"
	"fmt"
	"slices"

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

// mcpListOwner binds a session's MCP-list snapshot to the scope that wrote it;
// the snapshot key is the client-reported session id alone.
type mcpListOwner struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Actor     string `json:"actor"`
}

func mcpListOwnerCacheKey(sessionID string) string {
	return "session:mcp-list-owner:" + sessionID
}

func mcpListOwnerFromContext(ctx context.Context) mcpListOwner {
	owner := mcpListOwner{OrgID: "", ProjectID: "", Actor: ""}
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ProjectID != nil {
		owner.OrgID = authCtx.ActiveOrganizationID
		owner.ProjectID = authCtx.ProjectID.String()
	}
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok {
		owner.Actor = actor.String()
	}
	return owner
}

func (o mcpListOwner) isAgent() bool {
	principal, err := urn.ParsePrincipal(o.Actor)
	return err == nil && principal.Type == urn.PrincipalTypeAgent
}

// shares reports whether o and other may use one session's snapshot: known
// scopes must agree, and an agent-owned snapshot belongs to that agent alone.
func (o mcpListOwner) shares(other mcpListOwner) bool {
	if o.ProjectID != "" && other.ProjectID != "" && (o.OrgID != other.OrgID || o.ProjectID != other.ProjectID) {
		return false
	}
	if o.isAgent() || other.isAgent() {
		return o.Actor == other.Actor
	}
	return true
}

// claimMCPListSnapshot records ctx as the session snapshot's owner and reports
// whether ctx may write it. An agent may not take over an unowned snapshot.
func (s *Service) claimMCPListSnapshot(ctx context.Context, sessionID string) bool {
	writer := mcpListOwnerFromContext(ctx)
	var existing mcpListOwner
	err := s.cache.Get(ctx, mcpListOwnerCacheKey(sessionID), &existing)
	switch {
	case err == nil && !existing.shares(writer):
		s.logger.WarnContext(ctx, "refusing MCP list snapshot write from a different owner",
			attr.SlogEvent("mcp_list_snapshot_owner_mismatch"),
			attr.SlogGenAIConversationID(sessionID),
		)
		return false
	case err == nil:
		if writer.ProjectID == "" {
			writer.OrgID, writer.ProjectID = existing.OrgID, existing.ProjectID
		}
		if writer.Actor == "" {
			writer.Actor = existing.Actor
		}
	case writer.isAgent():
		var entries []MCPServerEntry
		if s.cache.Get(ctx, sessionMCPListCacheKey(sessionID), &entries) == nil {
			s.logger.WarnContext(ctx, "refusing agent takeover of an unowned MCP list snapshot",
				attr.SlogEvent("mcp_list_snapshot_owner_mismatch"),
				attr.SlogGenAIConversationID(sessionID),
			)
			return false
		}
	}
	if err := s.cache.Set(ctx, mcpListOwnerCacheKey(sessionID), writer, sessionMCPListTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to record MCP list snapshot owner",
			attr.SlogError(err),
			attr.SlogGenAIConversationID(sessionID),
		)
	}
	return true
}

// strippedTeeKey reports whether a raw OTLP attribute must be dropped from the
// event-feed copy: spoofed actor keys always, identity keys for agents.
func strippedTeeKey(key string, agent bool) bool {
	if key == string(attr.AuthorizationActorTypeKey) || key == string(attr.AuthorizationActorIDKey) {
		return true
	}
	return agent && slices.Contains(selfReportedIdentityKeys, attr.Key(key))
}

// sanitizeTeedLogsPayload applies the stored-row attribution rules to a raw
// OTLP export before it is teed, so the event-feed copy matches the rows.
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
			resourceLog.Resource.Attributes = slices.DeleteFunc(resourceLog.Resource.Attributes, func(a *gen.OTELResourceAttribute) bool {
				return a != nil && strippedTeeKey(a.Key, agent)
			})
		}
		for _, scopeLog := range resourceLog.ScopeLogs {
			if scopeLog == nil {
				continue
			}
			for _, record := range scopeLog.LogRecords {
				if record == nil {
					continue
				}
				record.Attributes = slices.DeleteFunc(record.Attributes, func(a *gen.OTELAttribute) bool {
					return a != nil && strippedTeeKey(a.Key, agent)
				})
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
