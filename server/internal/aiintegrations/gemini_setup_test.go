package aiintegrations

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/ai_integrations"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/geminivault"
)

const geminiTestMatterID = "11111111-2222-4333-8444-555555555555"

// fakeGeminiSetup records what setup asked Google for.
type fakeGeminiSetup struct {
	mu        sync.Mutex
	configs   []geminivault.Config
	ensures   int
	getMatter int
	lists     int
	ensureErr error
	getErr    error
	listErr   error
	units     []geminivault.OrgUnit
}

func (f *fakeGeminiSetup) newClient(cfg geminivault.Config) (geminiSetupClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configs = append(f.configs, cfg)
	return f, nil
}

func (f *fakeGeminiSetup) EnsureMatter(_ context.Context, name, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensures++
	if name != geminiMatterName {
		return "", errors.New("unexpected matter name")
	}
	return geminiTestMatterID, f.ensureErr
}

func (f *fakeGeminiSetup) GetMatter(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getMatter++
	return f.getErr
}

func (f *fakeGeminiSetup) ListOrgUnits(context.Context) ([]geminivault.OrgUnit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	return f.units, f.listErr
}

func (f *fakeGeminiSetup) counts() (ensures, getMatter, lists int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ensures, f.getMatter, f.lists
}

func newGeminiSetupTestService(t *testing.T) (context.Context, *Service, *fakeGeminiSetup, *fakeConfigPoller, func() int64) {
	t.Helper()
	ctx, conn, service, poller, orgID := newUpsertTestService(t, "")
	fake := &fakeGeminiSetup{units: []geminivault.OrgUnit{
		{ID: "id:root", Path: "/", Name: "Example Co", Description: ""},
		{ID: "id:sales", Path: "/Sales", Name: "Sales", Description: "Sales team"},
	}}
	service.newGeminiClient = fake.newClient
	count := func() int64 { return countAIIntegrationConfigs(t, ctx, conn, orgID, true) }
	return ctx, service, fake, poller, count
}

func geminiUpsert(apiKey string, delegatedUser, orgUnitID, matterID *string) *gen.UpsertConfigPayload {
	return &gen.UpsertConfigPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Provider:               ProviderGeminiCompliance,
		APIKey:                 apiKey,
		ExternalOrganizationID: matterID,
		DelegatedUser:          delegatedUser,
		OrgUnitID:              orgUnitID,
		BillingMode:            nil,
		Enabled:                true,
	}
}

