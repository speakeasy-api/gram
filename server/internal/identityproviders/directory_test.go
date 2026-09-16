package identityproviders_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const (
	testDirectoryApplicationID = "directory-app-example"
	testDirectoryID            = "directory-example"
	testDirectoryToken         = "directory-token-secret"
)

type directoryOktaServer struct {
	server *httptest.Server

	createdApplications atomic.Int64
	mu                  sync.Mutex
	assignedGroups      []string
	validationErr       error
}

func TestDirectorySetupGuidedLifecycle(t *testing.T) {
	t.Parallel()

	fake := newDirectoryOktaServer(t)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	createConnection(t, ctx, ti, "https://example.okta.com")
	prepareDirectoryConnection(t, ctx, ti)
	storeDirectoryHandoff(t, ctx, ti, "https://directory.example.test/scim/v2", testDirectoryToken)

	setup, err := ti.service.DescribeSetup(ctx, &gen.DescribeSetupPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Len(t, setup.Steps, 3)
	require.Equal(t, "directory", setup.Steps[2].Key)
	require.Equal(t, "not_started", setup.Steps[2].State)
	require.Nil(t, setup.Steps[2].PortalIntent)
	require.Empty(t, setup.Steps[2].PrintedValues)

	result, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "directory",
		Values:       []*gen.IdentityProviderSetupValue{},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, "awaiting_verification", result.Step.State)
	require.Equal(t, new("https://example-admin.okta.com/admin/app/scim2testapp/instance/"+testDirectoryApplicationID+"/#tab-provisioning"), result.Step.DeepLink)
	require.Equal(t, []*gen.IdentityProviderPrintedValue{
		{Label: "SCIM base URL", Value: "https://directory.example.test/scim/v2", Copyable: true, Secret: false},
		{Label: "Bearer token", Value: testDirectoryToken, Copyable: true, Secret: true},
		{Label: "Provisioning tab", Value: "https://example-admin.okta.com/admin/app/scim2testapp/instance/" + testDirectoryApplicationID + "/#tab-provisioning", Copyable: false, Secret: false},
	}, result.Step.PrintedValues)
	require.Equal(t, int64(1), fake.createdApplications.Load())
	require.Equal(t, []string{"engineering"}, fake.AssignedGroups())
	require.NoError(t, fake.ValidationError())

	row, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, testDirectoryApplicationID, row.DirectoryApplicationID.String)
	require.Equal(t, "configured", row.DirectoryState.String)

	ti.workos.On("ListDirectories", mock.Anything, row.WorkosID.String).Return([]workos.Directory{{
		ID: testDirectoryID, OrganizationID: row.WorkosID.String, Type: "Okta SCIM v2.0", Name: "Okta", State: "linked", CreatedAt: "", UpdatedAt: "",
	}}, nil).Once()
	ti.workos.On("ListDirectoryGroups", mock.Anything, testDirectoryID).Return([]workos.DirectoryGroup{{
		ID: "directory-group", DirectoryID: testDirectoryID, OrganizationID: row.WorkosID.String, Name: "Engineering", CreatedAt: "", UpdatedAt: "",
	}}, nil).Once()
	ti.workos.On("ListDirectoryUsers", mock.Anything, testDirectoryID).Return([]workos.DirectoryUser{{
		ID: "directory-user", DirectoryID: testDirectoryID, OrganizationID: row.WorkosID.String, Email: "member@example.test", State: "active", CustomAttributes: nil, CreatedAt: "", UpdatedAt: "",
	}}, nil).Once()

	verified, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "directory", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "passed", verified.Outcome)
	require.Len(t, verified.Evidence.Reads, 4)
	require.Equal(t, "directory_application", verified.Evidence.Reads[0].Resource)
	require.Equal(t, "directory_connection", verified.Evidence.Reads[1].Resource)
	require.Equal(t, "directory_groups", verified.Evidence.Reads[2].Resource)
	require.Equal(t, 1, *verified.Evidence.Reads[2].Count)
	require.Equal(t, "directory_users", verified.Evidence.Reads[3].Resource)
	require.Equal(t, 1, *verified.Evidence.Reads[3].Count)

	row, err = repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "passed", row.DirectoryState.String)
	require.Equal(t, int32(1), row.DirectoryGroupCount.Int32)
	require.Equal(t, int32(1), row.DirectoryUserCount.Int32)
	require.Equal(t, testDirectoryID, row.DirectoryWorkosID.String)

	_, err = ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{
		StepKey:      "directory",
		Values:       []*gen.IdentityProviderSetupValue{},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), fake.createdApplications.Load())
	row, err = repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "passed", row.DirectoryState.String)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionVerified)
	require.NoError(t, err)
	auditJSON := string(record.Metadata) + string(record.BeforeSnapshot) + string(record.AfterSnapshot)
	require.NotContains(t, auditJSON, testDirectoryToken)
}

