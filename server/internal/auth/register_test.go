package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	authserver "github.com/speakeasy-api/gram/server/gen/http/auth/server"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func TestService_Register(t *testing.T) {
	t.Parallel()

	t.Run("successful register creates organization", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{} // User has no organizations
		ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

		// Create and store a session with no active organization
		session := sessions.Session{
			SessionID:            t.Name(),
			UserID:               userInfo.UserID,
			ActiveOrganizationID: "", // No active organization
			WorkOSSessionID:      "",
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			AccountType:          "test",
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.RegisterPayload{
			OrgName:      "Test Organization",
			SessionToken: nil,
		}

		err = instance.service.Register(ctx, payload)
		require.NoError(t, err)

		storedSession, err := instance.sessionManager.GetSession(ctx, session.SessionID)
		require.NoError(t, err)
		require.NotEmpty(t, storedSession.ActiveOrganizationID)
		require.Empty(t, instance.trialNotifier.trialStarted)
	})

	t.Run("register fails when user already has active organization", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Create and store a session with active organization
		session := sessions.Session{
			SessionID:            t.Name(),
			UserID:               userInfo.UserID,
			ActiveOrganizationID: userInfo.Organizations[0].ID, // Has active organization
			WorkOSSessionID:      "",
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			AccountType:          "test",
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.RegisterPayload{
			OrgName:      "Test Organization",
			SessionToken: nil,
		}

		err = instance.service.Register(ctx, payload)
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeInvalid, oopsErr.Code)
		require.Contains(t, err.Error(), "user already has an active organization")
	})

	t.Run("register fails when org name is empty", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{} // User has no organizations
		ctx, instance := newTestAuthService(t, userInfo)

		// Create and store a session with no active organization
		session := sessions.Session{
			SessionID:            t.Name(),
			UserID:               userInfo.UserID,
			ActiveOrganizationID: "", // No active organization
			WorkOSSessionID:      "",
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			AccountType:          "test",
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.RegisterPayload{
			OrgName:      "",
			SessionToken: nil,
		}

		err = instance.service.Register(ctx, payload)
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeInvalid, oopsErr.Code)
		require.Contains(t, err.Error(), "org name is required")
	})

	t.Run("register fails with invalid characters in org name", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{} // User has no organizations
		ctx, instance := newTestAuthService(t, userInfo)

		// Create and store a session with no active organization
		session := sessions.Session{
			SessionID:            t.Name(),
			UserID:               userInfo.UserID,
			ActiveOrganizationID: "", // No active organization
			WorkOSSessionID:      "",
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		// Set up auth context
		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			AccountType:          "test",
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		testCases := []struct {
			name    string
			orgName string
		}{
			{"with a newline", "Test\nOrg"},
			{"with a tab", "Test\tOrg"},
			{"with a bidi override", "Test\u202eOrg"},
			{"with a zero-width space", "Test\u200bOrg"},
			{"with invalid utf-8", "Test\xffOrg"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				payload := &gen.RegisterPayload{
					OrgName:      tc.orgName,
					SessionToken: nil,
				}

				err := instance.service.Register(ctx, payload)
				require.Error(t, err)

				var oopsErr *oops.ShareableError
				require.ErrorAs(t, err, &oopsErr)
				require.Equal(t, oops.CodeInvalid, oopsErr.Code)
				require.Contains(t, err.Error(), "organization name contains invalid characters")
			})
		}
	})

	t.Run("register fails when org name has too few letters or numbers", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{} // User has no organizations
		ctx, instance := newTestAuthService(t, userInfo)

		session := sessions.Session{
			SessionID:            t.Name(),
			UserID:               userInfo.UserID,
			ActiveOrganizationID: "", // No active organization
			WorkOSSessionID:      "",
		}
		err := instance.sessionManager.StoreSession(ctx, session)
		require.NoError(t, err)

		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: session.ActiveOrganizationID,
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			AccountType:          "test",
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		// Names built entirely from punctuation or symbols carry no meaning and
		// must be rejected server-side, as must a lone initial.
		testCases := []struct {
			name    string
			orgName string
		}{
			{"only hyphens", "-----"},
			{"only underscores", "___"},
			{"mixed punctuation", "- _ -"},
			{"only symbols", "€ £ ¥"},
			{"single letter", "A-"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				payload := &gen.RegisterPayload{
					OrgName:      tc.orgName,
					SessionToken: nil,
				}

				err := instance.service.Register(ctx, payload)
				require.Error(t, err)

				var oopsErr *oops.ShareableError
				require.ErrorAs(t, err, &oopsErr)
				require.Equal(t, oops.CodeInvalid, oopsErr.Code)
				require.Contains(t, err.Error(), "organization name must contain at least 2 letters or numbers")
			})
		}
	})

	t.Run("register allows valid characters in org name", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name    string
			orgName string
		}{
			{"alphanumeric", "TestOrg123"},
			{"with spaces", "Test Organization"},
			{"with hyphens", "Test-Organization"},
			{"with underscores", "Test_Organization"},
			{"mixed valid characters", "Test-Org_123 Demo"},
			{"with punctuation", "Test Org, Inc."},
			{"with an apostrophe", "Test's Organization"},
			{"with an ampersand", "Test & Organization"},
			{"accented latin", "Tëst Örganization"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				// Each subtest gets its own service, session and context so they
				// can run in parallel without racing on shared Redis state.
				userInfo := defaultMockUserInfo()
				userInfo.Organizations = []MockOrganizationEntry{}
				ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

				sessionID := "session-" + tc.name
				session := sessions.Session{
					SessionID:            sessionID,
					UserID:               userInfo.UserID,
					ActiveOrganizationID: "",
					WorkOSSessionID:      "",
				}
				err := instance.sessionManager.StoreSession(ctx, session)
				require.NoError(t, err)

				ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
					SessionID:            &sessionID,
					UserID:               session.UserID,
					ActiveOrganizationID: "",
					AccountType:          "test",
					Email:                &userInfo.Email,
				})

				err = instance.service.Register(ctx, &gen.RegisterPayload{
					OrgName:      tc.orgName,
					SessionToken: nil,
				})
				require.NoError(t, err)
			})
		}
	})

	t.Run("register fails when no session context", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Don't authenticate, so no session context
		payload := &gen.RegisterPayload{
			OrgName:      "Test Organization",
			SessionToken: nil,
		}

		err := instance.service.Register(ctx, payload)
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("register fails when session ID is nil", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		ctx, instance := newTestAuthService(t, userInfo)

		// Create auth context but with nil session ID
		authCtx := &contextvalues.AuthContext{
			UserID:               "test-user-123",
			ActiveOrganizationID: "",
			SessionID:            nil,
			ProjectID:            nil,
			OrganizationSlug:     "",
			Email:                &userInfo.Email,
			AccountType:          "test",
			ProjectSlug:          nil,
			APIKeyScopes:         nil,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		payload := &gen.RegisterPayload{
			OrgName:      "Test Organization",
			SessionToken: nil,
		}

		err := instance.service.Register(ctx, payload)
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("register preserves WorkOSSessionID", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{} // no orgs yet
		ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

		session := sessions.Session{
			SessionID:            "workos-register-test",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: "",
			WorkOSSessionID:      "workos-sid-register-456",
		}
		require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

		authCtx := &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: "",
			AccountType:          "test",
			Email:                &userInfo.Email,
		}
		ctx = contextvalues.SetAuthContext(ctx, authCtx)

		err := instance.service.Register(ctx, &gen.RegisterPayload{
			OrgName: "Preserve Session Org",
		})
		require.NoError(t, err)

		stored, err := instance.sessionManager.GetSession(ctx, session.SessionID)
		require.NoError(t, err)
		require.Equal(t, "workos-sid-register-456", stored.WorkOSSessionID, "WorkOSSessionID must survive Register")
		require.NotEmpty(t, stored.ActiveOrganizationID, "should have an active org after Register")
	})

	t.Run("register uses base slug when no collision exists", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{}
		ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

		session := sessions.Session{
			SessionID:            "slug-no-collision",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: "",
		}
		require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

		ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: "",
			AccountType:          "test",
			Email:                &userInfo.Email,
		})

		err := instance.service.Register(ctx, &gen.RegisterPayload{
			OrgName: "Unique Org Name",
		})
		require.NoError(t, err)

		// Verify the slug is the plain slugified version with no suffix.
		orgQueries := orgRepo.New(instance.conn)
		org, err := orgQueries.GetOrganizationMetadataBySlug(ctx, "unique-org-name")
		require.NoError(t, err)
		assert.Equal(t, "unique-org-name", org.Slug)
		assert.Equal(t, "Unique Org Name", org.Name)
	})

	t.Run("register appends random suffix on slug collision", func(t *testing.T) {
		t.Parallel()

		userInfo := defaultMockUserInfo()
		userInfo.Organizations = []MockOrganizationEntry{}
		ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

		// Pre-create an org that occupies the slug "collide-me".
		require.NoError(t, instance.createTestOrganization(ctx, MockOrganizationEntry{
			ID:   "existing-org-collide",
			Name: "Collide Me",
			Slug: "collide-me",
		}, ""))

		session := sessions.Session{
			SessionID:            "slug-collision",
			UserID:               userInfo.UserID,
			ActiveOrganizationID: "",
		}
		require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

		ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
			SessionID:            &session.SessionID,
			UserID:               session.UserID,
			ActiveOrganizationID: "",
			AccountType:          "test",
			Email:                &userInfo.Email,
		})

		err := instance.service.Register(ctx, &gen.RegisterPayload{
			OrgName: "Collide Me",
		})
		require.NoError(t, err)

		// The new org must NOT have the bare slug — it should have a random suffix.
		stored, err := instance.sessionManager.GetSession(ctx, session.SessionID)
		require.NoError(t, err)
		require.NotEmpty(t, stored.ActiveOrganizationID)

		orgQueries := orgRepo.New(instance.conn)
		newOrg, err := orgQueries.GetOrganizationMetadata(ctx, stored.ActiveOrganizationID)
		require.NoError(t, err)

		assert.NotEqual(t, "collide-me", newOrg.Slug, "slug must not collide with existing org")
		assert.Contains(t, newOrg.Slug, "collide-me-", "slug should start with base and have a random suffix")
		assert.Len(t, newOrg.Slug, len("collide-me-")+4, "suffix should be 4 hex chars")
	})
}

