package admin

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	ecrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// The platform credential handlers curate platform external_credentials
// records (organization_id NULL AND project_id NULL), shared across every
// organization. No project or organization exists to scope an RBAC grant, so
// each handler requires a live platform administrator; audit is
// structured-logs only (audit_log.organization_id is NOT NULL).

// SetGCPIdentity supplies the GCP identity the platform credential handlers
// screen impersonation targets and verify credentials with. Without it every
// platform credential method reports unavailable.
func (s *Service) SetGCPIdentity(identity *gcpauth.Identity) {
	s.gcpIdentity = identity
}

// platformOrganizationID is the NULL organization_id that selects the platform
// tenancy tier in the shared external_credentials queries (organization_id IS
// NOT DISTINCT FROM the parameter).
var platformOrganizationID = pgtype.Text{String: "", Valid: false}

// authorizePlatformCredentials admits only the verified staff identity the
// admin transport installs, and only when its email belongs to a live platform
// administrator (users.admin set, user not deleted), re-read on every request.
// Access to the admin app alone is not enough: these credentials are the
// identity Speakeasy signs and encrypts with for every organization. It returns
// the actor's email for audit lines.
func (s *Service) authorizePlatformCredentials(ctx context.Context) (string, *slog.Logger, error) {
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		return "", s.logger, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogAdminOIDCSubject(a.OIDCSubject), attr.SlogAuthSource("gram_admin"))

	if s.gcpIdentity == nil {
		return "", logger, oops.C(oops.CodeUnavailable)
	}

	isAdmin, err := usersrepo.New(s.db).IsPlatformAdminEmail(ctx, a.Email)
	if err != nil {
		return "", logger, oops.E(oops.CodeUnavailable, err, "service is temporarily unavailable").LogError(ctx, logger)
	}
	if !isAdmin {
		return "", logger, oops.E(oops.CodeForbidden, nil, "platform admin required").LogWarn(ctx, logger)
	}
	return a.Email, logger, nil
}

// logPlatformCredentialMutation records a structured-log audit line (actor,
// action, subject) for a platform credential mutation, standing in for the
// audit rows platform-scoped records can't have. Call it only after the
// transaction commits so the log never claims a mutation that rolled back.
func logPlatformCredentialMutation(ctx context.Context, logger *slog.Logger, actorEmail, action, subjectID string) {
	logger.InfoContext(ctx, "platform external credential gcp_iam_credential "+action,
		attr.SlogAuditAction(action),
		attr.SlogAuditSubject("gcp_iam_credential"),
		attr.SlogAuditSubjectID(subjectID),
		attr.SlogAuthUserEmail(actorEmail),
	)
}

// logPlatformExemptionGranted records that a platform credential was exempted
// from own-project screening.
func logPlatformExemptionGranted(ctx context.Context, logger *slog.Logger, actorEmail string, credentialID uuid.UUID, target string) {
	logger.InfoContext(ctx, "platform admin exempted a gcp iam credential from own-project screening",
		attr.SlogAuthUserEmail(actorEmail),
		attr.SlogExternalCredentialID(credentialID.String()),
		attr.SlogGCPImpersonateServiceAccount(target),
	)
}

type gcpCredentialInput struct {
	impersonateServiceAccount *string
	wifPoolID                 *string
	wifProviderID             *string
	wifProjectNumber          *string
}

type gcpColumns struct {
	ImpersonateServiceAccount pgtype.Text
	WifPoolID                 pgtype.Text
	WifProviderID             pgtype.Text
	WifProjectNumber          pgtype.Text
}

