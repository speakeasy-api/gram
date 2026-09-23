package slackdirectoryconnections_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestConnectionLifecycle(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	first := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	row, err := repo.New(f.db).GetSlackDirectoryConnection(ctx, repo.GetSlackDirectoryConnectionParams{OrganizationID: f.auth.ActiveOrganizationID, ID: uuid.MustParse(first.ID)})
	require.NoError(t, err)
	require.NotContains(t, row.CredentialsEncrypted.String, "synthetic-access-token")
	plaintext, err := f.enc.Decrypt(row.CredentialsEncrypted.String)
	require.NoError(t, err)
	var bundle slackdirectoryconnections.TokenBundle
	require.NoError(t, json.Unmarshal([]byte(plaintext), &bundle))
	require.Equal(t, 1, bundle.Version)
	require.Equal(t, "synthetic-refresh-token", bundle.RefreshToken)
	reconnect := authorize(t, ctx, f, begin(t, ctx, f, &first.ID), "TEXAMPLE01")
	require.Equal(t, first.ID, reconnect.ID)
	require.NotEqual(t, first.Generation, reconnect.Generation)
	disconnected, err := f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: reconnect.ID, Generation: reconnect.Generation})
	require.NoError(t, err)
	require.Equal(t, "disconnected", disconnected.Status)
	require.NotEqual(t, reconnect.Generation, disconnected.Generation)
	row, err = repo.New(f.db).GetSlackDirectoryConnection(ctx, repo.GetSlackDirectoryConnectionParams{OrganizationID: f.auth.ActiveOrganizationID, ID: uuid.MustParse(first.ID)})
	require.NoError(t, err)
	require.False(t, row.CredentialsEncrypted.Valid)
	require.True(t, row.DisconnectedAt.Valid)
	final := authorize(t, ctx, f, begin(t, ctx, f, &first.ID), "TEXAMPLE01")
	require.Equal(t, first.ID, final.ID)
	require.NotEqual(t, disconnected.Generation, final.Generation)
	count, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionSlackDirectoryConnectionAuthorize)
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
	count, err = audittest.AuditLogCountByAction(ctx, f.db, audit.ActionSlackDirectoryConnectionDisconnect)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestMultipleWorkspaces(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	first := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	second := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE02")
	require.NotEqual(t, first.ID, second.ID)
	replacement := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	require.Equal(t, first.ID, replacement.ID)
	require.NotEqual(t, first.Generation, replacement.Generation)
	list, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, list.Connections, 2)
}

func TestReplayAndInvalidState(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	state := begin(t, ctx, f, nil)
	authorize(t, ctx, f, state, "TEXAMPLE01")
	for _, invalid := range []string{state, "invalid", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		result, err := f.service.Callback(ctx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: invalid, Code: conv.PtrEmpty("unused-code"), Error: nil})
		require.NoError(t, err)
		require.Contains(t, result.Location, "slack_result=invalid_state")
	}
	f.provider.AssertNumberOfCalls(t, "Exchange", 1)
}

func TestExpiredState(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	state := begin(t, ctx, f, nil)
	digest := sha256.Sum256([]byte(state))
	key := "slack-directory-oauth:" + hex.EncodeToString(digest[:])
	var cached map[string]any
	require.NoError(t, f.cache.Get(ctx, key, &cached))
	cached["ExpiresAt"] = time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
	require.NoError(t, f.cache.Set(ctx, key, cached, time.Minute))
	result, err := f.service.Callback(ctx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: state, Code: conv.PtrEmpty("unused"), Error: nil})
	require.NoError(t, err)
	require.Contains(t, result.Location, "invalid_state")
	f.provider.AssertNotCalled(t, "Exchange", mock.Anything, mock.Anything)
}

func TestWrongWorkspacePreservesCredentials(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	first := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	state := begin(t, ctx, f, &first.ID)
	f.provider.On("Exchange", mock.Anything, "wrong").Return(&slackdirectoryconnections.Authorization{WorkspaceID: "TEXAMPLE02"}, nil).Once()
	result, err := f.service.Callback(ctx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: state, Code: conv.PtrEmpty("wrong"), Error: nil})
	require.NoError(t, err)
	require.Contains(t, result.Location, "wrong_workspace")
	rows, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, rows.Connections, 1)
	require.Equal(t, first, rows.Connections[0])
}

