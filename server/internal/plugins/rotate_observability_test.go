package plugins_test

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func rotateObservabilityPayload(fate string) *gen.RotateObservabilityCredentialPayload {
	return &gen.RotateObservabilityCredentialPayload{
		PreviousKeyFate:  fate,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	}
}

// listHooksKeys returns the project's live (unexpired) observability hooks keys,
// newest first -- the set a rotation retires.
func listHooksKeys(t *testing.T, ctx context.Context, conn *pgxpool.Pool) []keysrepo.ApiKey {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	all, err := keysrepo.New(conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)

	var keys []keysrepo.ApiKey
	for _, key := range all {
		if !key.ProjectID.Valid || key.ProjectID.UUID != *authCtx.ProjectID {
			continue
		}
		if !strings.HasPrefix(key.Name, "plugins-hooks-") {
			continue
		}
		if key.ExpiresAt.Valid && !key.ExpiresAt.Time.After(time.Now()) {
			continue
		}
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b keysrepo.ApiKey) int {
		return b.CreatedAt.Time.Compare(a.CreatedAt.Time)
	})

	return keys
}

// keyExpiry reads the grace deadline a rotation wrote for one key.
func keyExpiry(t *testing.T, ctx context.Context, conn *pgxpool.Pool, keyHash string) time.Time {
	t.Helper()

	key, err := keysrepo.New(conn).GetAPIKeyByKeyHash(ctx, keyHash)
	require.NoError(t, err)
	require.True(t, key.ExpiresAt.Valid, "expected a grace deadline")

	return key.ExpiresAt.Time
}

func setKeyExpiry(t *testing.T, ctx context.Context, conn *pgxpool.Pool, keyHash string, expiresAt time.Time) {
	t.Helper()

	require.NoError(t, testrepo.New(conn).SetAPIKeyExpiresAtFixture(ctx, testrepo.SetAPIKeyExpiresAtFixtureParams{
		ExpiresAt: conv.ToPGTimestamptz(expiresAt),
		KeyHash:   keyHash,
	}))
}

func TestRotateObservabilityCredential_RevokeImmediately(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, _, err := ti.service.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)

	before := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, before, 1)

	createBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyCreate)
	require.NoError(t, err)
	revokeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.NoError(t, err)
	require.NotEmpty(t, result.Key)
	require.True(t, strings.HasPrefix(result.Key, result.KeyPrefix))
	require.Equal(t, "revoke_immediately", result.PreviousKeyFate)
	require.Len(t, result.PreviousKeys, 1)
	require.Equal(t, before[0].ID.String(), result.PreviousKeys[0].ID)
	require.Nil(t, result.PreviousKeysExpireAt)
	require.False(t, result.MarketplaceRepublished)
	require.NotNil(t, result.MarketplaceUpdateDeferred)
	require.False(t, *result.MarketplaceUpdateDeferred)

	after := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, after, 1)
	require.Equal(t, result.KeyPrefix, after[0].KeyPrefix)
	require.NotEqual(t, before[0].ID, after[0].ID)

	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, before[0].KeyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the revoked key must stop authenticating")

	createAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyCreate)
	require.NoError(t, err)
	revokeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)
	require.Equal(t, createBefore+1, createAfter)
	require.Equal(t, revokeBefore+1, revokeAfter)
}

func TestRotateObservabilityCredential_GraceExpiresPreviousKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, _, err := ti.service.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)

	before := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, before, 1)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.Equal(t, "grace", result.PreviousKeyFate)
	require.Len(t, result.PreviousKeys, 1)
	require.NotNil(t, result.PreviousKeysExpireAt)
	expiresAt, err := time.Parse(time.RFC3339, *result.PreviousKeysExpireAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().UTC().Add(7*24*time.Hour), expiresAt, time.Minute)

	// A scoped key carrying an expiry must keep authenticating until that expiry;
	// this is the whole point of the grace window.
	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, before[0].KeyHash)
	require.NoError(t, err, "the previous key must stay valid during the grace window")

	setKeyExpiry(t, ctx, ti.conn, before[0].KeyHash, time.Now().UTC().Add(-time.Second))

	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, before[0].KeyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the key must stop authenticating once the grace window closes")
}