// resolveGcpColumns validates the platform GCP form and produces the subtype
// column values. The wif_* fields must be provided together; the approach
// (Workload Identity Federation, impersonation, or ambient) follows from which
// fields are set.
func resolveGcpColumns(ctx context.Context, logger *slog.Logger, in gcpCredentialInput) (gcpColumns, error) {
	// Service account emails are case-insensitive; store one form so IAM
	// members built from it always match.
	impersonate := conv.PtrToPGTextTrimmed(in.impersonateServiceAccount)
	impersonate.String = strings.ToLower(impersonate.String)
	poolID := conv.PtrToPGTextTrimmed(in.wifPoolID)
	providerID := conv.PtrToPGTextTrimmed(in.wifProviderID)
	projectNumber := conv.PtrToPGTextTrimmed(in.wifProjectNumber)

	cols := gcpColumns{
		ImpersonateServiceAccount: impersonate,
		WifPoolID:                 poolID,
		WifProviderID:             providerID,
		WifProjectNumber:          projectNumber,
	}

	wifSet := 0
	for _, f := range []pgtype.Text{poolID, providerID, projectNumber} {
		if f.Valid {
			wifSet++
		}
	}
	if wifSet != 0 && wifSet != 3 {
		return cols, oops.E(oops.CodeBadRequest, nil, "wif_pool_id, wif_provider_id, and wif_project_number must be set together").LogError(ctx, logger)
	}
	// GCP project numbers are decimal digits; anything else could never
	// resolve as a Workload Identity Federation audience.
	if projectNumber.Valid && strings.IndexFunc(projectNumber.String, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return cols, oops.E(oops.CodeBadRequest, nil, "wif_project_number must be the numeric GCP project number").LogWarn(ctx, logger)
	}

	return cols, nil
}

// requireNoLiveManagedKeys locks the platform gcp_iam credential for the rest
// of the transaction and refuses the mutation while managed signing keys
// (identity provider connections) still sign through it. The row lock is what
// closes the race with provisioning: a managed key insert takes FOR KEY SHARE
// on the credential, so it is either counted here or blocked until this
// transaction ends and the provisioner re-checks the credential. Reports false
// when no live platform gcp_iam credential has the id.
func requireNoLiveManagedKeys(ctx context.Context, logger *slog.Logger, q *ecrepo.Queries, id uuid.UUID) (bool, error) {
	_, err := q.LockExternalCredentialForUpdate(ctx, ecrepo.LockExternalCredentialForUpdateParams{
		ID:             id,
		OrganizationID: platformOrganizationID,
		Provider:       "gcp_iam",
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, oops.E(oops.CodeUnexpected, err, "error locking platform external credential").LogError(ctx, logger)
	}

	count, err := q.CountLiveManagedExternalKeysByCredential(ctx, id)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "error counting managed signing keys").LogError(ctx, logger)
	}
	if count > 0 {
		return true, oops.E(oops.CodeConflict, nil, "credential backs %d managed signing keys; rotate them first", count)
	}

	return true, nil
}

// requireNoManagedEncryptionKeys refuses a change to a platform credential's
// identity, or its deletion, while Speakeasy-managed organization encryption
// keys still reach KMS through it. Call it after requireNoLiveManagedKeys has
// locked the credential.
func requireNoManagedEncryptionKeys(ctx context.Context, logger *slog.Logger, q *ecrepo.Queries, id uuid.UUID) error {
	count, err := q.CountManagedEncryptionKeysByCredential(ctx, id)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error counting organization encryption keys").LogError(ctx, logger)
	}
	if count > 0 {
		return oops.E(oops.CodeConflict, nil, "credential backs %d organization encryption keys; create a new credential and switch to it instead", count)
	}
	return nil
}

// platformOwnProjectExemption screens a platform credential's impersonation
// target. A target that is not a user-managed service account is refused, as on
// the organization tier: it could never be impersonated. A service account in
// Speakeasy's own project records the own-project exemption. Platform rows back
// Speakeasy's own signing keys, so that is the expected shape; the provisioner
// screens the stored row at use and would refuse it otherwise.
func (s *Service) platformOwnProjectExemption(ctx context.Context, logger *slog.Logger, target pgtype.Text) (bool, error) {
	if !target.Valid || strings.TrimSpace(target.String) == "" {
		return false, nil
	}
	kind, reason, err := s.gcpIdentity.ImpersonationTargetProblem(ctx, logger, strings.TrimSpace(target.String))
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "cannot validate impersonate_service_account right now, try again shortly").LogError(ctx, logger)
	}
	if kind == gcpauth.TargetMalformed {
		return false, oops.E(oops.CodeBadRequest, nil, "%s", reason).LogWarn(ctx, logger)
	}
	return kind == gcpauth.TargetOwnProject, nil
}