func TestDisconnectInvalidatesPendingAuthorization(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	first := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	state := begin(t, ctx, f, &first.ID)
	_, err := f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: first.ID, Generation: first.Generation})
	require.NoError(t, err)
	f.provider.On("Exchange", mock.Anything, "stale").Return(&slackdirectoryconnections.Authorization{WorkspaceID: "TEXAMPLE01", Scopes: []string{"users:read", "users:read.email"}, Tokens: slackdirectoryconnections.TokenBundle{Version: 1}}, nil).Once()
	result, err := f.service.Callback(ctx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: state, Code: conv.PtrEmpty("stale"), Error: nil})
	require.NoError(t, err)
	require.Contains(t, result.Location, "connection_changed")
	rows, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "disconnected", rows.Connections[0].Status)
}

func TestCancelledAuthorizationPreservesConnection(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	first := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	result, err := f.service.Callback(ctx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: begin(t, ctx, f, &first.ID), Code: nil, Error: conv.PtrEmpty("access_denied")})
	require.NoError(t, err)
	require.Contains(t, result.Location, "slack_result=cancelled")
	require.Contains(t, result.Location, "https://dashboard.example/")
	rows, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, first, rows.Connections[0])
}

func TestSessionAndTenantBinding(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	state := begin(t, ctx, f, nil)
	other := *f.auth
	other.SessionID = conv.PtrEmpty("other-session")
	result, err := f.service.Callback(contextvalues.SetAuthContext(ctx, &other), &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: state, Code: conv.PtrEmpty("unused"), Error: nil})
	require.NoError(t, err)
	require.Contains(t, result.Location, "invalid_state")
	first := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	other.ActiveOrganizationID = "org_other_synthetic"
	other.SessionID = f.auth.SessionID
	f.flags.SetFlag(feature.FlagClaudeTagSupport, other.ActiveOrganizationID, true)
	otherCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &other), authz.NewGrant(authz.ScopeOrgAdmin, other.ActiveOrganizationID))
	rows, err := f.service.List(otherCtx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, rows.Connections)
	_, err = f.service.Disconnect(otherCtx, &gen.DisconnectPayload{SessionToken: nil, ID: first.ID, Generation: first.Generation})
	require.Error(t, err)
	_, err = f.service.Begin(otherCtx, &gen.BeginPayload{SessionToken: nil, ConnectionID: &first.ID})
	require.Error(t, err)
	result, err = f.service.Callback(otherCtx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: begin(t, ctx, f, nil), Code: conv.PtrEmpty("unused"), Error: nil})
	require.NoError(t, err)
	require.Contains(t, result.Location, "invalid_state")
}

func TestFlagAndRBAC(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	denied := authztest.WithExactGrants(t, ctx)
	_, err := f.service.List(denied, &gen.ListPayload{SessionToken: nil})
	require.Error(t, err)
	_, err = f.service.Begin(denied, &gen.BeginPayload{SessionToken: nil, ConnectionID: nil})
	require.Error(t, err)
	f.flags.SetFlag(feature.FlagClaudeTagSupport, f.auth.ActiveOrganizationID, false)
	_, err = f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.Error(t, err)
	_, err = f.service.Begin(ctx, &gen.BeginPayload{SessionToken: nil, ConnectionID: nil})
	require.Error(t, err)
	f.flags.SetFlag(feature.FlagClaudeTagSupport, f.auth.ActiveOrganizationID, true)
	missingSession := *f.auth
	missingSession.SessionID = nil
	_, err = f.service.List(contextvalues.SetAuthContext(ctx, &missingSession), &gen.ListPayload{SessionToken: nil})
	require.Error(t, err)
}

func TestNewAuthorizationRejectsConcurrentWorkspaceReplacement(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	stale := begin(t, ctx, f, nil)
	authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	f.provider.On("Exchange", mock.Anything, "stale").Return(&slackdirectoryconnections.Authorization{WorkspaceID: "TEXAMPLE01"}, nil).Once()
	result, err := f.service.Callback(ctx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: stale, Code: conv.PtrEmpty("stale"), Error: nil})
	require.NoError(t, err)
	require.Contains(t, result.Location, "connection_changed")
}

