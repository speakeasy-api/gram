package chat

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SessionAccess holds the per-chat gate so services that reveal a session's
// content under another name, such as a risk finding's matched value, answer
// the ownership question exactly as the chat service does.
type SessionAccess struct {
	logger *slog.Logger
	repo   *repo.Queries
	authz  *authz.Engine
}

func NewSessionAccess(logger *slog.Logger, db repo.DBTX, authzEngine *authz.Engine) *SessionAccess {
	return &SessionAccess{
		logger: logger,
		repo:   repo.New(db),
		authz:  authzEngine,
	}
}

// AnswersForSession reports whether the caller may read the session with no
// chat:read grant: they own it, or own or edit the agent behind an agent-only
// session. It records no denial and, like the chat gate, admits the runtime.
func (a *SessionAccess) AnswersForSession(ctx context.Context, chatID uuid.UUID) (bool, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return false, nil
	}
	// Impersonation bypasses RBAC, so the owner rule answers nothing for a
	// support or legacy-impersonated session, as loadAuthorizedChat refuses.
	if authz.CheckSensitiveContentAccess(ctx) != nil {
		return false, nil
	}

	chat, err := a.repo.GetChat(ctx, repo.GetChatParams{ID: chatID, ProjectID: *authCtx.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, oops.E(oops.CodeUnexpected, err, "failed to load chat").LogError(ctx, a.logger)
	}

	noGrant := false
	err = a.authorizeChatAccess(ctx, authCtx, chat, chatAccessRead, &noGrant)
	var shared *oops.ShareableError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &shared) && shared.Code == oops.CodeUnexpected:
		return false, err
	default:
		return false, nil
	}
}

// chatAccess selects which scope a non-owner needs to reach a chat. Reads and
// destructive mutations are deliberately separate scopes: a session reviewer
// holding chat:read can open every transcript in the project and pin one as a
// shared bookmark, but deleting or otherwise mutating someone else's session
// is destructive and needs chat:write. Owners bypass both.
type chatAccess int

const (
	chatAccessRead chatAccess = iota + 1
	chatAccessWrite
)

func (a chatAccess) check(chatID string) authz.Check {
	if a == chatAccessWrite {
		return authz.ChatWriteCheck(chatID)
	}
	return authz.ChatReadCheck(chatID)
}

// authorizeChatRead decides whether the caller may read a chat's content.
//
// It is shared by every path that hands a transcript to a caller — LoadChat
// and the turn stream — because they expose the same data and so must answer
// this the same way. The turn stream originally checked only that the chat
// belonged to the caller's project, which let an embedded chat-session token
// watch any other user's chat in that project.
func (s *Service) authorizeChatRead(ctx context.Context, authCtx *contextvalues.AuthContext, chat repo.GetChatRow) error {
	return s.access.authorizeChatAccess(ctx, authCtx, chat, chatAccessRead, nil)
}