func (s *Service) ListPlatformExternalCredentials(ctx context.Context, payload *gen.ListPlatformExternalCredentialsPayload) (*gen.ListExternalCredentialsResult, error) {
	_, logger, err := s.authorizePlatformCredentials(ctx)
	if err != nil {
		return nil, err
	}

	provider := pgtype.Text{String: "", Valid: false}
	if payload.Provider != nil {
		provider = conv.ToPGText(*payload.Provider)
	}

	rows, err := ecrepo.New(s.db).ListExternalCredentials(ctx, ecrepo.ListExternalCredentialsParams{
		OrganizationID: platformOrganizationID,
		Provider:       provider,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing platform external credentials").LogError(ctx, logger)
	}

	credentials := make([]*gen.ExternalCredentialSummary, 0, len(rows))
	for _, ec := range rows {
		credentials = append(credentials, &gen.ExternalCredentialSummary{
			ID:             ec.ID.String(),
			OrganizationID: ec.OrganizationID.String,
			Provider:       ec.Provider,
			Name:           ec.Name,
			CreatedAt:      ec.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:      ec.UpdatedAt.Time.Format(time.RFC3339),
		})
	}
	return &gen.ListExternalCredentialsResult{Credentials: credentials}, nil
}

func (s *Service) GetGcpIamPlatformCredential(ctx context.Context, payload *gen.GetGcpIamPlatformCredentialPayload) (*gen.GcpIamCredential, error) {
	_, logger, err := s.authorizePlatformCredentials(ctx)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	row, err := ecrepo.New(s.db).GetGcpIamCredential(ctx, ecrepo.GetGcpIamCredentialParams{ID: id, OrganizationID: platformOrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "platform gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading platform gcp iam credential").LogError(ctx, logger)
	}

	return buildPlatformGcpIamCredentialView(row.ExternalCredential, row.GcpIamCredential), nil
}

func (s *Service) CreateGcpIamPlatformCredential(ctx context.Context, payload *gen.CreateGcpIamPlatformCredentialPayload) (*gen.GcpIamCredential, error) {
	actor, logger, err := s.authorizePlatformCredentials(ctx)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required").LogError(ctx, logger)
	}

	cols, err := resolveGcpColumns(ctx, logger, gcpCredentialInput{
		impersonateServiceAccount: payload.ImpersonateServiceAccount,
		wifPoolID:                 payload.WifPoolID,
		wifProviderID:             payload.WifProviderID,
		wifProjectNumber:          payload.WifProjectNumber,
	})
	if err != nil {
		return nil, err
	}
	exempt, err := s.platformOwnProjectExemption(ctx, logger, cols.ImpersonateServiceAccount)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating platform external credential").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := ecrepo.New(dbtx)

	// Platform scope: organization_id NULL (project_id defaults NULL).
	ec, err := q.CreateExternalCredential(ctx, ecrepo.CreateExternalCredentialParams{
		OrganizationID: platformOrganizationID,
		Provider:       "gcp_iam",
		Name:           name,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating platform external credential").LogError(ctx, logger)
	}

	gcp, err := q.CreateGcpIamCredential(ctx, ecrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      ec.ID,
		ImpersonateServiceAccount: cols.ImpersonateServiceAccount,
		WifPoolID:                 cols.WifPoolID,
		WifProviderID:             cols.WifProviderID,
		WifProjectNumber:          cols.WifProjectNumber,
		SkipProjectVerification:   exempt,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating platform gcp iam credential").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving platform external credential").LogError(ctx, logger)
	}

	logPlatformCredentialMutation(ctx, logger, actor, "create", ec.ID.String())
	if exempt {
		logPlatformExemptionGranted(ctx, logger, actor, ec.ID, cols.ImpersonateServiceAccount.String)
	}

	return buildPlatformGcpIamCredentialView(ec, gcp), nil
}