func TestRotateObservabilityCredential_GraceDoesNotReviveExpiredKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, _, err := ti.service.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)

	keys := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, keys, 1)
	expired := keys[0]
	setKeyExpiry(t, ctx, ti.conn, expired.KeyHash, time.Now().UTC().Add(-time.Hour))

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.Empty(t, result.PreviousKeys, "an already-expired key is not a previous key to retire")
	require.Nil(t, result.PreviousKeysExpireAt)

	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, expired.KeyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows, "a grace rotation must not hand an expired key a new window")
}

func TestRotateObservabilityCredential_ConcurrentRotationsRetireOriginalKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, _, err := ti.service.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)

	original := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, original, 1)

	type rotation struct {
		result *gen.RotateObservabilityCredentialResult
		err    error
	}
	results := make(chan rotation, 2)
	for range 2 {
		go func() {
			result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
			results <- rotation{result: result, err: err}
		}()
	}

	minted := make([]string, 0, 2)
	for range 2 {
		got := <-results
		require.NoError(t, got.err)
		require.NotEmpty(t, got.result.Key)
		minted = append(minted, got.result.Key)
	}

	// Each rotation retires only the keys that predate its own cutoff, so it can
	// never revoke a replacement minted after that cutoff. Two rotations that
	// genuinely overlap therefore both survive; two that serialize end with the
	// later one winning, which is ordinary last-writer-wins. What must never
	// happen is the pair cancelling out and leaving the project with no usable
	// hooks credential.
	surviving := 0
	for _, key := range minted {
		hash, err := auth.GetAPIKeyHash(key)
		require.NoError(t, err)
		if _, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, hash); err == nil {
			surviving++
		}
	}
	require.NotZero(t, surviving, "overlapping rotations must not revoke every replacement")

	// The credential that existed before either rotation must be gone.
	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, original[0].KeyHash)
	require.ErrorIs(t, err, pgx.ErrNoRows, "overlapping rotations must not leave the pre-rotation key valid")

	require.Len(t, listHooksKeys(t, ctx, ti.conn), surviving)
}

func TestRotateObservabilityCredential_GraceDoesNotExtendAnOpenWindow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, _, err := ti.service.DownloadObservabilityPlugin(ctx, &gen.DownloadObservabilityPluginPayload{Platform: "claude"})
	require.NoError(t, err)

	original := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, original, 1)

	first, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.NotNil(t, first.PreviousKeysExpireAt)
	firstDeadline, err := time.Parse(time.RFC3339, *first.PreviousKeysExpireAt)
	require.NoError(t, err)

	// Rotating again must not push the original key's deadline further out;
	// otherwise repeated rotations keep one credential alive indefinitely.
	second, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.NotEmpty(t, second.PreviousKeys)

	deadline := keyExpiry(t, ctx, ti.conn, original[0].KeyHash)
	require.WithinDuration(t, firstDeadline, deadline, time.Second,
		"the original key must keep the deadline its first grace rotation set")
}

func TestRotateObservabilityCredential_RejectsDisabledObservability(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	disabled := false
	_, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{
		MarketplaceName:      nil,
		ObservabilityEnabled: &disabled,
		SessionToken:         nil,
		ProjectSlugInput:     nil,
	})
	require.NoError(t, err)

	_, err = ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)

	require.Empty(t, listHooksKeys(t, ctx, ti.conn), "a rejected rotation must not mint a credential")
}

func TestRotateObservabilityCredential_RefusesWhenPublishedMCPCannotBeCarried(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	features := &feature.InMemory{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	features.SetFlagPayload(feature.FlagHooksRollout, authCtx.ActiveOrganizationID, []byte(`{"version": 9999}`))

	publishTestObservabilityProject(t, ctx, ti, "rotate-uncarriable")

	keysBefore, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	mcpCountBefore := countPluginMCPKeys(keysBefore)

	// The published repo can no longer be read, so the MCP component cannot be
	// carried. Regenerating it would mint a replacement consumer key and rewrite
	// the packages customers installed, so the rotation must refuse instead.
	mock.lastPushedFiles = nil

	_, err = ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeFailedPrecondition, oopsErr.Code)

	keysAfter, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, mcpCountBefore, countPluginMCPKeys(keysAfter), "a refused rotation must not mint a consumer key")
}