// authorizeChatAccess is the shared gate behind every per-chat endpoint.
//
// Project scoping lives here as well as in the SQL so it cannot be dropped by
// one call site, and so the turn stream (which keeps its own lookup) answers
// the ownership question identically.
func (a *SessionAccess) authorizeChatAccess(ctx context.Context, authCtx *contextvalues.AuthContext, chat repo.GetChatRow, access chatAccess, filteredGrant *bool) error {
	// older chat_messages may not have project_id in the model, but it will
	// always exist on the chat
	if chat.ProjectID != *authCtx.ProjectID {
		return oops.C(oops.CodeUnauthorized)
	}

	// External-user and chat-session-token callers must match the chat owner.
	// First-party project credentials read any session in their project: the
	// dashboard session, the managed-assistant runtime, and a direct API key,
	// which the RBAC engine already treats as self-scoped (see
	// authz.Engine.ShouldEnforce).
	//
	// A chat-session token minted via an API key restores that key's APIKeyID
	// (chatsessions.Manager.Authorize), so APIKeyID alone does not prove the
	// caller authenticated *as* the key — treating it as first-party would let
	// an end user's chat-session token read every project chat. Direct legacy
	// Gram-Key auth carries scopes, while direct principal auth is
	// identified by its private mode. API-key-minted chat tokens may retain legacy
	// mode but have no scopes, so they still go through owner matching.
	_, isAssistantCall := contextvalues.GetAssistantPrincipal(ctx)
	apiKeyMode, hasAPIKeyAuthorization := contextvalues.APIKeyAuthorization(ctx)
	isDirectLegacyAPIKey := hasAPIKeyAuthorization && apiKeyMode == contextvalues.APIKeyAuthorizationModeLegacy && len(authCtx.APIKeyScopes) > 0
	isDirectPrincipalAPIKey := hasAPIKeyAuthorization && apiKeyMode == contextvalues.APIKeyAuthorizationModePrincipal
	isDirectAPIKeyCall := authCtx.APIKeyID != "" && (isDirectLegacyAPIKey || isDirectPrincipalAPIKey)
	isAPIKeyChatSession := authCtx.APIKeyID != "" && !isDirectAPIKeyCall
	surface, _ := contextvalues.GetActingSurface(ctx)
	actor, authenticated := contextvalues.AuthenticatedActor(ctx)
	isPlatformMember := surface == contextvalues.ActingSurfacePlatformMCP && authenticated && authCtx.UserID != "" && actor.Type == urn.PrincipalTypeUser && actor.ID == authCtx.UserID
	// OAuth-authenticated Platform members use the owner/grant gate below, just
	// like dashboard members, rather than an embedded external-user token gate.
	if authCtx.SessionID == nil && !isDirectAPIKeyCall && !isPlatformMember {
		if !isAssistantCall {
			if chat.ExternalUserID.String != "" && chat.ExternalUserID.String != authCtx.ExternalUserID {
				return oops.C(oops.CodeUnauthorized)
			}
			if isAPIKeyChatSession {
				externalOwnerMatches := chat.ExternalUserID.Valid && chat.ExternalUserID.String != "" && chat.ExternalUserID.String == authCtx.ExternalUserID
				userOwnerMatches := chat.UserID.Valid && chat.UserID.String != "" && chat.UserID.String == authCtx.UserID
				if !externalOwnerMatches && !userOwnerMatches {
					return oops.C(oops.CodeUnauthorized)
				}
			}
		}
	}

	// Gate dashboard access on chat:read or chat:write. The check is a no-op
	// unless RBAC is enforced for the request (see authz.Engine.ShouldEnforce —
	// API-key and sessionless callers are exempt). Members can always reach
	// sessions they own, so bypass the scope check for the owner; reaching
	// anyone else's session requires an explicit grant, which no system role
	// holds. The managed-assistant runtime is exempt — it consumes transcripts
	// programmatically, not as a reviewer.
	isOwner := authCtx.UserID != "" && chat.UserID.Valid && chat.UserID.String == authCtx.UserID
	// An agent's session has no human owner, so the agent's owner and its
	// editors (agent:write) stand in for one, for reading only. A session that
	// also carries a human identity (user_id or external_user_id) stays that
	// human's, whatever agent_id says. Deleting or otherwise mutating what the
	// agent did still needs chat:write.
	agentOnlySession := chat.AgentID.Valid && !chat.UserID.Valid && (!chat.ExternalUserID.Valid || chat.ExternalUserID.String == "")
	if !isAssistantCall && !isOwner && access == chatAccessRead && authCtx.UserID != "" && agentOnlySession {
		answers, err := a.answersForAgent(ctx, authCtx, chat)
		if err != nil {
			return err
		}
		isOwner = answers
	}
	if !isAssistantCall && !isOwner {
		if filteredGrant != nil {
			if !*filteredGrant {
				return oops.C(oops.CodeForbidden)
			}
			return nil
		}
		if err := a.authz.Require(ctx, access.check(chat.ID.String())); err != nil {
			return err
		}
	}

	return nil
}

// answersForAgent reports whether the caller owns the agent behind an
// agent-only session or holds agent:write on it. The write check is evaluated
// only when RBAC is enforced: a disabled check allows everything and must not
// turn every agent's sessions into the caller's.
func (a *SessionAccess) answersForAgent(ctx context.Context, authCtx *contextvalues.AuthContext, chat repo.GetChatRow) (bool, error) {
	owns, err := a.repo.IsAgentOwner(ctx, repo.IsAgentOwnerParams{
		AgentID:        chat.AgentID.UUID,
		OrganizationID: chat.OrganizationID,
		OwnerUserID:    authCtx.UserID,
	})
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "resolve agent owner").LogError(ctx, a.logger)
	}
	if owns {
		return true, nil
	}

	enforce, err := a.authz.ShouldEnforce(ctx)
	if err != nil {
		a.logger.WarnContext(ctx, "could not determine RBAC enforcement for agent session access", attr.SlogError(err))
		return false, nil
	}
	if !enforce {
		return false, nil
	}
	// A grant on a deleted or foreign agent id answers for nothing, as the
	// listing's candidate agents exclude them.
	live, err := a.repo.IsLiveAgent(ctx, repo.IsLiveAgentParams{
		AgentID:        chat.AgentID.UUID,
		OrganizationID: chat.OrganizationID,
	})
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "resolve agent").LogError(ctx, a.logger)
	}
	if !live {
		return false, nil
	}
	// Evaluate, not Require: falling through to the chat:read gate below is
	// the expected outcome for a non-editor, not a denial worth recording.
	canEdit, err := a.authz.Evaluate(ctx, agentWriteCheck(chat.AgentID.UUID))
	if err != nil {
		a.logger.WarnContext(ctx, "agent:write check failed for agent session access", attr.SlogError(err))
		return false, nil
	}
	return canEdit, nil
}
