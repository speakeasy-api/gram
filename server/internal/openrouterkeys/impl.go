// Package openrouterkeys implements the adminOpenRouterKeys service: the
// platform-admin surface over openrouter_api_keys, the per-(organization, key
// type) platform OpenRouter keys that pay for completions. It exposes each
// key's encryption state plus the enable/disable and encrypt-at-rest
// remediation actions. Key material never leaves the server: handlers resolve
// it only to call OpenRouter and never place it in a response or audit entry.
package openrouterkeys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/admin_open_router_keys"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin_open_router_keys/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background/activities/keybillinglock"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/openrouterkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	auth        *auth.Auth
	audit       *audit.Logger
	enc         *encryption.Client
	provisioner openrouter.Provisioner
}

const keyBillingLockWaitTimeout = 5 * time.Second

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	provisioner openrouter.Provisioner,
	enc *encryption.Client,
) *Service {
	logger = logger.With(attr.SlogComponent("openrouterkeys.api"))
	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/openrouterkeys"),
		logger:      logger,
		db:          db,
		auth:        auth.New(logger, db, sessions, authzEngine),
		audit:       auditLogger,
		enc:         enc,
		provisioner: provisioner,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// requirePlatformAdmin extracts the auth context and enforces the
// platform-admin flag. Unlike org-scoped admin services, no active
// organization is required: this surface spans every organization's keys.
func (s *Service) requirePlatformAdmin(ctx context.Context) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogUserID(authCtx.UserID))

	if !authCtx.IsAdmin {
		// Not-found rather than forbidden so a non-admin probe cannot confirm
		// the admin surface exists. The error log keeps the attempt visible to
		// operators.
		return nil, logger, oops.E(oops.CodeNotFound, nil, "not found").LogError(ctx, logger)
	}

	return authCtx, logger, nil
}

func (s *Service) ListKeys(ctx context.Context, _ *gen.ListKeysPayload) (*gen.ListKeysResult, error) {
	_, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListOpenRouterAPIKeysForAdmin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list openrouter keys").LogError(ctx, logger)
	}

	keys := make([]*gen.AdminOpenRouterKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, &gen.AdminOpenRouterKey{
			OrganizationID:   row.OrganizationID,
			OrganizationName: row.OrganizationName,
			OrganizationSlug: row.OrganizationSlug,
			GramAccountType:  row.GramAccountType,
			KeyType:          row.KeyType,
			MonthlyCredits:   row.MonthlyCredits,
			Disabled:         row.Disabled,
			EncryptionStatus: encryptionStatus(row.HasPlaintext, row.HasEncrypted),
			CreatedAt:        row.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:        row.UpdatedAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListKeysResult{Keys: keys}, nil
}

func (s *Service) GetKeyUsage(ctx context.Context, payload *gen.GetKeyUsagePayload) (*gen.GetKeyUsageResult, error) {
	_, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogOrganizationID(payload.OrganizationID), attr.SlogOpenRouterKeyType(payload.KeyType))

	row, err := orrepo.New(s.db).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: payload.OrganizationID,
		KeyType:        payload.KeyType,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "no openrouter key of this type for the organization")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "read openrouter key").LogError(ctx, logger)
	}

	if row.Disabled {
		return nil, oops.E(oops.CodeInvalid, nil, "key is disabled; usage is not polled")
	}

	apiKey, err := s.keyMaterial(row)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve openrouter key material").LogError(ctx, logger)
	}

	used, upstreamLimit, err := s.provisioner.GetKeyUsage(ctx, apiKey)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "fetch openrouter key usage").LogError(ctx, logger)
	}

	return &gen.GetKeyUsageResult{
		CreditsUsed:    used,
		MonthlyCredits: row.MonthlyCredits,
		UpstreamLimit:  upstreamLimit,
	}, nil
}

