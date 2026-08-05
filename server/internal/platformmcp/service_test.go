package platformmcp

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/platform_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
)

func TestLifecycleResult(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 5, 12, 30, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		admission Admission
		lifecycle Lifecycle
		want      string
	}{
		{name: "default project missing", admission: AdmissionEnabled, want: "default_project_missing"},
		{name: "marketplace unpublished", admission: AdmissionEnabled, lifecycle: Lifecycle{DefaultProjectID: "project"}, want: "marketplace_unpublished"},
		{name: "indeterminate", admission: AdmissionIndeterminate, lifecycle: Lifecycle{DefaultProjectID: "project", MarketplacePublished: true}, want: "status_indeterminate"},
		{name: "disabled", admission: AdmissionDisabled, lifecycle: Lifecycle{DefaultProjectID: "project", MarketplacePublished: true}, want: "gate_disabled"},
		{name: "eligible", admission: AdmissionEnabled, lifecycle: Lifecycle{DefaultProjectID: "project", MarketplacePublished: true}, want: "eligible"},
		{name: "awaiting discovery", admission: AdmissionEnabled, lifecycle: Lifecycle{DefaultProjectID: "project", MarketplacePublished: true, Connections: []LifecycleConnection{{ID: "connection", AuthorizedAt: &now}}}, want: "authorized_awaiting_discovery"},
		{name: "ready takes precedence", admission: AdmissionEnabled, lifecycle: Lifecycle{Connections: []LifecycleConnection{{ID: "awaiting"}, {ID: "ready", Ready: true}}}, want: "ready"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := lifecycleResult(test.admission, test.lifecycle)

			require.Equal(t, test.want, result.ReasonCode)
			require.Equal(t, admissionString(test.admission), result.Admission)
			require.Equal(t, len(test.lifecycle.Connections) > 0, result.Authorized)
			require.Len(t, result.Connections, len(test.lifecycle.Connections))
		})
	}
}

func TestServiceRevokeConnection(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name          string
		authorizerErr error
		revokeErr     error
		wantCode      oops.Code
		wantCalled    bool
	}{
		{name: "live admin can revoke", wantCalled: true},
		{name: "non admin forbidden", authorizerErr: ErrForbidden, wantCode: oops.CodeForbidden},
		{name: "other organization connection is not found", revokeErr: platformoauth.ErrNotFound, wantCode: oops.CodeNotFound, wantCalled: true},
		{name: "already revoked connection is not found", revokeErr: platformoauth.ErrRevoked, wantCode: oops.CodeNotFound, wantCalled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := &testLifecycleStore{revokeErr: test.revokeErr}
			service := &Service{authorizer: &testServiceAuthorizer{err: test.authorizerErr}, lifecycle: store}
			ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "organization", UserID: "user"})

			err := service.RevokeConnection(ctx, &gen.RevokeConnectionPayload{ConnectionID: "connection"})

			if test.wantCode == "" {
				require.NoError(t, err)
			} else {
				var shareable *oops.ShareableError
				require.ErrorAs(t, err, &shareable)
				require.Equal(t, test.wantCode, shareable.Code)
			}
			require.Equal(t, test.wantCalled, store.revokeCalled)
			if test.wantCalled {
				require.Equal(t, "organization", store.organizationID)
				require.Equal(t, "connection", store.connectionID)
			}
		})
	}
}

func TestServiceGetLifecycleDoesNotExposeCredentials(t *testing.T) {
	t.Parallel()

	authorizedAt := time.Date(2026, 8, 5, 12, 30, 0, 0, time.UTC)
	store := &testLifecycleStore{lifecycle: Lifecycle{
		DefaultProjectID:     "project",
		MarketplacePublished: true,
		Connections: []LifecycleConnection{
			{ID: "connection-one", AuthorizedAt: &authorizedAt, Ready: true},
			{ID: "connection-two"},
		},
	}}
	service := &Service{
		logger:     discardLogger(),
		authorizer: &testServiceAuthorizer{},
		lifecycle:  store,
		admission:  testAdmissionEvaluator{admission: AdmissionEnabled},
	}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "organization", OrganizationSlug: "organization", UserID: "user"})

	result, err := service.GetLifecycle(ctx, &gen.GetLifecyclePayload{})

	require.NoError(t, err)
	require.Equal(t, "ready", result.ReasonCode)
	require.True(t, result.Authorized)
	require.True(t, result.Ready)
	require.Len(t, result.Connections, 2)
	require.Equal(t, "connection-one", result.Connections[0].ID)
	require.Equal(t, authorizedAt.Format(time.RFC3339), *result.Connections[0].AuthorizedAt)
	require.Empty(t, result.Connections[0].ReauthorizedAt)
	require.Equal(t, "connection-two", result.Connections[1].ID)
}

type testLifecycleStore struct {
	lifecycle      Lifecycle
	getErr         error
	revokeErr      error
	revokeCalled   bool
	organizationID string
	connectionID   string
}

func (s *testLifecycleStore) GetLifecycle(_ context.Context, organizationID string) (Lifecycle, error) {
	s.organizationID = organizationID
	return s.lifecycle, s.getErr
}

func (s *testLifecycleStore) RevokeConnection(_ context.Context, organizationID, connectionID string, _ time.Time) error {
	s.revokeCalled = true
	s.organizationID = organizationID
	s.connectionID = connectionID
	return s.revokeErr
}

type testServiceAuthorizer struct {
	err error
}

func (a *testServiceAuthorizer) RequireLiveOrgAdmin(_ context.Context, _ Principal) error {
	return a.err
}

type testAdmissionEvaluator struct {
	admission Admission
	err       error
}

func (e testAdmissionEvaluator) Evaluate(_ context.Context, _, _ string) (Admission, error) {
	return e.admission, e.err
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

var _ lifecycleStore = (*testLifecycleStore)(nil)
var _ admissionEvaluator = testAdmissionEvaluator{}
var _ Authorizer = (*testServiceAuthorizer)(nil)
