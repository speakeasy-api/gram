package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// expireConsentGrant stores a grant for client whose authorization lifetime has ended.
func expireConsentGrant(t *testing.T, ctx context.Context, fx consentActionFixture, client uuid.UUID) {
	t.Helper()
	accessEncrypted, err := fx.ti.enc.Encrypt([]byte("dead-token"))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(fx.ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:             fx.subject,
		UserSessionIssuerID:    fx.shared,
		RemoteSessionClientID:  client,
		AccessTokenEncrypted:   accessEncrypted,
		AuthorizationExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Second)),
		RefreshTokenEncrypted:  pgtype.Text{},
		RefreshExpiresAt:       pgtype.Timestamptz{},
		AccessExpiresAt:        pgtype.Timestamptz{},
		Scopes:                 []string{},
		Resource:               conv.ToPGTextEmpty(consentChainedUpstream),
	})
	require.NoError(t, err)
}

func postApprove(t *testing.T, ctx context.Context, fx consentActionFixture) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"approve"}, "tool_filtering": {"off"}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsent(w, req.WithContext(ctx), fx.endpoint))
	return w
}

// An approval cannot issue a token while every connection has expired. The
// page stays usable, so the person can reconnect and approve from it.
func TestConsentPost_RefusesWhileEveryConnectionHasExpired(t *testing.T) {
	t.Parallel()

	ctx, fx := seedTwoClientConsentEndpoint(t, "expired-all")
	expireConsentGrant(t, ctx, fx, fx.clientA)

	w := postApprove(t, ctx, fx)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, fx.endpoint.Slug, strings.Split(strings.TrimPrefix(loc.Path, "/mcp/"), "/")[0], "returns to the consent page, not the client")
	require.Equal(t, fx.stateID, loc.Query().Get("state"))
	require.Empty(t, loc.Query().Get("code"))

	code, page, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, page)
	require.Contains(t, page, `data-consent-enabled="false"`, "the page offers no approval")

	// Reconnecting makes the same challenge approvable.
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, fx.clientA, fx.subject, "token-a", consentChainedUpstream)
	code, page, _ = render(t, fx)
	require.Equal(t, http.StatusOK, code, page)
	require.Contains(t, page, `data-consent-enabled="true"`)
	grant := approveConsent(t, ctx, fx)
	require.Equal(t, map[uuid.UUID]uuid.UUID{fx.endpoint.McpServerID.UUID: fx.clientA}, grant.UpstreamClientPicks)
}

func TestConsentPost_ApprovesWhenAnotherConnectionStillWorks(t *testing.T) {
	t.Parallel()

	ctx, fx := seedTwoClientConsentEndpoint(t, "expired-one")
	expireConsentGrant(t, ctx, fx, fx.clientA)
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.shared, fx.clientB, fx.subject, "token-b", consentChainedUpstream)

	grant := approveConsent(t, ctx, fx)
	require.Equal(t, map[uuid.UUID]uuid.UUID{fx.endpoint.McpServerID.UUID: fx.clientB}, grant.UpstreamClientPicks)
}

// The verify refusal says why nothing was probed instead of blaming routing.
func TestServeConsentAction_ValidateExpiredGrantNamesTheCause(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "aim857-cause")
	projectID, _ := consentTestTenant(t, ctx)
	sess := storedSession(t, ctx, fx)
	require.NoError(t, remotesessions_repo.New(fx.ti.conn).SetRemoteSessionAccessExpiresAt(ctx, remotesessions_repo.SetRemoteSessionAccessExpiresAtParams{
		AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		ID:              sess.ID,
		ProjectID:       conv.ToNullUUID(projectID),
	}))

	_, err := postValidate(t, fx, fx.clientID)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.ErrorContains(t, shareable.Unwrap(), "no usable credential")
	require.NotContains(t, shareable.Unwrap().Error(), "routes to no upstream")
}