// EncryptKey is the remediation scrub for a key stored in plaintext: it
// records the AES-256-GCM ciphertext and clears the plaintext column in a
// single statement, after verifying the ciphertext decrypts back to the exact
// plaintext. OpenRouter only returns key material at creation, so the
// verification runs before anything destructive. The provisioning advisory
// lock serializes the scrub against concurrent first-time provisioning and
// the lazy read-repair path.
func (s *Service) EncryptKey(ctx context.Context, payload *gen.EncryptKeyPayload) (*gen.AdminOpenRouterKey, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogOrganizationID(payload.OrganizationID), attr.SlogOpenRouterKeyType(payload.KeyType))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin openrouter key encryption").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txq := orrepo.New(dbtx)
	if err := txq.LockOpenRouterKeyProvisioning(ctx, orrepo.LockOpenRouterKeyProvisioningParams{
		OrganizationID: payload.OrganizationID,
		KeyType:        payload.KeyType,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock openrouter key").LogError(ctx, logger)
	}

	row, err := txq.GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: payload.OrganizationID,
		KeyType:        payload.KeyType,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "no openrouter key of this type for the organization")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "read openrouter key").LogError(ctx, logger)
	}

	if !row.Key.Valid || row.Key.String == "" {
		if !row.KeyEncrypted.Valid {
			return nil, oops.E(oops.CodeUnexpected, nil, "key row holds no key material").LogError(ctx, logger)
		}
		// Already scrubbed; nothing to do.
		return s.adminKeyView(ctx, logger, payload.OrganizationID, payload.KeyType)
	}

	// Prefer a ciphertext already written by creation dual-writes or lazy
	// read-repair; otherwise mint one now. Either way, prove it decrypts back
	// to the exact plaintext before the plaintext is destroyed.
	ciphertext := row.KeyEncrypted.String
	if !row.KeyEncrypted.Valid {
		ciphertext, err = s.enc.Encrypt([]byte(row.Key.String))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "encrypt openrouter key").LogError(ctx, logger)
		}
	}
	decrypted, err := s.enc.Decrypt(ciphertext)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "verify encrypted openrouter key").LogError(ctx, logger)
	}
	if decrypted != row.Key.String {
		return nil, oops.E(oops.CodeUnexpected, nil, "encrypted openrouter key does not match the stored plaintext").LogError(ctx, logger)
	}

	if _, err := txq.SetOpenRouterKeyEncrypted(ctx, orrepo.SetOpenRouterKeyEncryptedParams{
		OrganizationID: payload.OrganizationID,
		KeyType:        payload.KeyType,
		KeyEncrypted:   conv.ToPGText(ciphertext),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set openrouter key encrypted").LogError(ctx, logger)
	}

	// Deliberately not audit logged: audit entries are exposed to the owning
	// organization through the auditlogs API, and this scrub is internal
	// storage hygiene with no customer-observable effect. The structured log
	// below keeps an operator trail.
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit openrouter key encryption").LogError(ctx, logger)
	}
	logger.InfoContext(ctx, "encrypted openrouter key at rest",
		attr.SlogUserID(authCtx.UserID),
	)

	return s.adminKeyView(ctx, logger, payload.OrganizationID, payload.KeyType)
}