func TestRegister_CreatesOrganizationForNameWithNoDerivableSlug(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	userInfo.Organizations = []MockOrganizationEntry{}
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

	session := sessions.Session{
		SessionID:            "slug-non-latin",
		UserID:               userInfo.UserID,
		ActiveOrganizationID: "",
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: "",
		AccountType:          "test",
		Email:                &userInfo.Email,
	})

	const orgName = "アクメ株式会社"
	require.NoError(t, instance.service.Register(ctx, &gen.RegisterPayload{OrgName: orgName}))

	stored, err := instance.sessionManager.GetSession(ctx, session.SessionID)
	require.NoError(t, err)
	require.NotEmpty(t, stored.ActiveOrganizationID)

	org, err := orgRepo.New(instance.conn).GetOrganizationMetadata(ctx, stored.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, orgName, org.Name)
	require.Regexp(t, `^org-[a-z1-9]{8}$`, org.Slug)
}

// TestRegisterHTTP_OrganizationlessSessionProvisionsOrg issues POST
// /rpc/auth.register through the mounted HTTP stack so session
// authentication and authz.PrepareContext run before the handler.
func TestRegisterHTTP_OrganizationlessSessionProvisionsOrg(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	userInfo.Organizations = []MockOrganizationEntry{}
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

	session := sessions.Session{
		SessionID:            t.Name(),
		UserID:               userInfo.UserID,
		ActiveOrganizationID: "",
		WorkOSSessionID:      "",
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	mux := goahttp.NewMuxer()
	auth.Attach(mux, instance.service)

	const orgName = "HTTP Register Org"
	body, err := json.Marshal(map[string]string{"org_name": orgName})
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/rpc/auth.register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Session", session.SessionID)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	stored, err := instance.sessionManager.GetSession(ctx, session.SessionID)
	require.NoError(t, err)
	require.NotEmpty(t, stored.ActiveOrganizationID)

	org, err := orgRepo.New(instance.conn).GetOrganizationMetadata(ctx, stored.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, orgName, org.Name)

	infoReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/rpc/auth.info", nil)
	infoReq.Header.Set("Gram-Session", session.SessionID)
	infoRec := httptest.NewRecorder()
	mux.ServeHTTP(infoRec, infoReq)
	require.Equal(t, http.StatusOK, infoRec.Code, infoRec.Body.String())

	var info authserver.InfoResponseBody
	require.NoError(t, json.Unmarshal(infoRec.Body.Bytes(), &info))
	require.Equal(t, stored.ActiveOrganizationID, info.ActiveOrganizationID)
	require.Len(t, info.Organizations, 1)
	require.Equal(t, orgName, info.Organizations[0].Name)
	require.Len(t, info.Organizations[0].Projects, 1)
	require.Equal(t, "Default", info.Organizations[0].Projects[0].Name)
	require.Equal(t, "default", info.Organizations[0].Projects[0].Slug)
}