func TestDirectoryVerificationRemainsPendingBeforeFirstGroupPush(t *testing.T) {
	t.Parallel()

	fake := newDirectoryOktaServer(t)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	createConnection(t, ctx, ti, "https://example.okta.com")
	prepareDirectoryConnection(t, ctx, ti)
	storeDirectoryHandoff(t, ctx, ti, "https://directory.example.test/scim/v2", testDirectoryToken)
	_, err := ti.service.SubmitSetupStep(ctx, &gen.SubmitSetupStepPayload{StepKey: "directory", Values: []*gen.IdentityProviderSetupValue{}, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)

	row, err := repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	ti.workos.On("ListDirectories", mock.Anything, row.WorkosID.String).Return([]workos.Directory{{
		ID: testDirectoryID, OrganizationID: row.WorkosID.String, Type: "Okta SCIM v2.0", Name: "Okta", State: "linked", CreatedAt: "", UpdatedAt: "",
	}}, nil).Once()
	ti.workos.On("ListDirectoryGroups", mock.Anything, testDirectoryID).Return([]workos.DirectoryGroup{}, nil).Once()
	ti.workos.On("ListDirectoryUsers", mock.Anything, testDirectoryID).Return([]workos.DirectoryUser{}, nil).Once()

	verified, err := ti.service.VerifySetupStep(ctx, &gen.VerifySetupStepPayload{StepKey: "directory", SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, "pending_validation", verified.Outcome)
	require.Contains(t, verified.Detail, "wait for the first directory group push")
	row, err = repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "pending_validation", row.DirectoryState.String)
}

func prepareDirectoryConnection(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	setStoredClientID(t, ctx, ti)
	queries := repo.New(ti.conn)
	row, err := queries.GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.NoError(t, err)
	capabilities := []string{"directory_read", "application_assignment_read", "sign_in_provisioning", "group_assignment"}
	grantedScopes := []string{"okta.apps.read", "okta.groups.read", "okta.groups.manage", "okta.users.read", "okta.apps.manage"}
	require.NoError(t, queries.UpdateOktaIdentityProviderGrantedScopes(ctx, repo.UpdateOktaIdentityProviderGrantedScopesParams{
		GrantedScopes:                grantedScopes,
		OrganizationID:               ti.orgID,
		IdentityProviderConnectionID: row.ID,
	}))
	updated, err := queries.UpdateIdentityProviderVerification(ctx, repo.UpdateIdentityProviderVerificationParams{
		Status:                       "active",
		StatusDetail:                 pgtype.Text{},
		Capabilities:                 capabilities,
		LastVerifiedAt:               conv.ToPGTimestamptz(time.Now().UTC()),
		VerifyEvidence:               nil,
		OrganizationID:               ti.orgID,
		IdentityProviderConnectionID: row.ID,
		ClientID:                     row.ClientID,
		SigningKeyID:                 row.SigningKeyID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), updated)
}

func storeDirectoryHandoff(t *testing.T, ctx context.Context, ti *testInstance, baseURL, token string) {
	t.Helper()
	encrypted, err := ti.encryption.Encrypt([]byte(token))
	require.NoError(t, err)
	_, err = adminrepo.New(ti.conn).AdminSetOrganizationDirectoryHandoff(ctx, adminrepo.AdminSetOrganizationDirectoryHandoffParams{
		OrganizationID:                ti.orgID,
		DirectoryScimBaseUrl:          conv.ToPGText(baseURL),
		DirectoryScimTokenEncrypted:   conv.ToPGText(encrypted),
		DirectoryScimTokenFingerprint: conv.ToPGText("00000000"),
		DirectoryHandoffSetByUserID:   conv.ToPGText("operator@example.test"),
	})
	require.NoError(t, err)
}

func newDirectoryOktaServer(t *testing.T) *directoryOktaServer {
	t.Helper()
	fake := &directoryOktaServer{server: nil, createdApplications: atomic.Int64{}, mu: sync.Mutex{}, assignedGroups: nil, validationErr: nil}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.ServeHTTP))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *directoryOktaServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/oauth2/v1/token":
		if err := r.ParseForm(); err != nil {
			f.fail(w, err)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"` + testAccessToken + `","expires_in":3600,"scope":"` + r.Form.Get("scope") + `"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/apps":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			f.fail(w, err)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			f.fail(w, err)
			return
		}
		if payload["name"] != "scim2testapp" || payload["label"] != "Speakeasy Directory" || len(payload) != 2 {
			f.fail(w, errors.New("unexpected directory application payload"))
			return
		}
		f.createdApplications.Add(1)
		_, _ = w.Write([]byte(`{"id":"` + testDirectoryApplicationID + `","status":"ACTIVE","label":"Speakeasy Directory","signOnMode":"SAML_2_0"}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/groups":
		_, _ = w.Write([]byte(`[
			{"id":"everyone","type":"BUILT_IN","profile":{"name":"Everyone"}},
			{"id":"okta-admins","type":"BUILT_IN","profile":{"name":"Okta Administrators"}},
			{"id":"engineering","type":"OKTA_GROUP","profile":{"name":"Engineering"}}
		]`))
	case r.Method == http.MethodPut && r.URL.Path == "/api/v1/apps/"+testDirectoryApplicationID+"/groups/engineering":
		f.mu.Lock()
		f.assignedGroups = append(f.assignedGroups, "engineering")
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"id":"engineering"}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/apps/"+testDirectoryApplicationID:
		_, _ = w.Write([]byte(`{"id":"` + testDirectoryApplicationID + `","status":"ACTIVE","label":"Speakeasy Directory","signOnMode":"SAML_2_0"}`))
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/apps/"+testDirectoryApplicationID+"/connections/default":
		_, _ = w.Write([]byte(`{"status":"UNKNOWN"}`))
	default:
		http.NotFound(w, r)
	}
}

func (f *directoryOktaServer) fail(w http.ResponseWriter, err error) {
	f.mu.Lock()
	if f.validationErr == nil {
		f.validationErr = err
	}
	f.mu.Unlock()
	http.Error(w, "invalid request", http.StatusBadRequest)
}

func (f *directoryOktaServer) AssignedGroups() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.assignedGroups...)
}

func (f *directoryOktaServer) ValidationError() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.validationErr
}