func (s *Service) DisableKey(ctx context.Context, payload *gen.DisableKeyPayload) (*gen.AdminOpenRouterKey, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogOrganizationID(payload.OrganizationID), attr.SlogOpenRouterKeyType(payload.KeyType))

	changed := false
	err = keybillinglock.WithAcquireTimeout(ctx, logger, s.db, payload.OrganizationID, openrouter.KeyType(payload.KeyType), keyBillingLockWaitTimeout, func(conn *pgxpool.Conn) error {
		// DisableAPIKey treats a missing key as a no-op, but the admin surface
		// should 404 instead of pretending success.
		row, err := repo.New(conn).GetOpenRouterAPIKeyForAdmin(ctx, repo.GetOpenRouterAPIKeyForAdminParams{
			OrganizationID: payload.OrganizationID,
			KeyType:        payload.KeyType,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return oops.E(oops.CodeNotFound, err, "no openrouter key of this type for the organization")
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "read openrouter key").LogError(ctx, logger)
		case row.Disabled:
			return nil
		}

		if err := s.provisioner.DisableAPIKey(ctx, payload.OrganizationID, openrouter.KeyType(payload.KeyType)); err != nil {
			return oops.E(oops.CodeUnexpected, err, "disable openrouter key").LogError(ctx, logger)
		}
		changed = true
		return nil
	})
	if err != nil {
		var shareable *oops.ShareableError
		if errors.As(err, &shareable) {
			return nil, shareable
		}
		if errors.Is(err, keybillinglock.ErrAcquireTimeout) {
			return nil, oops.E(oops.CodeUnavailable, err, "another billing operation is in progress; retry shortly").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock openrouter key for disable").LogError(ctx, logger)
	}
	if !changed {
		return s.adminKeyView(ctx, logger, payload.OrganizationID, payload.KeyType)
	}

	if err := s.logKeyAction(ctx, authCtx, payload.OrganizationID, payload.KeyType, audit.ActionOpenRouterAPIKeyDisable); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log openrouter key disable").LogError(ctx, logger)
	}

	return s.adminKeyView(ctx, logger, payload.OrganizationID, payload.KeyType)
}

func (s *Service) EnableKey(ctx context.Context, payload *gen.EnableKeyPayload) (*gen.AdminOpenRouterKey, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogOrganizationID(payload.OrganizationID), attr.SlogOpenRouterKeyType(payload.KeyType))

	changed := false
	err = keybillinglock.WithAcquireTimeout(ctx, logger, s.db, payload.OrganizationID, openrouter.KeyType(payload.KeyType), keyBillingLockWaitTimeout, func(conn *pgxpool.Conn) error {
		row, err := repo.New(conn).GetOpenRouterAPIKeyForAdmin(ctx, repo.GetOpenRouterAPIKeyForAdminParams{
			OrganizationID: payload.OrganizationID,
			KeyType:        payload.KeyType,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return oops.E(oops.CodeNotFound, err, "no openrouter key of this type for the organization")
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "read openrouter key").LogError(ctx, logger)
		case !row.Disabled:
			return nil
		}

		// Keep a recorded ceiling. Legacy zero rows resolve to the current
		// account policy before the explicit refresh.
		limit := int(row.MonthlyCredits)
		if limit == 0 {
			activeTrial := false
			if !openrouter.IsSpecialLimitOrg(row.OrganizationID) {
				_, trialErr := trialsrepo.New(conn).GetActiveTrial(ctx, row.OrganizationID)
				switch {
				case trialErr == nil:
					activeTrial = true
				case !errors.Is(trialErr, pgx.ErrNoRows):
					logger.WarnContext(ctx, "error reading active trial; using the account type credit limit",
						attr.SlogError(trialErr),
					)
				}
			}

			var ok bool
			limit, ok = openrouter.DefaultCreditLimit(row.OrganizationID, billing.Tier(row.GramAccountType), activeTrial)
			if !ok {
				return oops.E(oops.CodeUnexpected, fmt.Errorf("no OpenRouter credit policy for account type %q", row.GramAccountType), "enable openrouter key").LogError(ctx, logger)
			}
		}
		if _, err := s.provisioner.RefreshAPIKeyLimit(ctx, payload.OrganizationID, openrouter.KeyType(payload.KeyType), &limit); err != nil {
			return oops.E(oops.CodeUnexpected, err, "enable openrouter key").LogError(ctx, logger)
		}
		changed = true
		return nil
	})
	if err != nil {
		var shareable *oops.ShareableError
		if errors.As(err, &shareable) {
			return nil, shareable
		}
		if errors.Is(err, keybillinglock.ErrAcquireTimeout) {
			return nil, oops.E(oops.CodeUnavailable, err, "another billing operation is in progress; retry shortly").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock openrouter key for enable").LogError(ctx, logger)
	}
	if !changed {
		return s.adminKeyView(ctx, logger, payload.OrganizationID, payload.KeyType)
	}

	if err := s.logKeyAction(ctx, authCtx, payload.OrganizationID, payload.KeyType, audit.ActionOpenRouterAPIKeyEnable); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log openrouter key enable").LogError(ctx, logger)
	}

	return s.adminKeyView(ctx, logger, payload.OrganizationID, payload.KeyType)
}

