package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/userblock"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func blockUserAdminContext(ctx context.Context) context.Context {
	return contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		OIDCSubject: "oidc-staff-operator",
		Email:       "operator@example.invalid",
		Name:        "Staff Operator",
	})
}

func TestBlockUser_FullFlowWithMockedWorkOS(t *testing.T) {
	t.Parallel()

	var (
		mu              sync.Mutex
		metadataBody    map[string]any
		revokedSessions []string
		radarEntries    []string
		sessionListPage int
	)

	workosClient := newAdminWorkOSHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")

		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/user_management/users/workos_block_target":
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&metadataBody))
			_, _ = w.Write([]byte(`{"id":"workos_block_target","email":"blocked@example.invalid"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/user_management/users/workos_block_target/sessions":
			sessionListPage++
			if r.URL.Query().Get("after") == "" {
				_, _ = w.Write([]byte(`{"data":[{"id":"wos_sess_1"},{"id":"wos_sess_2"}],"list_metadata":{"after":"page_2"}}`))
				return
			}
			assert.Equal(t, "page_2", r.URL.Query().Get("after"))
			_, _ = w.Write([]byte(`{"data":[{"id":"wos_sess_3"}],"list_metadata":{"after":""}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/user_management/sessions/revoke":
			var body struct {
				SessionID string `json:"session_id"`
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			if body.SessionID == "wos_sess_3" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"session not found"}`))
				return
			}
			revokedSessions = append(revokedSessions, body.SessionID)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"revoked"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/radar/lists/email/block":
			var body struct {
				Entry string `json:"entry"`
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			radarEntries = append(radarEntries, body.Entry)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected WorkOS request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, workosClient)
	ctx = blockUserAdminContext(ctx)

	uRepo := usersrepo.New(conn)
	oRepo := orgRepo.New(conn)
	kRepo := keysrepo.New(conn)
	pmRepo := platformrepo.New(conn)

	const (
		targetUserID   = "user-admin-block-target"
		targetWorkOSID = "workos_block_target"
		peerUserID     = "user-admin-block-peer"
		soleOrgID      = "org-admin-block-sole"
		sharedOrgID    = "org-admin-block-shared"
	)

	_, err := uRepo.UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          targetUserID,
		Email:       "Blocked@Example.Invalid",
		DisplayName: "Blocked Target",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)
	require.NoError(t, uRepo.SetUserWorkosID(ctx, usersrepo.SetUserWorkosIDParams{
		ID:       targetUserID,
		WorkosID: conv.ToPGText(targetWorkOSID),
	}))
	_, err = uRepo.UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          peerUserID,
		Email:       "peer@example.invalid",
		DisplayName: "Peer User",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)

	seedOrg(t, ctx, conn, orgFixture{id: soleOrgID, name: "Sole Org", slug: "admin-block-sole"})
	seedOrg(t, ctx, conn, orgFixture{id: sharedOrgID, name: "Shared Org", slug: "admin-block-shared"})

	for _, rel := range []struct{ orgID, userID string }{
		{soleOrgID, targetUserID},
		{sharedOrgID, targetUserID},
		{sharedOrgID, peerUserID},
	} {
		_, err := oRepo.UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
			OrganizationID: rel.orgID,
			UserID:         conv.ToPGText(rel.userID),
		})
		require.NoError(t, err)
	}

	keyRow, err := kRepo.CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  sharedOrgID,
		ProjectID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		CreatedByUserID: targetUserID,
		Name:            "Target Key",
		KeyPrefix:       "sk_blk99",
		KeyHash:         "hash-admin-block-target-key",
		Scopes:          []string{"mcp:read"},
	})
	require.NoError(t, err)

	pmClient, err := pmRepo.CreatePlatformMCPOAuthClient(ctx, platformrepo.CreatePlatformMCPOAuthClientParams{
		ClientID:              "pmcp-admin-block-client",
		ClientSecretHash:      conv.ToPGTextEmpty(""),
		ClientName:            "Target Platform Client",
		RedirectUris:          []string{"http://127.0.0.1:8123/cb"},
		ClientSecretExpiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
	})
	require.NoError(t, err)
	genID := uuid.New()
	future := conv.ToPGTimestamptz(time.Now().Add(time.Hour))
	pmConn, err := pmRepo.CreatePlatformMCPConnection(ctx, platformrepo.CreatePlatformMCPConnectionParams{
		ID:                     uuid.New(),
		OrganizationID:         soleOrgID,
		SubjectUrn:             urn.NewUserSubject(targetUserID).String(),
		OauthClientID:          pmClient.ID,
		ActiveGeneration:       genID,
		AuthorizationExpiresAt: future,
	})
	require.NoError(t, err)
	_, err = pmRepo.CreatePlatformMCPSession(ctx, platformrepo.CreatePlatformMCPSessionParams{
		ID:                   uuid.New(),
		OrganizationID:       soleOrgID,
		ConnectionID:         pmConn.ID,
		OauthClientID:        pmClient.ID,
		ConnectionGeneration: genID,
		Jti:                  "pmcp-admin-block-jti",
		RefreshTokenHash:     "pmcp-admin-block-refresh",
		ExpiresAt:            future,
		RefreshExpiresAt:     future,
	})
	require.NoError(t, err)

	gate := userblock.NewGate(conn)
	wasBlocked, err := gate.Blocked(ctx, targetUserID)
	require.NoError(t, err)
	require.False(t, wasBlocked)

	reason := "automated abuse"
	res, err := svc.BlockUser(ctx, &gen.BlockUserPayload{
		UserID: targetUserID,
		Reason: &reason,
	})
	require.NoError(t, err)

	require.Equal(t, targetUserID, res.UserID)
	require.Equal(t, "Blocked@Example.Invalid", res.Email)
	require.NotNil(t, res.WorkosUserID)
	require.Equal(t, targetWorkOSID, *res.WorkosUserID)
	require.False(t, res.AlreadyBlacklisted)
	require.NotEmpty(t, res.BlacklistedAt)
	require.Equal(t, []string{soleOrgID}, res.DisabledOrganizationIds)
	require.Equal(t, 1, res.RevokedAPIKeyCount)
	require.Equal(t, 1, res.RevokedPlatformMcpConnectionCount)
	require.Equal(t, 1, res.RevokedPlatformMcpSessionCount)
	require.Equal(t, 1, res.RevokedPlatformMcpOauthClientCount)

	require.Equal(t, blockStepSucceeded, res.SessionCache.Status)
	require.Equal(t, blockStepSucceeded, res.WorkosMetadata.Status)
	require.Equal(t, blockStepSucceeded, res.WorkosSessions.Status)
	require.Equal(t, 2, res.WorkosSessionsRevokedCount)
	require.Equal(t, blockStepSucceeded, res.WorkosRadar.Status)
	require.Equal(t, blockStepNotConfigured, res.RegistryDisable.Status)
	require.Len(t, res.Warnings, 1)
	require.Contains(t, res.Warnings[0], "POST /v1/admin/users/{userID}/disable")

	// Verify WorkOS HTTP side effects.
	mu.Lock()
	require.Equal(t, map[string]any{"blacklisted": "true"}, metadataBody["metadata"])
	require.Equal(t, 2, sessionListPage)
	require.ElementsMatch(t, []string{"wos_sess_1", "wos_sess_2"}, revokedSessions)
	require.Equal(t, []string{"blocked@example.invalid"}, radarEntries)
	mu.Unlock()

	// Verify DB state + immediate in-memory gate invalidation + audit logs.
	isBlocked, err := gate.Blocked(ctx, targetUserID)
	require.NoError(t, err)
	require.True(t, isBlocked)
	require.NotNil(t, readDisabledAt(t, ctx, conn, soleOrgID))
	require.Nil(t, readDisabledAt(t, ctx, conn, sharedOrgID))

	revokedKey, err := kRepo.GetAPIKeyByID(ctx, keysrepo.GetAPIKeyByIDParams{ID: keyRow.ID, OrganizationID: sharedOrgID})
	require.NoError(t, err)
	require.True(t, revokedKey.DeletedAt.Valid)
	require.True(t, revokedKey.Deleted)

	_, err = pmRepo.GetActivePlatformMCPOAuthClientByClientID(ctx, "pmcp-admin-block-client")
	require.ErrorIs(t, err, pgx.ErrNoRows)

	blockAudit, err := audittest.LatestAuditLogByActionAndSubject(ctx, conn, audit.ActionAccessMemberBlock, targetUserID)
	require.NoError(t, err)
	require.Equal(t, "oidc-staff-operator", blockAudit.ActorID)
	require.Equal(t, "Staff Operator", blockAudit.ActorDisplay)

	// Re-running on an already-blacklisted user is idempotent.
	second, err := svc.BlockUser(ctx, &gen.BlockUserPayload{UserID: targetUserID, Reason: &reason})
	require.NoError(t, err)
	require.True(t, second.AlreadyBlacklisted)
	require.Equal(t, res.BlacklistedAt, second.BlacklistedAt)
	require.Empty(t, second.DisabledOrganizationIds)
	require.Zero(t, second.RevokedAPIKeyCount)
}

func TestBlockUser_PartialFailureWhenWorkOSErrors(t *testing.T) {
	t.Parallel()

	workosClient := newAdminWorkOSHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/user_management/users/workos_partial":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"metadata update rejected"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/user_management/users/workos_partial/sessions":
			_, _ = w.Write([]byte(`{"data":[{"id":"sess_ok"},{"id":"sess_fail"}],"list_metadata":{"after":""}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/user_management/sessions/revoke":
			var body struct {
				SessionID string `json:"session_id"`
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			if body.SessionID == "sess_fail" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"invalid session"}`))
				return
			}
			assert.Equal(t, "sess_ok", body.SessionID)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"revoked"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/radar/lists/email/block":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"radar not enabled"}`))
		default:
			t.Errorf("unexpected WorkOS request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, workosClient)
	ctx = blockUserAdminContext(ctx)

	uRepo := usersrepo.New(conn)
	const targetUserID = "user-admin-block-partial"
	_, err := uRepo.UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          targetUserID,
		Email:       "partial@example.invalid",
		DisplayName: "Partial Target",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)
	require.NoError(t, uRepo.SetUserWorkosID(ctx, usersrepo.SetUserWorkosIDParams{
		ID:       targetUserID,
		WorkosID: conv.ToPGText("workos_partial"),
	}))

	res, err := svc.BlockUser(ctx, &gen.BlockUserPayload{UserID: targetUserID})
	require.NoError(t, err, "external WorkOS failures must not fail or roll back the committed DB block")

	row, err := uRepo.GetUser(ctx, targetUserID)
	require.NoError(t, err)
	require.True(t, row.BlacklistedAt.Valid, "DB blacklist must remain committed")

	require.Equal(t, blockStepFailed, res.WorkosMetadata.Status)
	require.Equal(t, blockStepPartial, res.WorkosSessions.Status)
	require.Equal(t, 1, res.WorkosSessionsRevokedCount)
	require.Equal(t, blockStepFailed, res.WorkosRadar.Status)
	require.Equal(t, blockStepNotConfigured, res.RegistryDisable.Status)
	require.GreaterOrEqual(t, len(res.Warnings), 4)
}

func TestBlockUser_WithoutWorkOSConfigured(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	ctx = blockUserAdminContext(ctx)

	const targetUserID = "user-admin-block-no-workos"
	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          targetUserID,
		Email:       "noworkos@example.invalid",
		DisplayName: "No WorkOS User",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)

	res, err := svc.BlockUser(ctx, &gen.BlockUserPayload{UserID: targetUserID})
	require.NoError(t, err)
	require.Equal(t, blockStepNotConfigured, res.WorkosMetadata.Status)
	require.Equal(t, blockStepNotConfigured, res.WorkosSessions.Status)
	require.Equal(t, blockStepNotConfigured, res.WorkosRadar.Status)
	require.Equal(t, blockStepNotConfigured, res.RegistryDisable.Status)
	require.Len(t, res.Warnings, 2)
}