func TestGeminiSetupSavesCredentialsThenStartsOnOrgUnitSelection(t *testing.T) {
	t.Parallel()
	ctx, service, fake, poller, _ := newGeminiSetupTestService(t)
	key := geminiTestServiceAccount(t)

	// Step 1: key and delegated admin. The matter is found or created, and the
	// connection holds until an org unit is chosen.
	saved, err := service.UpsertConfig(ctx, geminiUpsert(key, conv.PtrEmpty("admin@example.com"), nil, nil))
	require.NoError(t, err)
	require.False(t, saved.Enabled)
	require.Equal(t, geminiTestMatterID, *saved.ExternalOrganizationID)
	require.Equal(t, "admin@example.com", *saved.DelegatedUser)
	require.Nil(t, saved.OrgUnitID)
	require.Empty(t, poller.calls)
	ensures, getMatter, lists := fake.counts()
	require.Equal(t, []int{1, 0, 0}, []int{ensures, getMatter, lists})

	// The dashboard lists units with the saved credentials, never a matter.
	units, err := service.ListOrgUnits(ctx, &gen.ListOrgUnitsPayload{ApikeyToken: nil, SessionToken: nil, Provider: ProviderGeminiCompliance})
	require.NoError(t, err)
	require.Len(t, units.OrgUnits, 2)
	require.Equal(t, "/", units.OrgUnits[0].Path)
	require.Nil(t, units.OrgUnits[0].Description)
	require.Equal(t, "Sales team", *units.OrgUnits[1].Description)
	listConfig := fake.configs[len(fake.configs)-1]
	require.Equal(t, key, listConfig.ServiceAccountJSON)
	require.Equal(t, "admin@example.com", listConfig.DelegatedUser)
	require.Empty(t, listConfig.MatterID)

	// Step 2: choose a unit without resubmitting the key. Syncing starts.
	selected, err := service.UpsertConfig(ctx, geminiUpsert("", nil, conv.PtrEmpty("id:root"), nil))
	require.NoError(t, err)
	require.True(t, selected.Enabled)
	require.Equal(t, "id:root", *selected.OrgUnitID)
	require.Equal(t, "/", *selected.OrgUnitPath)
	require.Equal(t, geminiTestMatterID, *selected.ExternalOrganizationID)
	ensures, getMatter, lists = fake.counts()
	// Starting the sync rechecks the matter, which may have closed since step 1.
	require.Equal(t, []int{1, 1, 2}, []int{ensures, getMatter, lists})

	// A billing-only edit changes nothing Google cares about.
	_, err = service.UpsertConfig(ctx, &gen.UpsertConfigPayload{SessionToken: nil, ApikeyToken: nil, Provider: ProviderGeminiCompliance, APIKey: "", ExternalOrganizationID: nil, DelegatedUser: nil, OrgUnitID: nil, BillingMode: conv.PtrEmpty("flat_rate"), Enabled: true})
	require.NoError(t, err)
	ensures, getMatter, lists = fake.counts()
	require.Equal(t, []int{1, 1, 2}, []int{ensures, getMatter, lists})

	// Changing the delegated admin rechecks the matter and the unit as them.
	_, err = service.UpsertConfig(ctx, geminiUpsert("", conv.PtrEmpty("other-admin@example.com"), nil, nil))
	require.NoError(t, err)
	ensures, getMatter, lists = fake.counts()
	require.Equal(t, []int{1, 2, 3}, []int{ensures, getMatter, lists})
	require.Equal(t, "other-admin@example.com", fake.configs[len(fake.configs)-1].DelegatedUser)
	require.Equal(t, geminiTestMatterID, fake.configs[len(fake.configs)-1].MatterID)
}

func TestGeminiSetupRefusalSavesNothing(t *testing.T) {
	t.Parallel()
	ctx, service, fake, _, count := newGeminiSetupTestService(t)
	fake.ensureErr = &geminivault.HTTPError{StatusCode: 403, TokenExchange: false}

	_, err := service.UpsertConfig(ctx, geminiUpsert(geminiTestServiceAccount(t), conv.PtrEmpty("admin@example.com"), nil, nil))
	require.ErrorContains(t, err, "Vault license")
	require.Equal(t, int64(0), count())
}

func TestGeminiSetupRejectsBadInputBeforeCallingGoogle(t *testing.T) {
	t.Parallel()
	ctx, service, fake, _, count := newGeminiSetupTestService(t)
	key := geminiTestServiceAccount(t)

	_, err := service.UpsertConfig(ctx, geminiUpsert(`{"service_account":{},"delegated_user":"admin@example.com"}`, conv.PtrEmpty("admin@example.com"), nil, nil))
	require.ErrorContains(t, err, "service account JSON key file")
	_, err = service.UpsertConfig(ctx, geminiUpsert(key, nil, nil, nil))
	require.ErrorContains(t, err, "delegated_user")
	_, err = service.UpsertConfig(ctx, geminiUpsert(key, conv.PtrEmpty("admin@example.com"), nil, conv.PtrEmpty("not-a-uuid")))
	require.ErrorContains(t, err, "matter UUID")
	require.Empty(t, fake.configs)
	require.Equal(t, int64(0), count())
}

