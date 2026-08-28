package openrouterkeys_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/openrouterkeys"
	orgmetarepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orgrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type testInstance struct {
	service     *openrouterkeys.Service
	conn        *pgxpool.Pool
	enc         *encryption.Client
	provisioner *stubProvisioner
}

// stubProvisioner stands in for the OpenRouter client. Methods that mutate
// upstream state mirror the real implementation's local writes so handlers
// can re-read the row after acting, without any upstream HTTP.
type stubProvisioner struct {
	mu sync.Mutex

	logger *slog.Logger
	conn   *pgxpool.Pool

	// usage and usageLimit are returned by GetKeyUsage.
	usage      float64
	usageLimit *int64

	usageCalls       []string
	disableCalls     []string
	refreshCalls     []string
	addCauseCalls    []string
	removeCauseCalls []string
}

var _ openrouter.Provisioner = (*stubProvisioner)(nil)

func (s *stubProvisioner) ProvisionAPIKey(ctx context.Context, orgID string, keyType openrouter.KeyType) (string, error) {
	return "", nil
}

func (s *stubProvisioner) RefreshAPIKeyLimit(ctx context.Context, orgID string, keyType openrouter.KeyType, limit *int) (int, error) {
	return s.refreshAPIKeyLimit(ctx, s.conn, orgID, keyType, limit)
}

func (s *stubProvisioner) RefreshAPIKeyLimitWithDB(ctx context.Context, db openrouter.DBTX, orgID string, keyType openrouter.KeyType, limit *int) (int, error) {
	return s.refreshAPIKeyLimit(ctx, db, orgID, keyType, limit)
}

func (s *stubProvisioner) refreshAPIKeyLimit(ctx context.Context, db openrouter.DBTX, orgID string, keyType openrouter.KeyType, limit *int) (int, error) {
	s.mu.Lock()
	s.refreshCalls = append(s.refreshCalls, orgID+"/"+string(keyType))
	s.mu.Unlock()

	keyLimit := 0
	if limit != nil {
		keyLimit = *limit
	}
	key, err := orgrepo.New(db).GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	})
	if err != nil {
		return 0, fmt.Errorf("stub refresh read: %w", err)
	}
	if _, err := orgrepo.New(db).UpdateOpenRouterKey(ctx, orgrepo.UpdateOpenRouterKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
		MonthlyCredits: int64(keyLimit),
		KeyHash:        key.KeyHash,
		Reinstate:      key.Disabled,
	}); err != nil {
		return 0, fmt.Errorf("stub refresh write: %w", err)
	}
	return keyLimit, nil
}

func (s *stubProvisioner) AddAPIKeyDisableCause(ctx context.Context, orgID string, keyType openrouter.KeyType, cause openrouter.DisableCause) (openrouter.DisableCauseChange, error) {
	return s.addAPIKeyDisableCause(ctx, s.conn, orgID, keyType, cause)
}

func (s *stubProvisioner) AddAPIKeyDisableCauseWithDB(ctx context.Context, db openrouter.DBTX, orgID string, keyType openrouter.KeyType, cause openrouter.DisableCause) (openrouter.DisableCauseChange, error) {
	return s.addAPIKeyDisableCause(ctx, db, orgID, keyType, cause)
}

func (s *stubProvisioner) addAPIKeyDisableCause(ctx context.Context, db openrouter.DBTX, orgID string, keyType openrouter.KeyType, cause openrouter.DisableCause) (openrouter.DisableCauseChange, error) {
	s.mu.Lock()
	s.addCauseCalls = append(s.addCauseCalls, orgID+"/"+string(keyType)+"/"+string(cause))
	s.mu.Unlock()

	queries := orgrepo.New(db)
	key, err := queries.GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(keyType)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return openrouter.DisableCauseChange{}, nil
	case err != nil:
		return openrouter.DisableCauseChange{}, fmt.Errorf("stub add disable cause read: %w", err)
	case key.DisableCauses == nil:
		return openrouter.DisableCauseChange{}, errors.New("cannot add OpenRouter disable cause to unclassified key")
	case slices.Contains(key.DisableCauses, string(cause)):
		return openrouter.DisableCauseChange{}, nil
	}

	accessChanged := len(key.DisableCauses) == 0
	if _, err := queries.AddOpenRouterAPIKeyDisableCause(ctx, orgrepo.AddOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: orgID, KeyType: string(keyType), KeyHash: key.KeyHash, DisableCause: string(cause),
	}); err != nil {
		return openrouter.DisableCauseChange{}, fmt.Errorf("stub add disable cause write: %w", err)
	}
	return openrouter.DisableCauseChange{CauseChanged: true, KeyAccessChanged: accessChanged}, nil
}

