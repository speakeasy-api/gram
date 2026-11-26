package admin

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/secretbackfills/functionsaccessorganization"
	"github.com/speakeasy-api/gram/server/internal/secretbackfills/marketplacetokenbackfill"
)

const (
	// secretBackfillTimeout bounds one run, which outlives the request so a
	// closed tab does not cut it short. Production holds a few hundred
	// marketplace tokens and a few thousand runner keys, which take seconds.
	secretBackfillTimeout = 10 * time.Minute

	// secretBackfillBatchSize is the most rows one statement covers, the
	// migrations CLI's default for both backfills. Every function tool call
	// reads functions_access, so its batches stay small.
	secretBackfillBatchSize = 500

	secretBackfillMarketplaceTokens      = "marketplace_tokens"
	secretBackfillRunnerKeyOrganizations = "runner_key_organizations"
)

// SecretBackfillPlatformKey is what the temporary marketplace token backfill
// needs from the process: the platform key it seals with, and the token store
// over the process's organization encryption.
type SecretBackfillPlatformKey struct {
	// Platform is the platform key. Its fingerprint must match the servers'.
	Platform *encryption.Client

	// Tokens moves one connection's plaintext token.
	Tokens marketplacetokenbackfill.Mover
}

// SetSecretBackfillPlatformKey enables applying the temporary marketplace
// token backfill. Without it, applying reports unavailable; previews and the
// runner key organization backfill need no key.
func (s *Service) SetSecretBackfillPlatformKey(key *SecretBackfillPlatformKey) {
	s.secretBackfillPlatformKey = key
}

func (s *Service) GetSecretBackfills(ctx context.Context, _ *gen.GetSecretBackfillsPayload) (*gen.AdminSecretBackfills, error) {
	tokens, err := marketplacetokenbackfill.Run(ctx, s.db, nil, marketplacetokenbackfill.Options{OrganizationID: "", Apply: false, BatchSize: secretBackfillBatchSize})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "preview the marketplace token backfill").LogError(ctx, s.logger)
	}

	tokenOrganizations, err := marketplacetokenbackfill.Organizations(ctx, s.db)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "preview the marketplace token backfill").LogError(ctx, s.logger)
	}

	runnerKeys, err := functionsaccessorganization.Run(ctx, s.db, functionsaccessorganization.Options{OrganizationID: "", Apply: false, BatchSize: secretBackfillBatchSize})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "preview the runner key organization backfill").LogError(ctx, s.logger)
	}

	runnerKeyOrganizations, err := functionsaccessorganization.Organizations(ctx, s.db)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "preview the runner key organization backfill").LogError(ctx, s.logger)
	}

	var fingerprint *string
	if s.secretBackfillPlatformKey != nil {
		fingerprint = new(s.secretBackfillPlatformKey.Platform.Fingerprint())
	}

	return &gen.AdminSecretBackfills{
		MarketplaceTokens:      marketplaceTokenBackfillView(tokens),
		RunnerKeyOrganizations: runnerKeyOrganizationBackfillView(runnerKeys),
		Organizations:          secretBackfillOrganizationsView(tokenOrganizations, runnerKeyOrganizations),
		PlatformKeyFingerprint: fingerprint,
	}, nil
}

func (s *Service) RunMarketplaceTokenBackfill(ctx context.Context, payload *gen.RunMarketplaceTokenBackfillPayload) (*gen.AdminMarketplaceTokenBackfill, error) {
	subject, err := secretBackfillOperator(ctx)
	if err != nil {
		return nil, err
	}

	key := s.secretBackfillPlatformKey
	if key == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "this admin process has no platform key, so it cannot seal marketplace tokens").LogWarn(ctx, s.logger)
	}

	// The operator checked a fingerprint the page showed earlier. The process
	// may have restarted with another key since, and sealing with a key the
	// servers do not run would leave them unable to show the install URLs.
	if fingerprint := key.Platform.Fingerprint(); payload.PlatformKeyFingerprint != fingerprint {
		return nil, oops.E(oops.CodeConflict, nil, "this admin server's platform key fingerprint is now %s, not the one you checked; refresh, check it against the servers' encryption key loaded log, then apply again", fingerprint).LogWarn(ctx, s.logger)
	}

	organizationID := conv.PtrValOrEmpty(payload.OrganizationID, "")
	var report marketplacetokenbackfill.Report
	err = s.runSecretBackfill(ctx, secretBackfillMarketplaceTokens, func(runCtx context.Context) error {
		var runErr error
		report, runErr = marketplacetokenbackfill.Run(runCtx, s.db, key.Tokens, marketplacetokenbackfill.Options{OrganizationID: organizationID, Apply: true, BatchSize: secretBackfillBatchSize})
		if runErr != nil {
			return fmt.Errorf("apply the marketplace token backfill: %w", runErr)
		}
		return nil
	})
	switch {
	case err != nil && orgencryption.IsKeyProblem(err):
		return nil, oops.E(oops.CodeUnavailable, err, "an organization's encryption key is unavailable, so the backfill stopped after moving %d tokens; run it again once the key is back", report.Moved).LogWarn(ctx, s.logger)
	case err != nil:
		return nil, s.secretBackfillError(ctx, err, "the backfill stopped after moving %d tokens; run it again to continue", report.Moved)
	}

	s.logger.InfoContext(ctx, "marketplace token backfill applied", secretBackfillAuditAttrs(secretBackfillMarketplaceTokens, subject, organizationID,
		attr.SlogEncryptionKeyFingerprint(key.Platform.Fingerprint()),
	)...)

	return marketplaceTokenBackfillView(report), nil
}

