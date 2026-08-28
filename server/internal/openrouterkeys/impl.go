// Package openrouterkeys implements the adminOpenRouterKeys service: the
// platform-admin surface over openrouter_api_keys, the per-(organization, key
// type) platform OpenRouter keys that pay for completions. It exposes each
// key's credit usage plus the enable/disable actions. Key material never
// leaves the server: handlers resolve it only to call OpenRouter and never
// place it in a response or audit entry.
package openrouterkeys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/openrouterkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type lockedSessionProvisioner interface {
	AddAPIKeyDisableCauseWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType, openrouter.DisableCause) (openrouter.DisableCauseChange, error)
	RemoveAPIKeyDisableCauseWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType, openrouter.DisableCause, *int) (int, openrouter.DisableCauseChange, error)
	ReconcileAPIKeyDisabledWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType) error
}

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
			DisableCauses:    row.DisableCauses,
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

func (s *Service) DisableKey(ctx context.Context, payload *gen.DisableKeyPayload) (*gen.AdminOpenRouterKey, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogOrganizationID(payload.OrganizationID), attr.SlogOpenRouterKeyType(payload.KeyType))

	if err := s.mutateAdminLock(ctx, logger, authCtx, payload.OrganizationID, payload.KeyType, true); err != nil {
		return nil, err
	}
	return s.adminKeyView(ctx, logger, payload.OrganizationID, payload.KeyType)
}

func (s *Service) EnableKey(ctx context.Context, payload *gen.EnableKeyPayload) (*gen.AdminOpenRouterKey, error) {
	authCtx, logger, err := s.requirePlatformAdmin(ctx)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogOrganizationID(payload.OrganizationID), attr.SlogOpenRouterKeyType(payload.KeyType))

	if err := s.mutateAdminLock(ctx, logger, authCtx, payload.OrganizationID, payload.KeyType, false); err != nil {
		return nil, err
	}
	return s.adminKeyView(ctx, logger, payload.OrganizationID, payload.KeyType)
}

func (s *Service) mutateAdminLock(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, organizationID, keyType string, add bool) error {
	action := audit.ActionOpenRouterAPIKeyEnable
	operation := "enable"
	if add {
		action = audit.ActionOpenRouterAPIKeyDisable
		operation = "disable"
	}

	err := keybillinglock.WithAcquireTimeout(ctx, logger, s.db, organizationID, openrouter.KeyType(keyType), keyBillingLockWaitTimeout, func(conn *pgxpool.Conn) error {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin openrouter key %s transaction: %w", operation, err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		row, err := repo.New(tx).GetOpenRouterAPIKeyForAdmin(ctx, repo.GetOpenRouterAPIKeyForAdminParams{
			OrganizationID: organizationID,
			KeyType:        keyType,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return oops.E(oops.CodeNotFound, err, "no openrouter key of this type for the organization")
		case err != nil:
			return fmt.Errorf("read openrouter key before %s: %w", operation, err)
		case row.DisableCauses == nil:
			return fmt.Errorf("%s openrouter key: disable causes are unclassified", operation)
		}

		provisioner, ok := s.provisioner.(lockedSessionProvisioner)
		if !ok {
			return fmt.Errorf("%s openrouter key: provisioner cannot use the locked database session", operation)
		}

		hasAdminLock := slices.Contains(row.DisableCauses, string(openrouter.DisableCauseAdminLock))
		if hasAdminLock != add {
			var change openrouter.DisableCauseChange
			if add {
				change, err = provisioner.AddAPIKeyDisableCauseWithDB(ctx, tx, organizationID, openrouter.KeyType(keyType), openrouter.DisableCauseAdminLock)
			} else {
				_, change, err = provisioner.RemoveAPIKeyDisableCauseWithDB(ctx, tx, organizationID, openrouter.KeyType(keyType), openrouter.DisableCauseAdminLock, nil)
			}
			if err != nil {
				return fmt.Errorf("%s openrouter key admin lock: %w", operation, err)
			}
			if !change.CauseChanged {
				return fmt.Errorf("%s openrouter key admin lock: cause did not change after locked preflight", operation)
			}

			if err := s.logKeyAction(ctx, tx, authCtx, organizationID, keyType, action); err != nil {
				return err
			}
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit openrouter key %s transaction: %w", operation, err)
		}
		if err := provisioner.ReconcileAPIKeyDisabledWithDB(ctx, conn, organizationID, openrouter.KeyType(keyType)); err != nil {
			return fmt.Errorf("%s openrouter key after committing admin lock: %w", operation, err)
		}
		return nil
	})
	if err == nil {
		return nil
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		return shareable
	}
	if errors.Is(err, keybillinglock.ErrAcquireTimeout) {
		return oops.E(oops.CodeUnavailable, err, "another billing operation is in progress; retry shortly").LogWarn(ctx, logger)
	}
	return oops.E(oops.CodeUnexpected, err, "admin %s openrouter key", operation).LogError(ctx, logger)
}

// keyMaterial resolves the usable API key from a row by decrypting the
// encrypted column. A row without a ciphertext is a hard error.
func (s *Service) keyMaterial(row orrepo.OpenrouterApiKey) (string, error) {
	if !row.KeyEncrypted.Valid {
		return "", errors.New("key row holds no encrypted key material")
	}

	plaintext, err := s.enc.Decrypt(row.KeyEncrypted.String)
	if err != nil {
		return "", fmt.Errorf("decrypt openrouter key: %w", err)
	}

	return plaintext, nil
}

// logKeyAction writes snapshot-free audit and outbox records on the caller's
// mutation transaction so local cause state and customer-visible history are
// committed or rolled back together.
func (s *Service) logKeyAction(ctx context.Context, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, organizationID string, keyType string, action audit.Action) error {
	var err error

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
		DisableCauses:    row.DisableCauses,
		CreatedAt:        row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:        row.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}
