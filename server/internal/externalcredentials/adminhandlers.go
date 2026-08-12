package externalcredentials

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
)

// The adminExternalCredentials handlers curate platform external_credentials
// records (organization_id NULL AND project_id NULL), shared across every
// organization. No project/org exists to scope an RBAC grant, so each handler
// gates inline on the platform-admin flag; audit is structured-logs only
// (audit_log.organization_id is NOT NULL).

// logPlatformMutation records a structured-log audit line (actor, action,
// subject) for a platform credential mutation, standing in for the auditlogs
// rows platform-scoped records can't have. Call it only after the transaction
// commits so the log never claims a mutation that rolled back.
func logPlatformMutation(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, action, subject, subjectID string) {
	logger.InfoContext(ctx, "platform external credential "+subject+" "+action,
		attr.SlogAuditAction(action),
		attr.SlogAuditSubject(subject),
		attr.SlogAuditSubjectID(subjectID),
		attr.SlogAuthUserEmail(conv.PtrValOrEmpty(authCtx.Email, "")),
	)
}

// platformOrganizationID is the NULL organization_id that selects the platform
// tenancy tier in the shared external_credentials queries (organization_id IS
// NOT DISTINCT FROM the parameter). Only the platform-admin handlers pass it;
// organization handlers always pass a real organization id.
var platformOrganizationID = pgtype.Text{String: "", Valid: false}

func (s *Service) CreateGcpIamPlatformCredential(ctx context.Context, payload *adminecgen.CreateGcpIamPlatformCredentialPayload) (*adminecgen.GcpIamCredential, error) {
	authCtx, logger, err := auth.RequirePlatformAdmin(ctx, s.logger)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	cols, err := s.resolveGcpColumns(ctx, logger, gcpCredentialInput{
		impersonateServiceAccount: payload.ImpersonateServiceAccount,
		wifPoolID:                 payload.WifPoolID,
		wifProviderID:             payload.WifProviderID,
		wifProjectNumber:          payload.WifProjectNumber,
	})
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating platform external credential").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	// Platform scope: organization_id NULL (project_id defaults NULL). Reuses the
	// org insert with a NULL organization_id.
	ec, err := q.CreateExternalCredential(ctx, repo.CreateExternalCredentialParams{
		OrganizationID: platformOrganizationID,
		Provider:       "gcp_iam",
		Name:           name,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating platform external credential").LogError(ctx, logger)
	}

	gcp, err := q.CreateGcpIamCredential(ctx, repo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      ec.ID,
		ImpersonateServiceAccount: cols.ImpersonateServiceAccount,
		WifPoolID:                 cols.WifPoolID,
		WifProviderID:             cols.WifProviderID,
		WifProjectNumber:          cols.WifProjectNumber,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating platform gcp iam credential").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving platform external credential").LogError(ctx, logger)
	}

	logPlatformMutation(ctx, logger, authCtx, "create", "gcp_iam_credential", ec.ID.String())

	return mv.BuildPlatformGcpIamCredentialView(ec, gcp), nil
}

