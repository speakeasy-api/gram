package auth_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
)

type orgCreatedLabels struct {
	source           string
	accountType      string
	emailDomainClass string
}

func newOrgCreationReader(t *testing.T) (*orgprovision.CreationRecorder, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	return orgprovision.NewCreationRecorder(provider), reader
}

func collectOrgsCreated(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader) map[orgCreatedLabels]int64 {
	t.Helper()

	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &metrics))
	counts := map[orgCreatedLabels]int64{}
	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "gram.organizations.created" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				source, _ := point.Attributes.Value(attr.OrganizationCreatedSourceKey)
				accountType, _ := point.Attributes.Value(attr.OrganizationCreatedAccountTypeKey)
				class, _ := point.Attributes.Value(attr.OrganizationCreatedEmailDomainClassKey)
				counts[orgCreatedLabels{source: source.AsString(), accountType: accountType.AsString(), emailDomainClass: class.AsString()}] += point.Value
			}
		}
	}
	return counts
}

func TestRegister_CountsCreatedOrganization(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	userInfo.Email = "counted@gmail.com"
	userInfo.Organizations = []MockOrganizationEntry{}
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)
	recorder, reader := newOrgCreationReader(t)
	instance.service.SetOrganizationCreationRecorder(recorder)

	session := sessions.Session{
		SessionID:            t.Name(),
		UserID:               userInfo.UserID,
		ActiveOrganizationID: "",
		WorkOSSessionID:      "",
		ImpersonatorEmail:    "",
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))
	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: "",
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                &userInfo.Email,
		AccountType:          "test",
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	})

	require.NoError(t, instance.service.Register(ctx, &gen.RegisterPayload{OrgName: "Counted Organization", SessionToken: nil}))

	require.Equal(t, map[orgCreatedLabels]int64{
		{source: orgprovision.SourceSignup, accountType: "free", emailDomainClass: "free_webmail"}: 1,
	}, collectOrgsCreated(t, ctx, reader))
}

func TestSwitchScopes_ExistingOrganizationIsNotCounted(t *testing.T) {
	t.Parallel()

	userInfo := speakeasyMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	recorder, reader := newOrgCreationReader(t)
	instance.service.SetOrganizationCreationRecorder(recorder)

	require.NoError(t, instance.createTestUser(ctx, userInfo))
	for _, org := range userInfo.Organizations {
		require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
	}

	session := sessions.Session{
		SessionID:            uuid.NewString(),
		UserID:               userInfo.UserID,
		ActiveOrganizationID: userInfo.Organizations[0].ID,
		WorkOSSessionID:      "",
		ImpersonatorEmail:    "",
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))
	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		AccountType:          "test",
		ProjectID:            nil,
		OrganizationSlug:     "",
		Email:                &userInfo.Email,
		ProjectSlug:          nil,
		APIKeyScopes:         nil,
	})

	// SwitchScopes upserts the selected organization's row, which already
	// exists, so the upsert updates it and must not count as a creation.
	newOrgID := userInfo.Organizations[1].ID
	_, err := instance.service.SwitchScopes(ctx, &gen.SwitchScopesPayload{OrganizationID: &newOrgID, ProjectID: nil, SessionToken: nil})
	require.NoError(t, err)

	require.Empty(t, collectOrgsCreated(t, ctx, reader))
}