func (s *Service) RunRunnerKeyOrganizationBackfill(ctx context.Context, payload *gen.RunRunnerKeyOrganizationBackfillPayload) (*gen.AdminRunnerKeyOrganizationBackfill, error) {
	subject, err := secretBackfillOperator(ctx)
	if err != nil {
		return nil, err
	}

	organizationID := conv.PtrValOrEmpty(payload.OrganizationID, "")
	var report functionsaccessorganization.Report
	err = s.runSecretBackfill(ctx, secretBackfillRunnerKeyOrganizations, func(runCtx context.Context) error {
		var runErr error
		report, runErr = functionsaccessorganization.Run(runCtx, s.db, functionsaccessorganization.Options{OrganizationID: organizationID, Apply: true, BatchSize: secretBackfillBatchSize})
		if runErr != nil {
			return fmt.Errorf("apply the runner key organization backfill: %w", runErr)
		}
		return nil
	})
	if err != nil {
		return nil, s.secretBackfillError(ctx, err, "the backfill stopped after filling %d runner keys; run it again to continue", report.Filled)
	}

	s.logger.InfoContext(ctx, "runner key organization backfill applied", secretBackfillAuditAttrs(secretBackfillRunnerKeyOrganizations, subject, organizationID)...)

	return runnerKeyOrganizationBackfillView(report), nil
}

// errSecretBackfillRunning reports that another run of the same backfill
// holds its lock.
var errSecretBackfillRunning = errors.New("another run of this backfill is in progress")

// runSecretBackfill runs one backfill while holding its lock, on a context
// that outlives the request. The lock lives in a transaction of its own and is
// released when that transaction ends, however the run ends; the backfill
// writes through the pool.
func (s *Service) runSecretBackfill(ctx context.Context, backfill string, run func(context.Context) error) error {
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), secretBackfillTimeout)
	defer cancel()

	lockTx, err := s.db.Begin(runCtx)
	if err != nil {
		return fmt.Errorf("begin the backfill lock transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return lockTx.Rollback(context.WithoutCancel(runCtx)) })

	locked, err := repo.New(lockTx).AdminTryLockSecretBackfill(runCtx, backfill)
	if err != nil {
		return fmt.Errorf("take the backfill lock: %w", err)
	}
	if !locked {
		return errSecretBackfillRunning
	}

	return run(runCtx)
}

// secretBackfillOperator returns the staff subject applying a backfill.
func secretBackfillOperator(ctx context.Context) (string, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return "", oops.C(oops.CodeUnauthorized)
	}

	return auth.OIDCSubject, nil
}

// secretBackfillAuditAttrs are the attributes of the log line that records an
// applied run. A run for every organization has no organization attribute.
func secretBackfillAuditAttrs(backfill string, subject string, organizationID string, extra ...any) []any {
	attrs := []any{
		attr.SlogAuditAction("secret_backfill:" + backfill),
		attr.SlogAdminOIDCSubject(subject),
	}
	if organizationID != "" {
		attrs = append(attrs, attr.SlogOrganizationID(organizationID))
	}
	return append(attrs, extra...)
}

// secretBackfillError maps a failed run. Both backfills are safe to run again,
// so the message says how far the run got.
func (s *Service) secretBackfillError(ctx context.Context, err error, format string, done int64) error {
	if errors.Is(err, errSecretBackfillRunning) {
		return oops.E(oops.CodeConflict, err, "%s", err.Error()).LogInfo(ctx, s.logger)
	}

	return oops.E(oops.CodeUnexpected, err, format, done).LogError(ctx, s.logger)
}

func marketplaceTokenBackfillView(report marketplacetokenbackfill.Report) *gen.AdminMarketplaceTokenBackfill {
	return &gen.AdminMarketplaceTokenBackfill{
		Attributed:           report.Attributed,
		Moved:                report.Moved,
		Skipped:              report.Skipped,
		NeedsOrganizationKey: report.NeedsOrganizationKey,
		Unattributed:         report.Counts.Unattributed,
		Legacy:               report.Counts.Legacy,
		Hashed:               report.Counts.Hashed,
	}
}

// secretBackfillOrganizationsView merges both backfills' organizations into
// one row per organization, by name.
func secretBackfillOrganizationsView(tokens []marketplacetokenbackfill.Organization, runnerKeys []functionsaccessorganization.Organization) []*gen.AdminSecretBackfillOrganization {
	byID := make(map[string]*gen.AdminSecretBackfillOrganization, len(tokens)+len(runnerKeys))
	row := func(id string, name string, slug string) *gen.AdminSecretBackfillOrganization {
		if existing, ok := byID[id]; ok {
			return existing
		}
		created := &gen.AdminSecretBackfillOrganization{OrganizationID: id, Name: name, Slug: slug, LegacyTokens: 0, UnattributedConnections: 0, PendingRunnerKeys: 0}
		byID[id] = created
		return created
	}
	for _, organization := range tokens {
		r := row(organization.ID, organization.Name, organization.Slug)
		r.LegacyTokens = organization.Legacy
		r.UnattributedConnections = organization.Unattributed
	}
	for _, organization := range runnerKeys {
		row(organization.ID, organization.Name, organization.Slug).PendingRunnerKeys = organization.Pending
	}

	return slices.SortedFunc(maps.Values(byID), func(a, b *gen.AdminSecretBackfillOrganization) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.OrganizationID, b.OrganizationID),
		)
	})
}

func runnerKeyOrganizationBackfillView(report functionsaccessorganization.Report) *gen.AdminRunnerKeyOrganizationBackfill {
	return &gen.AdminRunnerKeyOrganizationBackfill{
		Filled:              report.Filled,
		Pending:             report.Counts.Pending,
		MissingOrganization: report.Counts.MissingOrganization,
	}
}