func (s *stubProvisioner) RemoveAPIKeyDisableCause(ctx context.Context, orgID string, keyType openrouter.KeyType, cause openrouter.DisableCause, limit *int) (int, openrouter.DisableCauseChange, error) {
	return s.removeAPIKeyDisableCause(ctx, s.conn, orgID, keyType, cause, limit)
}

func (s *stubProvisioner) RemoveAPIKeyDisableCauseWithDB(ctx context.Context, db openrouter.DBTX, orgID string, keyType openrouter.KeyType, cause openrouter.DisableCause, limit *int) (int, openrouter.DisableCauseChange, error) {
	return s.removeAPIKeyDisableCause(ctx, db, orgID, keyType, cause, limit)
}

func (s *stubProvisioner) removeAPIKeyDisableCause(ctx context.Context, db openrouter.DBTX, orgID string, keyType openrouter.KeyType, cause openrouter.DisableCause, limit *int) (int, openrouter.DisableCauseChange, error) {
	s.mu.Lock()
	s.removeCauseCalls = append(s.removeCauseCalls, orgID+"/"+string(keyType)+"/"+string(cause))
	s.mu.Unlock()

	if limit != nil && *limit <= 0 {
		return 0, openrouter.DisableCauseChange{}, errors.New("remove OpenRouter API key disable cause: monthly credits must be positive")
	}

	queries := orgrepo.New(db)
	key, err := queries.GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(keyType)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, openrouter.DisableCauseChange{}, nil
	case err != nil:
		return 0, openrouter.DisableCauseChange{}, fmt.Errorf("stub remove disable cause read: %w", err)
	case key.DisableCauses == nil:
		return 0, openrouter.DisableCauseChange{}, errors.New("cannot remove OpenRouter disable cause from unclassified key")
	case !slices.Contains(key.DisableCauses, string(cause)):
		return int(key.MonthlyCredits), openrouter.DisableCauseChange{}, nil
	}

	accessChanged := true
	for _, existingCause := range key.DisableCauses {
		if existingCause != string(cause) {
			accessChanged = false
			break
		}
	}
	keyLimit := int(key.MonthlyCredits)
	if accessChanged && limit != nil {
		keyLimit = *limit
	} else if accessChanged && key.MonthlyCredits == 0 {
		org, orgErr := orgmetarepo.New(db).GetOrganizationMetadata(ctx, orgID)
		if orgErr != nil {
			return 0, openrouter.DisableCauseChange{}, fmt.Errorf("stub remove disable cause organization read: %w", orgErr)
		}
		keyLimit, _ = openrouter.ResolveDefaultCreditLimit(ctx, s.logger, db, org.ID, billing.Tier(org.GramAccountType))
	}
	if _, err := queries.RemoveOpenRouterAPIKeyDisableCause(ctx, orgrepo.RemoveOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: orgID, KeyType: string(keyType), KeyHash: key.KeyHash, DisableCause: string(cause),
		MonthlyCredits: int64(keyLimit), UpdateMonthlyCredits: accessChanged && int64(keyLimit) != key.MonthlyCredits,
	}); err != nil {
		return 0, openrouter.DisableCauseChange{}, fmt.Errorf("stub remove disable cause write: %w", err)
	}
	return keyLimit, openrouter.DisableCauseChange{CauseChanged: true, KeyAccessChanged: accessChanged}, nil
}

func (s *stubProvisioner) ReconcileAPIKeyDisabledWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType) error {
	return nil
}