func TestRotateObservabilityCredential_ForbiddenWithoutOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	ctx = authz.GrantsToContext(ctx, nil)

	_, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestRotateObservabilityCredential_InvalidFate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	_, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("never"))
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestRotateObservabilityCredential_NoPreviousKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.NoError(t, err)
	require.Empty(t, result.PreviousKeys)
	require.NotEmpty(t, result.Key)
	require.NotEmpty(t, result.KeyPrefix)
}

func TestRotateObservabilityCredential_DoesNotTouchConsumerKeys(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	publishTestObservabilityProject(t, ctx, ti, "rotate-consumer")

	keysBefore, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	var mcpKeyID string
	for _, key := range keysBefore {
		if strings.HasPrefix(key.Name, "plugins-mcp-") {
			mcpKeyID = key.ID.String()
		}
	}
	require.NotEmpty(t, mcpKeyID)
	mcpCountBefore := countPluginMCPKeys(keysBefore)

	_, err = ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.NoError(t, err)

	keysAfter, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	var mcpStillPresent bool
	for _, key := range keysAfter {
		if key.ID.String() == mcpKeyID {
			mcpStillPresent = true
		}
	}
	require.True(t, mcpStillPresent, "consumer MCP keys must survive observability rotation")
	require.Equal(t, mcpCountBefore, countPluginMCPKeys(keysAfter), "rotation must not mint a replacement consumer MCP key")
}

func TestRotateObservabilityCredential_RepublishesMarketplaceWhenEligible(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	features := &feature.InMemory{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	features.SetFlagPayload(feature.FlagHooksRollout, authCtx.ActiveOrganizationID, []byte(`{"version": 9999}`))

	publishTestObservabilityProject(t, ctx, ti, "rotate-publish")

	claudeObservability, _ := orgObservabilitySlugs(t, ctx, ti)
	hooksBefore := publishedHooksAPIKey(t, mock, claudeObservability)
	require.NotEmpty(t, hooksBefore)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.True(t, result.MarketplaceRepublished)
	require.NotNil(t, result.MarketplaceUpdateDeferred)
	require.False(t, *result.MarketplaceUpdateDeferred)
	require.Contains(t, result.Key, result.KeyPrefix)

	hooksAfter := publishedHooksAPIKey(t, mock, claudeObservability)
	require.Equal(t, result.Key, hooksAfter, "the republished plugin must embed the returned credential")
	require.NotEqual(t, hooksBefore, hooksAfter)
}

func TestRotateObservabilityCredential_DefersMarketplaceWhenGated(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	publishTestObservabilityProject(t, ctx, ti, "rotate-gated")

	claudeObservability, _ := orgObservabilitySlugs(t, ctx, ti)
	hooksBefore := publishedHooksAPIKey(t, mock, claudeObservability)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.False(t, result.MarketplaceRepublished)
	require.NotNil(t, result.MarketplaceUpdateDeferred)
	require.True(t, *result.MarketplaceUpdateDeferred, "a published marketplace that kept the old credential must be reported")
	require.NotEqual(t, hooksBefore, result.Key)

	require.Equal(t, hooksBefore, publishedHooksAPIKey(t, mock, claudeObservability),
		"a gated org's marketplace must keep the previous credential")
}

// A hooks-only rotation carries the MCP component, so an MCP change made since
// the last publish must still be pending afterwards -- recording the live
// fingerprints would let the next publish skip that change.
func TestRotateObservabilityCredential_KeepsPendingMCPChangePending(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	features := &feature.InMemory{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	features.SetFlagPayload(feature.FlagHooksRollout, authCtx.ActiveOrganizationID, []byte(`{"version": 9999}`))

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "rotate-pending"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "rotate-pending-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("rotate-pending server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	distributeTestSkill(t, ctx, ti, plugin.ID, "rotate-pending-skill")

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	// Change the plugin set without republishing.
	secondToolset := createTestToolset(t, ctx, ti.conn, "rotate-pending-toolset-2")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(secondToolset.ID.String()),
		DisplayName: conv.PtrEmpty("rotate-pending second server"),
		Policy:      "required",
		SortOrder:   1,
	})
	require.NoError(t, err)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("grace"))
	require.NoError(t, err)
	require.True(t, result.MarketplaceRepublished)

	status, err := ti.service.GetPublishStatus(ctx, &gen.GetPublishStatusPayload{})
	require.NoError(t, err)
	require.NotNil(t, status.UpToDate)
	require.False(t, *status.UpToDate, "the MCP change made before the rotation must still be pending")
}