// UpdateGcpIamPlatformCredential replaces a platform GCP credential's name and
// auth configuration (full replace, like the organization update).
func (s *Service) UpdateGcpIamPlatformCredential(ctx context.Context, payload *adminecgen.UpdateGcpIamPlatformCredentialPayload) (*adminecgen.GcpIamCredential, error) {
	authCtx, logger, err := auth.RequirePlatformAdmin(ctx, s.logger)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	cols, err := s.resolveGcpColumns(ctx, logger, gcpCredentialInput{
		impersonateServiceAccount: payload.ImpersonateServiceAccount,
		wifPoolID:                 payload.WifPoolID,
		wifProviderID:             payload.WifProviderID,
		wifProjectNumber:          payload.WifProjectNumber,
	})
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating platform external credential").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	// Confirm the id is a platform gcp_iam credential (organization_id NULL,
	// project_id NULL, provider gcp_iam) before touching the subtype, whose
	// update is keyed on the id alone.
	_, err = q.GetGcpIamCredential(ctx, repo.GetGcpIamCredentialParams{ID: id, OrganizationID: platformOrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "platform gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading platform gcp iam credential").LogError(ctx, logger)
	}

	ec, err := q.UpdateExternalCredential(ctx, repo.UpdateExternalCredentialParams{
		Name:           name,
		ID:             id,
		OrganizationID: platformOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// A concurrent delete between the load above and this update.
		return nil, oops.E(oops.CodeNotFound, err, "platform gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error updating platform external credential").LogError(ctx, logger)
	}

	gcp, err := q.UpdateGcpIamCredential(ctx, repo.UpdateGcpIamCredentialParams{
		ImpersonateServiceAccount: cols.ImpersonateServiceAccount,
		WifPoolID:                 cols.WifPoolID,
		WifProviderID:             cols.WifProviderID,
		WifProjectNumber:          cols.WifProjectNumber,
		ExternalCredentialID:      id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating platform gcp iam credential").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving platform external credential").LogError(ctx, logger)
	}

	logPlatformMutation(ctx, logger, authCtx, "update", "gcp_iam_credential", ec.ID.String())

	return mv.BuildPlatformGcpIamCredentialView(ec, gcp), nil
}

func (s *Service) ListPlatformExternalCredentials(ctx context.Context, payload *adminecgen.ListPlatformExternalCredentialsPayload) (*adminecgen.ListExternalCredentialsResult, error) {
	_, logger, err := auth.RequirePlatformAdmin(ctx, s.logger)
	if err != nil {
		return nil, err
	}

	provider := pgtype.Text{String: "", Valid: false}
	if payload.Provider != nil {
		provider = conv.ToPGText(*payload.Provider)
	}

	rows, err := repo.New(s.db).ListExternalCredentials(ctx, repo.ListExternalCredentialsParams{
		OrganizationID: platformOrganizationID,
		Provider:       provider,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing platform external credentials").LogError(ctx, logger)
	}

	return &adminecgen.ListExternalCredentialsResult{
		Credentials: mv.BuildPlatformExternalCredentialSummaryListView(rows),
	}, nil
}

func (s *Service) GetGcpIamPlatformCredential(ctx context.Context, payload *adminecgen.GetGcpIamPlatformCredentialPayload) (*adminecgen.GcpIamCredential, error) {
	_, logger, err := auth.RequirePlatformAdmin(ctx, s.logger)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	row, err := repo.New(s.db).GetGcpIamCredential(ctx, repo.GetGcpIamCredentialParams{ID: id, OrganizationID: platformOrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "platform gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading platform gcp iam credential").LogError(ctx, logger)
	}

	return mv.BuildPlatformGcpIamCredentialView(row.ExternalCredential, row.GcpIamCredential), nil
}

// VerifyGcpIamPlatformCredential runs a live "who am I" probe against the
// credential's resolved GCP identity and reports the effective principal.
// The probe is ephemeral: nothing is persisted. A resolution failure is a
// normal, reportable outcome (verified=false), not a request error.
func (s *Service) VerifyGcpIamPlatformCredential(ctx context.Context, payload *adminecgen.VerifyGcpIamPlatformCredentialPayload) (*adminecgen.VerifyPlatformCredentialResult, error) {
	authCtx, logger, err := auth.RequirePlatformAdmin(ctx, s.logger)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	row, err := repo.New(s.db).GetGcpIamCredential(ctx, repo.GetGcpIamCredentialParams{ID: id, OrganizationID: platformOrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "platform gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading platform gcp iam credential").LogError(ctx, logger)
	}

	principal, resolveErr := s.gcpResolver.ResolvePrincipal(ctx, gcpauth.Credential{
		ImpersonateServiceAccount: row.GcpIamCredential.ImpersonateServiceAccount.String,
		WifPoolID:                 row.GcpIamCredential.WifPoolID.String,
		WifProviderID:             row.GcpIamCredential.WifProviderID.String,
		WifProjectNumber:          row.GcpIamCredential.WifProjectNumber.String,
	})

	logPlatformMutation(ctx, logger, authCtx, "verify", "gcp_iam_credential", id.String())

	if resolveErr != nil {
		detail := resolveErr.Error()
		if errors.Is(resolveErr, gcpauth.ErrUnsupportedMode) {
			detail = "Workload Identity Federation credentials cannot be verified yet; the ambient attached identity and service account impersonation are supported"
		}
		logger.InfoContext(ctx, "platform gcp iam credential verify probe did not resolve", attr.SlogError(resolveErr))
		return &adminecgen.VerifyPlatformCredentialResult{
			Verified:       false,
			Principal:      nil,
			IdentitySource: nil,
			Detail:         conv.PtrEmpty(detail),
		}, nil
	}

	var detail string
	if principal.Email == "" {
		detail = "identity resolved but the source did not report a principal email"
	}

	return &adminecgen.VerifyPlatformCredentialResult{
		Verified:       true,
		Principal:      conv.PtrEmpty(principal.Email),
		IdentitySource: conv.PtrEmpty(string(principal.Source)),
		Detail:         conv.PtrEmpty(detail),
	}, nil
}

func (s *Service) DeleteGcpIamPlatformCredential(ctx context.Context, payload *adminecgen.DeleteGcpIamPlatformCredentialPayload) error {
	authCtx, logger, err := auth.RequirePlatformAdmin(ctx, s.logger)
	if err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error deleting platform external credential").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	// A missing (or wrong-provider) id is a no-op so deletes stay idempotent.
	deleted, err := repo.New(dbtx).SoftDeleteExternalCredential(ctx, repo.SoftDeleteExternalCredentialParams{
		ID:             id,
		OrganizationID: platformOrganizationID,
		Provider:       "gcp_iam",
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error deleting platform external credential").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error saving platform external credential deletion").LogError(ctx, logger)
	}

	logPlatformMutation(ctx, logger, authCtx, "delete", "gcp_iam_credential", deleted.ID.String())

	return nil
}