func (s *stubProvisioner) DisableAPIKey(ctx context.Context, orgID string, keyType openrouter.KeyType) error {
	s.mu.Lock()
	s.disableCalls = append(s.disableCalls, orgID+"/"+string(keyType))
	s.mu.Unlock()

	if err := orgrepo.New(s.conn).DisableOpenRouterAPIKey(ctx, orgrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	}); err != nil {
		return fmt.Errorf("stub disable write: %w", err)
	}
	return nil
}

func (s *stubProvisioner) GetCreditsUsed(ctx context.Context, orgID string, keyType openrouter.KeyType) (float64, int, error) {
	return 0, 0, nil
}

func (s *stubProvisioner) GetKeyUsage(ctx context.Context, apiKey string) (float64, *int64, error) {
	s.mu.Lock()
	s.usageCalls = append(s.usageCalls, apiKey)
	s.mu.Unlock()
	return s.usage, s.usageLimit, nil
}

func (s *stubProvisioner) ReconcileMonthlyCredits(ctx context.Context, orgID string, keyType openrouter.KeyType, currentLimit int64, currentGeneration int64, upstreamLimit *int64) (int64, error) {
	return currentLimit, nil
}

func (s *stubProvisioner) GetModelUsage(ctx context.Context, generationID string, orgID string, keyType openrouter.KeyType) (*openrouter.ModelUsage, error) {
	return nil, nil
}

func (s *stubProvisioner) UsageCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.usageCalls))
	copy(out, s.usageCalls)
	return out
}

func (s *stubProvisioner) RefreshCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.refreshCalls...)
}

func (s *stubProvisioner) AddCauseCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.addCauseCalls...)
}

func (s *stubProvisioner) RemoveCauseCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.removeCauseCalls...)
}

type provisionerFactory func(*slog.Logger, trace.TracerProvider, *pgxpool.Pool, *encryption.Client) openrouter.Provisioner

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()
	return newTestServiceWithProvisioner(t, nil)
}

func newTestServiceWithProvisioner(t *testing.T, factory provisionerFactory) (context.Context, *testInstance) {
	t.Helper()
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)

	conn, err := infra.CloneTestDatabase(t, "openrouterkeys_api")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 12)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	enc := testenv.NewEncryptionClient(t)
	stub := &stubProvisioner{
		mu:           sync.Mutex{},
		logger:       logger,
		conn:         conn,
		usage:        0,
		usageLimit:   nil,
		usageCalls:   nil,
		disableCalls: nil,
		refreshCalls: nil,
	}
	var provisioner openrouter.Provisioner = stub
	if factory != nil {
		provisioner = factory(logger, tracerProvider, conn, enc)
		stub = nil
	}

	return ctx, &testInstance{
		service:     openrouterkeys.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, audit.NewLogger(), provisioner, enc),
		conn:        conn,
		enc:         enc,
		provisioner: stub,
	}
}

// seedKey inserts an organization and one OpenRouter key row storing the
// given key material encrypted. Pass "" as the key to leave the encrypted
// column NULL, seeding a row that holds no key material.
func seedKey(t *testing.T, ctx context.Context, ti *testInstance, orgSuffix string, keyType string, key string) string {
	t.Helper()

	orgID := "org-" + orgSuffix + "-" + uuid.NewString()[:8]
	require.NoError(t, orgmetarepo.New(ti.conn).CreateOrganizationMetadata(ctx, orgmetarepo.CreateOrganizationMetadataParams{
		ID:   orgID,
		Name: "Org " + orgSuffix,
		Slug: orgID,
	}))

	var ciphertext string
	if key != "" {
		var err error
		ciphertext, err = ti.enc.Encrypt([]byte(key))
		require.NoError(t, err)
	}

	_, err := orgrepo.New(ti.conn).CreateOpenRouterAPIKey(ctx, orgrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        keyType,
		KeyEncrypted:   conv.ToPGTextEmpty(ciphertext),
		KeyHash:        "hash-" + orgSuffix,
		MonthlyCredits: 5,
	})
	require.NoError(t, err)

	return orgID
}

// withAdmin returns ctx with the auth context's IsAdmin flag flipped to true.
func withAdmin(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtxCopy := *authCtx
	authCtxCopy.IsAdmin = true
	return contextvalues.SetAuthContext(ctx, &authCtxCopy)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