// UpdateGcpIamPlatformCredential replaces a platform GCP credential's name and
// auth configuration (full replace, like the organization update).
func (s *Service) UpdateGcpIamPlatformCredential(ctx context.Context, payload *gen.UpdateGcpIamPlatformCredentialPayload) (*gen.GcpIamCredential, error) {
	actor, logger, err := s.authorizePlatformCredentials(ctx)
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

	cols, err := resolveGcpColumns(ctx, logger, gcpCredentialInput{
		impersonateServiceAccount: payload.ImpersonateServiceAccount,
		wifPoolID:                 payload.WifPoolID,
		wifProviderID:             payload.WifProviderID,
		wifProjectNumber:          payload.WifProjectNumber,
	})
	if err != nil {
		return nil, err
	}

	exempt, err := s.platformOwnProjectExemption(ctx, logger, cols.ImpersonateServiceAccount)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating platform external credential").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := ecrepo.New(dbtx)

	// Confirm the id is a platform gcp_iam credential (organization_id NULL,
	// project_id NULL, provider gcp_iam) before touching the subtype, whose
	// update is keyed on the id alone.
	prior, err := q.GetGcpIamCredential(ctx, ecrepo.GetGcpIamCredentialParams{ID: id, OrganizationID: platformOrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "platform gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading platform gcp iam credential").LogError(ctx, logger)
	}

	// The update is a full replace of the identity, so any managed key still
	// signing through this credential would lose its signer.
	found, err := requireNoLiveManagedKeys(ctx, logger, q, id)
	if err != nil {
		return nil, err
	}
	if !found {
		// A concurrent delete between the load above and the lock.
		return nil, oops.E(oops.CodeNotFound, nil, "platform gcp iam credential not found")
	}
	// Re-read under the lock: the earlier read may predate a concurrent change.
	locked, err := q.GetGcpIamCredential(ctx, ecrepo.GetGcpIamCredentialParams{ID: id, OrganizationID: platformOrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "platform gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading platform gcp iam credential").LogError(ctx, logger)
	}
	// Organization encryption keys and issuer keys sign through this exact
	// identity: the account they were granted to and how Speakeasy reaches it.
	// Issuer key cleanup also removes the grant from that account. So the
	// identity can't change while any of them depend on it. Renames stay
	// allowed.
	stored := locked.GcpIamCredential
	identityChanged := strings.ToLower(strings.TrimSpace(stored.ImpersonateServiceAccount.String)) != cols.ImpersonateServiceAccount.String ||
		stored.WifPoolID != cols.WifPoolID || stored.WifProviderID != cols.WifProviderID || stored.WifProjectNumber != cols.WifProjectNumber
	if identityChanged {
		if err := requireNoManagedEncryptionKeys(ctx, logger, q, id); err != nil {
			return nil, err
		}
		granted, err := q.CountIssuerKeysGrantedByCredential(ctx, ecrepo.CountIssuerKeysGrantedByCredentialParams{ExternalCredentialID: id, IncludePendingCleanup: true})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error counting issuer signing keys").LogError(ctx, logger)
		}
		if granted > 0 {
			return nil, oops.E(oops.CodeConflict, nil, "credential holds signer grants on %d issuer keys; create a new credential and switch to it instead", granted)
		}
	}

	ec, err := q.UpdateExternalCredential(ctx, ecrepo.UpdateExternalCredentialParams{
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

	gcp, err := q.UpdateGcpIamCredential(ctx, ecrepo.UpdateGcpIamCredentialParams{
		ImpersonateServiceAccount: cols.ImpersonateServiceAccount,
		WifPoolID:                 cols.WifPoolID,
		WifProviderID:             cols.WifProviderID,
		WifProjectNumber:          cols.WifProjectNumber,
		SkipProjectVerification:   exempt,
		ExternalCredentialID:      id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating platform gcp iam credential").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving platform external credential").LogError(ctx, logger)
	}

	logPlatformCredentialMutation(ctx, logger, actor, "update", ec.ID.String())
	// A rename that keeps an exempted target is not a new grant.
	carried := prior.GcpIamCredential.SkipProjectVerification && strings.EqualFold(strings.TrimSpace(prior.GcpIamCredential.ImpersonateServiceAccount.String), strings.TrimSpace(cols.ImpersonateServiceAccount.String))
	if exempt && !carried {
		logPlatformExemptionGranted(ctx, logger, actor, id, cols.ImpersonateServiceAccount.String)
	}

	return buildPlatformGcpIamCredentialView(ec, gcp), nil
}

// VerifyGcpIamPlatformCredential runs a live "who am I" probe against the
// credential's resolved GCP identity and reports the effective principal.
// The probe is ephemeral: nothing is persisted. A resolution failure is a
// normal, reportable outcome (verified=false), not a request error.
func (s *Service) VerifyGcpIamPlatformCredential(ctx context.Context, payload *gen.VerifyGcpIamPlatformCredentialPayload) (*gen.VerifyPlatformCredentialResult, error) {
	actor, logger, err := s.authorizePlatformCredentials(ctx)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid credential id").LogError(ctx, logger)
	}

	row, err := ecrepo.New(s.db).GetGcpIamCredential(ctx, ecrepo.GetGcpIamCredentialParams{ID: id, OrganizationID: platformOrganizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "platform gcp iam credential not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading platform gcp iam credential").LogError(ctx, logger)
	}

	principal, resolveErr := s.gcpIdentity.ResolvePrincipal(ctx, gcpauth.Credential{
		ImpersonateServiceAccount: row.GcpIamCredential.ImpersonateServiceAccount.String,
		WifPoolID:                 row.GcpIamCredential.WifPoolID.String,
		WifProviderID:             row.GcpIamCredential.WifProviderID.String,
		WifProjectNumber:          row.GcpIamCredential.WifProjectNumber.String,
	})

	logPlatformCredentialMutation(ctx, logger, actor, "verify", id.String())

	if resolveErr != nil {
		detail := resolveErr.Error()
		if errors.Is(resolveErr, gcpauth.ErrUnsupportedMode) {
			detail = "Workload Identity Federation credentials cannot be verified yet; the ambient attached identity and service account impersonation are supported"
		}
		logger.InfoContext(ctx, "platform gcp iam credential verify probe did not resolve", attr.SlogError(resolveErr))
		return &gen.VerifyPlatformCredentialResult{
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

	return &gen.VerifyPlatformCredentialResult{
		Verified:       true,
		Principal:      conv.PtrEmpty(principal.Email),
		IdentitySource: conv.PtrEmpty(string(principal.Source)),
		Detail:         conv.PtrEmpty(detail),
	}, nil
}

func (s *Service) DeleteGcpIamPlatformCredential(ctx context.Context, payload *gen.DeleteGcpIamPlatformCredentialPayload) error {
	actor, logger, err := s.authorizePlatformCredentials(ctx)
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

	q := ecrepo.New(dbtx)

	// A missing (or wrong-provider) id is a no-op so deletes stay idempotent.
	found, err := requireNoLiveManagedKeys(ctx, logger, q, id)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if err := requireNoManagedEncryptionKeys(ctx, logger, q, id); err != nil {
		return err
	}
	// Live issuer keys sign through this credential. Revoked ones don't block:
	// their cleanup reads the deleted credential's account.
	live, err := q.CountIssuerKeysGrantedByCredential(ctx, ecrepo.CountIssuerKeysGrantedByCredentialParams{ExternalCredentialID: id, IncludePendingCleanup: false})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error counting issuer signing keys").LogError(ctx, logger)
	}
	if live > 0 {
		return oops.E(oops.CodeConflict, nil, "credential backs %d live issuer keys; replace them first", live)
	}

	deleted, err := q.SoftDeleteExternalCredential(ctx, ecrepo.SoftDeleteExternalCredentialParams{
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

	logPlatformCredentialMutation(ctx, logger, actor, "delete", deleted.ID.String())

	return nil
}

// buildPlatformGcpIamCredentialView joins the supertype and GCP subtype rows
// into the platform GCP credential view. Platform rows carry no organization,
// so OrganizationID is empty.
func buildPlatformGcpIamCredentialView(ec ecrepo.ExternalCredential, gcp ecrepo.GcpIamCredential) *gen.GcpIamCredential {
	return &gen.GcpIamCredential{
		ID:                        ec.ID.String(),
		OrganizationID:            ec.OrganizationID.String,
		Provider:                  ec.Provider,
		Name:                      ec.Name,
		CreatedAt:                 ec.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                 ec.UpdatedAt.Time.Format(time.RFC3339),
		ImpersonateServiceAccount: conv.FromPGText[string](gcp.ImpersonateServiceAccount),
		WifPoolID:                 conv.FromPGText[string](gcp.WifPoolID),
		WifProviderID:             conv.FromPGText[string](gcp.WifProviderID),
		WifProjectNumber:          conv.FromPGText[string](gcp.WifProjectNumber),
	}
}