func TestGeminiSetupRejectsAnUnknownOrgUnit(t *testing.T) {
	t.Parallel()
	ctx, service, _, _, _ := newGeminiSetupTestService(t)
	_, err := service.UpsertConfig(ctx, geminiUpsert(geminiTestServiceAccount(t), conv.PtrEmpty("admin@example.com"), nil, nil))
	require.NoError(t, err)

	_, err = service.UpsertConfig(ctx, geminiUpsert("", nil, conv.PtrEmpty("id:elsewhere"), nil))
	require.ErrorContains(t, err, "organizational unit not found")
	cfg, err := service.GetConfig(ctx, &gen.GetConfigPayload{ApikeyToken: nil, SessionToken: nil, Provider: ProviderGeminiCompliance})
	require.NoError(t, err)
	require.Nil(t, cfg.OrgUnitID)
	require.False(t, cfg.Enabled)
}

func TestGeminiSetupUsesAProvidedMatter(t *testing.T) {
	t.Parallel()
	ctx, service, fake, _, _ := newGeminiSetupTestService(t)
	const provided = "99999999-8888-4777-8666-555555555555"

	saved, err := service.UpsertConfig(ctx, geminiUpsert(geminiTestServiceAccount(t), conv.PtrEmpty("admin@example.com"), conv.PtrEmpty("id:sales"), conv.PtrEmpty(provided)))
	require.NoError(t, err)
	require.Equal(t, provided, *saved.ExternalOrganizationID)
	require.Equal(t, "/Sales", *saved.OrgUnitPath)
	require.True(t, saved.Enabled)
	ensures, getMatter, lists := fake.counts()
	require.Equal(t, []int{0, 1, 1}, []int{ensures, getMatter, lists})
	require.Equal(t, provided, fake.configs[0].MatterID)
}

func TestListOrgUnitsRequiresSavedGeminiCredentials(t *testing.T) {
	t.Parallel()
	ctx, service, _, _, _ := newGeminiSetupTestService(t)
	_, err := service.ListOrgUnits(ctx, &gen.ListOrgUnitsPayload{ApikeyToken: nil, SessionToken: nil, Provider: ProviderGeminiCompliance})
	require.ErrorContains(t, err, "save the Gemini service account key")
	_, err = service.ListOrgUnits(ctx, &gen.ListOrgUnitsPayload{ApikeyToken: nil, SessionToken: nil, Provider: ProviderCursor})
	require.ErrorContains(t, err, "supports only")
}

func TestGeminiSetupErrorMessages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		step geminiSetupStep
		err  error
		code oops.Code
		want string
	}{
		{"closed matter", geminiStepVault, geminivault.ErrMatterNotOpen, oops.CodeInvalid, "not open"},
		{"bad key", geminiStepVault, &geminivault.HTTPError{StatusCode: 400, TokenExchange: true}, oops.CodeUnauthorized, "service account credentials"},
		{"directory scope not delegated", geminiStepDirectory, &geminivault.HTTPError{StatusCode: 401, TokenExchange: true}, oops.CodeUnauthorized, "admin.directory.orgunit.readonly"},
		{"vault privileges", geminiStepVault, &geminivault.HTTPError{StatusCode: 403, TokenExchange: false}, oops.CodeForbidden, "Manage Matters"},
		{"directory privileges", geminiStepDirectory, &geminivault.HTTPError{StatusCode: 403, TokenExchange: false}, oops.CodeForbidden, "read organizational units"},
		{"missing matter", geminiStepVault, &geminivault.HTTPError{StatusCode: 404, TokenExchange: false}, oops.CodeInvalid, "matter not found"},
		{"outage", geminiStepVault, &geminivault.HTTPError{StatusCode: 503, TokenExchange: false}, oops.CodeUnavailable, "try saving again"},
		{"throttled token", geminiStepVault, &geminivault.HTTPError{StatusCode: 429, TokenExchange: true}, oops.CodeUnavailable, "try saving again"},
		{"transport", geminiStepDirectory, errors.New("dial failed"), oops.CodeUnavailable, "try saving again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var shareable *oops.ShareableError
			require.ErrorAs(t, geminiSetupError(tc.step, tc.err), &shareable)
			require.Equal(t, tc.code, shareable.Code)
			require.Contains(t, shareable.Error(), tc.want)
		})
	}
}