// flakyRolloutProvider clears the hooks rollout for the first N payload reads
// and withholds it afterwards, which is what a flag flip between the handler's
// eligibility check and publishProject's re-read looks like.
type flakyRolloutProvider struct {
	mu               sync.Mutex
	clearedResponses int
	payload          []byte
}

// clearNext clears the rollout for the next n payload reads and withholds it
// after that, so a test can say exactly which read sees clearance.
func (p *flakyRolloutProvider) clearNext(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.clearedResponses = n
}

func (p *flakyRolloutProvider) FlagPayload(_ context.Context, _ feature.Flag, _ string, _ map[string]string) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.clearedResponses <= 0 {
		return nil, nil
	}
	p.clearedResponses--

	return p.payload, nil
}

func (p *flakyRolloutProvider) IsFlagEnabled(_ context.Context, _ feature.Flag, _ string, _ map[string]string) (bool, error) {
	return false, nil
}

func (p *flakyRolloutProvider) IsFlagEnabledLocal(_ context.Context, _ feature.Flag, _ string, _, _ map[string]string) (bool, error) {
	return false, nil
}

// A rotation the publish path declines after this handler's own gate check must
// still persist the credential it already handed back, or retiring the previous
// keys would leave the project with nothing that authenticates.
func TestRotateObservabilityCredential_PersistsKeyWhenPublishDeclines(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	features := &flakyRolloutProvider{payload: []byte(`{"version": 9999}`), clearedResponses: 0, mu: sync.Mutex{}}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)

	features.clearNext(math.MaxInt)
	publishTestObservabilityProject(t, ctx, ti, "rotate-declined")

	// Rewind the published hooks version so the org now has an upgrade pending:
	// the gate holds an uncleared org at its published version, which is what
	// makes publishProject decline a rotation.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rewindPublishedHooksVersion(t, ctx, ti.conn, *authCtx.ProjectID, "0")

	// Exactly one more cleared read: the rotation handler's own eligibility
	// check. publishProject's re-read then comes back uncleared, which is what a
	// flag flip between the two reads looks like.
	features.clearNext(1)

	claudeObservability, _ := orgObservabilitySlugs(t, ctx, ti)
	publishedBefore := publishedHooksAPIKey(t, mock, claudeObservability)

	result, err := ti.service.RotateObservabilityCredential(ctx, rotateObservabilityPayload("revoke_immediately"))
	require.NoError(t, err)
	require.False(t, result.MarketplaceRepublished)
	require.NotNil(t, result.MarketplaceUpdateDeferred)
	require.True(t, *result.MarketplaceUpdateDeferred)

	// The marketplace keeps the previous credential, as the gate demands.
	require.Equal(t, publishedBefore, publishedHooksAPIKey(t, mock, claudeObservability))

	// The key handed to the caller must authenticate even though it never
	// reached the marketplace.
	hash, err := auth.GetAPIKeyHash(result.Key)
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, hash)
	require.NoError(t, err, "a declined publish must still persist the credential it returned")

	live := listHooksKeys(t, ctx, ti.conn)
	require.Len(t, live, 1)
	require.Equal(t, result.KeyPrefix, live[0].KeyPrefix)
}

// publishTestObservabilityProject gives the project a plugin, a skill, and a
// published marketplace, which is the state a credential rotation acts on.
func publishTestObservabilityProject(t *testing.T, ctx context.Context, ti *testInstance, name string) {
	t.Helper()

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: name})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, name+"-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty(name + " server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)
	distributeTestSkill(t, ctx, ti, plugin.ID, name+"-skill")

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
}

func publishedHooksAPIKey(t *testing.T, mock *mockGitHubPublisher, observabilitySlug string) string {
	t.Helper()

	raw, ok := mock.lastPushedFiles[observabilitySlug+"/speakeasy.json"]
	require.True(t, ok, "published marketplace must contain the observability plugin config")

	var config struct {
		HooksAPIKey string `json:"hooks_api_key"`
	}
	require.NoError(t, json.Unmarshal(raw, &config))

	return config.HooksAPIKey
}

func countPluginMCPKeys(keys []keysrepo.ApiKey) int {
	count := 0
	for _, key := range keys {
		if strings.HasPrefix(key.Name, "plugins-mcp-") {
			count++
		}
	}

	return count
}
