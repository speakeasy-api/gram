package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// apiKeyCtx builds a context authenticated as a first-party project API key:
// no dashboard session and no external-user identity, scoped to ti.projectID.
func apiKeyCtx(t *testing.T, ti *chatTestInstance) context.Context {
	t.Helper()
	authCtx := &contextvalues.AuthContext{
		APIKeyID:             uuid.NewString(),
		APIKeyName:           "test-key",
		APIKeyScopes:         []string{"producer"},
		ProjectID:            &ti.projectID,
		ActiveOrganizationID: ti.orgID,
	}
	return contextvalues.SetAuthContext(t.Context(), authCtx)
}

// TestLoadChat_APIKey_ReadsAnyProjectChat proves a project API key can load a
// chat in its project even when that chat is owned by an external user. API
// keys are first-party project credentials (like the dashboard session), not
// owner-matched end users, so the external-user ownership gate does not apply.
func TestLoadChat_APIKey_ReadsAnyProjectChat(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)

	// A chat owned by a specific external user, which an API key does not match.
	seedCtx := initSessionCtx(t, ti)
	chatID := seedChat(t, seedCtx, ti, "owner-user", "external-user-123", "api key readable")
	seedNMessages(t, seedCtx, ti, chatID, 3)

	got, err := ti.service.LoadChat(apiKeyCtx(t, ti), loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, got.Messages, 3)
}

// TestLoadChat_APIKey_WrongProjectBlocked keeps the project boundary: an API key
// scoped to its own project cannot read a chat that belongs to another project.
func TestLoadChat_APIKey_WrongProjectBlocked(t *testing.T) {
	t.Parallel()
	owner := newTestChatService(t)
	other := newTestChatService(t)

	seedCtx := initSessionCtx(t, owner)
	chatID := seedChat(t, seedCtx, owner, "owner-user", "", "cross-project chat")

	// An API key scoped to a different project must not resolve the chat.
	_, err := owner.service.LoadChat(apiKeyCtx(t, other), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

// TestLoadChat_ExternalUserMismatchStillBlocked guards the ownership check for
// genuine external-user callers: they may only read their own sessions, so the
// API-key exemption must not have widened that path.
func TestLoadChat_ExternalUserMismatchStillBlocked(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)

	seedCtx := initSessionCtx(t, ti)
	chatID := seedChat(t, seedCtx, ti, "", "external-user-123", "someone elses chat")
	seedNMessages(t, seedCtx, ti, chatID, 2)

	_, err := ti.service.LoadChat(externalUserCtx(t, ti, "different-external-user"), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

// chatSessionTokenCtx mirrors what chatsessions.Manager.Authorize installs for a
// chat-session token minted via an API key: it restores the minting key's
// APIKeyID but carries NO API-key scopes, and is pinned to an external user.
func chatSessionTokenCtx(t *testing.T, ti *chatTestInstance, externalUserID string) context.Context {
	t.Helper()
	authCtx := &contextvalues.AuthContext{
		APIKeyID:             uuid.NewString(), // restored from the JWT claims
		APIKeyScopes:         nil,              // chat-session tokens never carry scopes
		ExternalUserID:       externalUserID,
		ProjectID:            &ti.projectID,
		ActiveOrganizationID: ti.orgID,
	}
	return contextvalues.SetAuthContext(t.Context(), authCtx)
}

// TestLoadChat_ChatSessionTokenStillOwnerMatched is the regression guard for the
// exemption: a chat-session token minted via an API key carries that key's
// APIKeyID, but it is an end-user credential and must NOT gain project-wide read.
// It may only load its own external user's chats.
func TestLoadChat_ChatSessionTokenStillOwnerMatched(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)

	seedCtx := initSessionCtx(t, ti)
	chatID := seedChat(t, seedCtx, ti, "", "external-user-A", "user A chat")
	seedNMessages(t, seedCtx, ti, chatID, 2)

	// A chat-session token for a different external user must be blocked, even
	// though it carries an APIKeyID.
	_, err := ti.service.LoadChat(chatSessionTokenCtx(t, ti, "external-user-B"), loadPayload(chatID.String()))
	requireOopsCode(t, err, oops.CodeUnauthorized)

	// The same token can still read its own external user's chat.
	got, err := ti.service.LoadChat(chatSessionTokenCtx(t, ti, "external-user-A"), loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, got.Messages, 2)
}