func TestMissingResponseDoesNotConsumeState(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	state := begin(t, ctx, f, nil)
	result, err := f.service.Callback(ctx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: state, Code: nil, Error: nil})
	require.NoError(t, err)
	require.Contains(t, result.Location, "invalid_state")
	authorize(t, ctx, f, state, "TEXAMPLE01")
}

func TestStaleDisconnect(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	first := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	newer := authorize(t, ctx, f, begin(t, ctx, f, &first.ID), "TEXAMPLE01")
	_, err := f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: first.ID, Generation: first.Generation})
	require.Error(t, err)
	rows, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, newer, rows.Connections[0])
}

type failingFlags struct{ *feature.InMemory }

func (f failingFlags) EvaluateFlag(context.Context, feature.Flag, string, map[string]string) (feature.Evaluation, error) {
	return feature.EvaluationEnabled, errors.New("provider unavailable")
}

func TestUnavailableFlagAndProvider(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	for _, flags := range []feature.Provider{nil, &feature.InMemory{}, failingFlags{f.flags}} {
		svc := f.build(flags, f.provider)
		_, err := svc.List(ctx, &gen.ListPayload{SessionToken: nil})
		require.Error(t, err)
		_, err = svc.Begin(ctx, &gen.BeginPayload{SessionToken: nil, ConnectionID: nil})
		require.Error(t, err)
	}
	svc := f.build(f.flags, nil)
	list, err := svc.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.False(t, list.AuthorizationConfigured)
	_, err = svc.Begin(ctx, &gen.BeginPayload{SessionToken: nil, ConnectionID: nil})
	require.Error(t, err)
}

func TestConcurrentCallbackConsumesStateOnce(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	state := begin(t, ctx, f, nil)
	f.provider.On("Exchange", mock.Anything, "concurrent").Return(&slackdirectoryconnections.Authorization{WorkspaceID: "TEXAMPLE01", Scopes: []string{"users:read", "users:read.email"}, Tokens: slackdirectoryconnections.TokenBundle{Version: 1, AccessToken: "synthetic"}}, nil).Once()
	results := make(chan string, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			<-start
			result, err := f.service.Callback(ctx, &slackdirectoryconnections.CallbackPayload{SessionToken: nil, State: state, Code: conv.PtrEmpty("concurrent"), Error: nil})
			if err != nil {
				results <- "unexpected_error"
				return
			}
			results <- result.Location
		})
	}
	close(start)
	workers.Wait()
	close(results)
	connected, rejected := 0, 0
	for result := range results {
		if strings.Contains(result, "slack_result=connected") {
			connected++
		}
		if strings.Contains(result, "invalid_state") {
			rejected++
		}
	}
	require.Equal(t, 1, connected)
	require.Equal(t, 1, rejected)
	f.provider.AssertExpectations(t)
}

func TestExpiredCredentialsRequireReconnect(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	tokens, err := json.Marshal(slackdirectoryconnections.TokenBundle{Version: 1, AccessToken: "synthetic", RefreshToken: "synthetic-refresh", ExpiresAt: conv.PtrEmpty(time.Now().Add(-time.Minute)), TokenType: "bot"})
	require.NoError(t, err)
	ciphertext, err := f.enc.Encrypt(tokens)
	require.NoError(t, err)
	_, err = repo.New(f.db).CreateSlackDirectoryConnection(ctx, repo.CreateSlackDirectoryConnectionParams{OrganizationID: f.auth.ActiveOrganizationID, SlackTeamID: "TEXAMPLE01", SlackTeamName: conv.ToPGText("Example workspace"), CredentialsEncrypted: conv.ToPGText(ciphertext), GrantedScopes: []string{}, Generation: uuid.New()})
	require.NoError(t, err)
	list, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "reconnect_required", list.Connections[0].Status)
	require.Equal(t, "authorization_expired", *list.Connections[0].LastErrorCode)
	serialized, err := json.Marshal(list) //nolint:musttag // Verify generated service views never contain credentials.
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "synthetic")
	connection := list.Connections[0]
	_, err = f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: connection.ID, Generation: connection.Generation})
	require.NoError(t, err)
	entry, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionSlackDirectoryConnectionDisconnect)
	require.NoError(t, err)
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(entry.BeforeSnapshot, &snapshot))
	require.Equal(t, "reconnect_required", snapshot["Status"])
	require.NotContains(t, string(entry.BeforeSnapshot), "synthetic")
}