func TestBlockUser_UserWithoutWorkOSIDStillBlocksRadarEmail(t *testing.T) {
	t.Parallel()

	var radarBlocked string
	workosClient := newAdminWorkOSHTTPClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/radar/lists/email/block", r.URL.Path)
		var body struct {
			Entry string `json:"entry"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		radarBlocked = body.Entry
		w.WriteHeader(http.StatusOK)
	})

	ctx, svc, conn := newTestAdminServiceWithWorkOS(t, workosClient)
	ctx = blockUserAdminContext(ctx)

	const targetUserID = "user-admin-block-local-only"
	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          targetUserID,
		Email:       "localonly@example.invalid",
		DisplayName: "Local Only",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)

	res, err := svc.BlockUser(ctx, &gen.BlockUserPayload{UserID: targetUserID})
	require.NoError(t, err)
	require.Nil(t, res.WorkosUserID)
	require.Equal(t, blockStepSkipped, res.WorkosMetadata.Status)
	require.Equal(t, blockStepSkipped, res.WorkosSessions.Status)
	require.Equal(t, blockStepSucceeded, res.WorkosRadar.Status)
	require.Equal(t, "localonly@example.invalid", radarBlocked)
}

func TestBlockUser_GuardsAndValidation(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	const staffOrgID = "org-admin-block-staff-guard"
	seedOrg(t, ctx, conn, orgFixture{id: staffOrgID, name: "Staff Guard Org", slug: "staff-guard"})
	svc.staffOrg = staff.NewOrganization(staffOrgID)

	uRepo := usersrepo.New(conn)
	for _, u := range []usersrepo.UpsertUserParams{
		{ID: "user-self", Email: "operator@example.invalid", DisplayName: "Operator", PhotoUrl: conv.ToPGTextEmpty(""), Admin: false},
		{ID: "user-platform-admin", Email: "admin@example.invalid", DisplayName: "Admin", PhotoUrl: conv.ToPGTextEmpty(""), Admin: true},
		{ID: "user-staff-member", Email: "staff@example.invalid", DisplayName: "Staff", PhotoUrl: conv.ToPGTextEmpty(""), Admin: false},
	} {
		_, err := uRepo.UpsertUser(ctx, u)
		require.NoError(t, err)
	}
	_, err := orgRepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: staffOrgID,
		UserID:         conv.ToPGText("user-staff-member"),
	})
	require.NoError(t, err)

	// Missing admin session operator email -> 401.
	_, err = svc.BlockUser(ctx, &gen.BlockUserPayload{UserID: "user-does-not-exist"})
	requireOopsCode(t, err, oops.CodeUnauthorized)

	adminCtx := blockUserAdminContext(ctx)

	// Blank user ID -> 400.
	_, err = svc.BlockUser(adminCtx, &gen.BlockUserPayload{UserID: "   "})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Unknown user ID -> 404.
	_, err = svc.BlockUser(adminCtx, &gen.BlockUserPayload{UserID: "user-does-not-exist"})
	requireOopsCode(t, err, oops.CodeNotFound)

	// Self-block -> 400.
	_, err = svc.BlockUser(adminCtx, &gen.BlockUserPayload{UserID: "user-self"})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Active platform admin -> 403.
	_, err = svc.BlockUser(adminCtx, &gen.BlockUserPayload{UserID: "user-platform-admin"})
	requireOopsCode(t, err, oops.CodeForbidden)

	// Active staff org member -> 403.
	_, err = svc.BlockUser(adminCtx, &gen.BlockUserPayload{UserID: "user-staff-member"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestBlockUser_HTTPTransportRequiresAuthAndStrictJSON(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          "user-http-block",
		Email:       "httpblock@example.invalid",
		DisplayName: "HTTP Block",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)

	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)

	// 1. Unauthenticated request is rejected with 401.
	unauthRec := httptest.NewRecorder()
	unauthReq := httptest.NewRequest(http.MethodPost, "/admin/users.block", strings.NewReader(`{"user_id":"user-http-block"}`))
	unauthReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(unauthRec, unauthReq)
	require.Equal(t, http.StatusUnauthorized, unauthRec.Code)

	// Create a valid staff session cookie via the test service's session store.
	cookieToken := makeAdminFeatureSession(t, ctx, svc, "operator@example.com")

	// 2. Unknown JSON field is rejected with 400 by strictAdminJSON.
	badJSONRec := httptest.NewRecorder()
	badJSONReq := httptest.NewRequest(http.MethodPost, "/admin/users.block", strings.NewReader(`{"user_id":"user-http-block","unexpected_field":true}`))
	badJSONReq.Header.Set("Content-Type", "application/json")
	badJSONReq.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: cookieToken})
	handler.ServeHTTP(badJSONRec, badJSONReq)
	require.Equal(t, http.StatusBadRequest, badJSONRec.Code)

	// 3. Valid authenticated request succeeds with 200 and returns the structured summary.
	okRec := httptest.NewRecorder()
	okReq := httptest.NewRequest(http.MethodPost, "/admin/users.block", strings.NewReader(`{"user_id":"user-http-block","reason":"policy violation"}`))
	okReq.Header.Set("Content-Type", "application/json")
	okReq.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: cookieToken})
	handler.ServeHTTP(okRec, okReq)
	require.Equal(t, http.StatusOK, okRec.Code, okRec.Body.String())

	var got map[string]any
	require.NoError(t, json.Unmarshal(okRec.Body.Bytes(), &got))
	require.Equal(t, "user-http-block", got["user_id"])
	require.Equal(t, "httpblock@example.invalid", got["email"])
	require.Equal(t, false, got["already_blacklisted"])
}