// keyMaterial resolves the usable API key from a row, preferring the
// encrypted column. Unlike the openrouter package's provisioning path this
// performs no read-repair: admin reads stay side-effect free.
func (s *Service) keyMaterial(row orrepo.OpenrouterApiKey) (string, error) {
	if row.KeyEncrypted.Valid {
		plaintext, err := s.enc.Decrypt(row.KeyEncrypted.String)
		if err != nil {
			return "", fmt.Errorf("decrypt openrouter key: %w", err)
		}
		return plaintext, nil
	}
	if row.Key.String == "" {
		return "", errors.New("key row holds no key material")
	}
	return row.Key.String, nil
}

// logKeyAction writes the audit entry for the upstream enable/disable
// actions. Those PATCH OpenRouter outside any local transaction, so the entry
// commits in its own transaction once the action has already succeeded.
func (s *Service) logKeyAction(ctx context.Context, authCtx *contextvalues.AuthContext, organizationID string, keyType string, action audit.Action) error {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin audit transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	keyURN := urn.NewOpenRouterAPIKey(organizationID, keyType)

	switch action {
	case audit.ActionOpenRouterAPIKeyDisable:
		err = s.audit.LogOpenRouterAPIKeyDisable(ctx, dbtx, audit.LogOpenRouterAPIKeyDisableEvent{
			OrganizationID:      organizationID,
			Actor:               actor,
			ActorDisplayName:    authCtx.Email,
			ActorSlug:           nil,
			OpenRouterAPIKeyURN: keyURN,
			KeyType:             keyType,
		})
	case audit.ActionOpenRouterAPIKeyEnable:
		err = s.audit.LogOpenRouterAPIKeyEnable(ctx, dbtx, audit.LogOpenRouterAPIKeyEnableEvent{
			OrganizationID:      organizationID,
			Actor:               actor,
			ActorDisplayName:    authCtx.Email,
			ActorSlug:           nil,
			OpenRouterAPIKeyURN: keyURN,
			KeyType:             keyType,
		})
	default:
		return fmt.Errorf("unsupported openrouter key audit action %q", action)
	}
	if err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit audit transaction: %w", err)
	}
	return nil
}

func (s *Service) adminKeyView(ctx context.Context, logger *slog.Logger, organizationID string, keyType string) (*gen.AdminOpenRouterKey, error) {
	row, err := repo.New(s.db).GetOpenRouterAPIKeyForAdmin(ctx, repo.GetOpenRouterAPIKeyForAdminParams{
		OrganizationID: organizationID,
		KeyType:        keyType,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "reload openrouter key").LogError(ctx, logger)
	}

	return &gen.AdminOpenRouterKey{
		OrganizationID:   row.OrganizationID,
		OrganizationName: row.OrganizationName,
		OrganizationSlug: row.OrganizationSlug,
		GramAccountType:  row.GramAccountType,
		KeyType:          row.KeyType,
		MonthlyCredits:   row.MonthlyCredits,
		Disabled:         row.Disabled,
		EncryptionStatus: encryptionStatus(row.HasPlaintext, row.HasEncrypted),
		CreatedAt:        row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:        row.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

// encryptionStatus projects the two column-presence flags into the API's
// three-state enum. Distinguishing "encrypted with plaintext retained" from
// fully scrubbed keeps a plaintext resurrection (e.g. a stale writer
// re-filling the column) visible on the admin page.
func encryptionStatus(hasPlaintext, hasEncrypted bool) string {
	switch {
	case hasEncrypted && !hasPlaintext:
		return "encrypted"
	case hasEncrypted && hasPlaintext:
		return "encrypted_with_plaintext"
	default:
		return "plaintext"
	}
}
