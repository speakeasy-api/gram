package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// IssuerMetadataRefreshActor is the system principal the scheduled refresh audits as.
	IssuerMetadataRefreshActor = "issuer-metadata-refresh"

	// IssuerMetadataRefreshActorDisplayName is the name the audit feed shows for that principal.
	IssuerMetadataRefreshActorDisplayName = "Issuer metadata refresh"

	// IssuerMetadataStaleAfter is how long a visit, successful or not, keeps an issuer off the sweep.
	IssuerMetadataStaleAfter = 24 * time.Hour

	// IssuerMetadataRetryAfter is how soon a transiently unreadable candidate is retried.
	IssuerMetadataRetryAfter = time.Hour
)

// IssuerMetadataRefreshCandidate is one issuer the scheduled refresh should visit, keyed by the identity every write re-asserts.
type IssuerMetadataRefreshCandidate struct {
	ID                  uuid.UUID
	IssuerURL           string
	Host                string
	ProjectID           uuid.NullUUID
	OrganizationID      pgtype.Text
	TunneledMcpServerID uuid.NullUUID
}

// IssuerMetadataRefresher runs the scheduled RFC 8414 metadata refresh over every tier's issuers.
type IssuerMetadataRefresher struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	policy      *guardian.Policy
	auditLogger *audit.Logger
	metrics     *remotesessionmetrics.IssuerMetadataRefresh
}

func NewIssuerMetadataRefresher(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, policy *guardian.Policy, auditLogger *audit.Logger) *IssuerMetadataRefresher {
	return &IssuerMetadataRefresher{
		logger:      logger.With(attr.SlogComponent("remotesessions_issuer_metadata_refresh")),
		db:          db,
		policy:      policy,
		auditLogger: auditLogger,
		metrics:     remotesessionmetrics.NewIssuerMetadataRefresh(logger, meterProvider),
	}
}

// IssuerMetadataRefreshHost is the lowercase host of an issuer URL without a default port, or "" when it does not parse.
func IssuerMetadataRefreshHost(issuerURL string) string {
	parsed, err := url.Parse(issuerURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	host := strings.ToLower(parsed.Host)
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		host = strings.TrimSuffix(host, ":"+port)
	}
	return host
}

// IssuerMetadataRefreshCursor is the keyset position of the last row of a ListDue page.
type IssuerMetadataRefreshCursor struct {
	VisitedAt pgtype.Timestamptz
	ID        uuid.UUID
}

// ListDue returns one page of issuers not visited since the stale cutoff, or left transiently unread before the retry cutoff, oldest visit first; the returned cursor is nil once the page came back short.
func (r *IssuerMetadataRefresher) ListDue(ctx context.Context, now time.Time, after *IssuerMetadataRefreshCursor, limit int32) ([]IssuerMetadataRefreshCandidate, *IssuerMetadataRefreshCursor, error) {
	params := repo.ListRemoteSessionIssuersDueForMetadataRefreshParams{
		StaleCutoff:    conv.ToPGTimestamptz(now.Add(-IssuerMetadataStaleAfter)),
		RetryCutoff:    conv.ToPGTimestamptz(now.Add(-IssuerMetadataRetryAfter)),
		AfterCursor:    false,
		AfterVisitedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		AfterID:        uuid.Nil,
		LimitValue:     limit,
	}
	if after != nil {
		params.AfterCursor = true
		params.AfterVisitedAt = after.VisitedAt
		params.AfterID = after.ID
	}
	rows, err := repo.New(r.db).ListRemoteSessionIssuersDueForMetadataRefresh(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list issuers due for metadata refresh: %w", err)
	}

	candidates := make([]IssuerMetadataRefreshCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, IssuerMetadataRefreshCandidate{
			ID:                  row.ID,
			IssuerURL:           row.Issuer,
			Host:                IssuerMetadataRefreshHost(row.Issuer),
			ProjectID:           row.ProjectID,
			OrganizationID:      row.OrganizationID,
			TunneledMcpServerID: row.TunneledMcpServerID,
		})
	}
	var next *IssuerMetadataRefreshCursor
	if limit > 0 && len(rows) == int(limit) {
		last := rows[len(rows)-1]
		next = &IssuerMetadataRefreshCursor{VisitedAt: last.VisitedAt, ID: last.ID}
	}
	return candidates, next, nil
}

// ListReprojectable returns issuers holding a stored document that predates one of the capability columns and has not failed since the stale cutoff.
func (r *IssuerMetadataRefresher) ListReprojectable(ctx context.Context, now time.Time, limit int32) ([]IssuerMetadataRefreshCandidate, error) {
	rows, err := repo.New(r.db).ListRemoteSessionIssuersForMetadataReprojection(ctx, repo.ListRemoteSessionIssuersForMetadataReprojectionParams{
		StaleCutoff: conv.ToPGTimestamptz(now.Add(-IssuerMetadataStaleAfter)),
		LimitValue:  limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list issuers for metadata reprojection: %w", err)
	}

	candidates := make([]IssuerMetadataRefreshCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, IssuerMetadataRefreshCandidate{
			ID:                  row.ID,
			IssuerURL:           row.Issuer,
			Host:                IssuerMetadataRefreshHost(row.Issuer),
			ProjectID:           row.ProjectID,
			OrganizationID:      row.OrganizationID,
			TunneledMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		})
	}
	return candidates, nil
}

// RecordSkipped meters an issuer the sweep decided not to contact.
func (r *IssuerMetadataRefresher) RecordSkipped(ctx context.Context, candidate IssuerMetadataRefreshCandidate, outcome remotesessionmetrics.IssuerMetadataRefreshOutcome) {
	r.metrics.Record(ctx, candidate.Host, outcome)
}

// Reproject rewrites an issuer's capability columns from its stored document, leaving the document and the tracking columns alone.
func (r *IssuerMetadataRefresher) Reproject(ctx context.Context, candidate IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	existing, outcome, err := r.load(ctx, candidate)
	if err != nil || outcome != "" {
		return r.record(ctx, candidate.Host, outcome), err
	}
	logger := r.logger.With(attr.SlogRemoteSessionIssuerID(existing.ID.String()), attr.SlogOAuthIssuer(existing.Issuer))

	doc, err := decodeStoredIssuerDocument(existing)
	if err != nil {
		logger.WarnContext(ctx, "stored issuer metadata document cannot be re-projected", attr.SlogError(err))
		msg := "stored issuer metadata document could not be decoded"
		if ude, ok := errors.AsType[*untrustedDocumentError](err); ok {
			msg = ude.reason
		}
		outcome, err := r.recordFailure(ctx, existing, msg, "", remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojectInvalid)
		return r.record(ctx, candidate.Host, outcome), err
	}

	outcome, err = r.apply(ctx, logger, existing, func(q *repo.Queries) (repo.RemoteSessionIssuer, error) {
		return q.ReprojectRemoteSessionIssuerMetadataCapabilities(ctx, repo.ReprojectRemoteSessionIssuerMetadataCapabilitiesParams{
			CodeChallengeMethodsSupported:              orEmptySlice(doc.CodeChallengeMethodsSupported),
			UserinfoEndpoint:                           doc.UserinfoEndpoint,
			IntrospectionEndpoint:                      doc.IntrospectionEndpoint,
			IntrospectionEndpointAuthMethodsSupported:  orEmptySlice(doc.IntrospectionEndpointAuthMethodsSupported),
			IDTokenSigningAlgValuesSupported:           orEmptySlice(doc.IDTokenSigningAlgValuesSupported),
			ClaimsSupported:                            orEmptySlice(doc.ClaimsSupported),
			BackchannelLogoutSupported:                 doc.BackchannelLogoutSupported,
			AuthorizationResponseIssParameterSupported: doc.AuthorizationResponseIssParameterSupported,
			ID:             existing.ID,
			Issuer:         existing.Issuer,
			ProjectID:      existing.ProjectID,
			OrganizationID: existing.OrganizationID,
		})
	}, remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojected)
	return r.record(ctx, candidate.Host, outcome), err
}

// Refresh fetches an issuer's upstream metadata and applies it; discovery failures are outcomes, not errors.
func (r *IssuerMetadataRefresher) Refresh(ctx context.Context, candidate IssuerMetadataRefreshCandidate) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	existing, outcome, err := r.load(ctx, candidate)
	if err != nil || outcome != "" {
		return r.record(ctx, candidate.Host, outcome), err
	}
	logger := r.logger.With(attr.SlogRemoteSessionIssuerID(existing.ID.String()), attr.SlogOAuthIssuer(existing.Issuer))

	params, _, err := refreshIssuerMetadata(ctx, r.policy, existing)
	if err != nil {
		msg, _ := discoveryFailureMessage(err)
		retryURL := discoveryRetryURL(err)
		failure := remotesessionmetrics.IssuerMetadataRefreshOutcomeDefinitiveFailure
		if retryURL != "" {
			failure = remotesessionmetrics.IssuerMetadataRefreshOutcomeTransientFailure
		}
		logger.WarnContext(ctx, "scheduled issuer metadata refresh failed", attr.SlogOutcome(string(failure)), attr.SlogError(err))
		outcome, err := r.recordFailure(ctx, existing, msg, retryURL, failure)
		return r.record(ctx, candidate.Host, outcome), err
	}

	success := remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed
	if params.MetadataLastErrorUrl != "" {
		success = remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshedPartial
	}
	outcome, err = r.apply(ctx, logger, existing, func(q *repo.Queries) (repo.RemoteSessionIssuer, error) {
		return q.UpdateRemoteSessionIssuerDiscoveredMetadata(ctx, params)
	}, success)
	return r.record(ctx, candidate.Host, outcome), err
}

// load reads the row under the listed identity; a miss is a conflict outcome.
func (r *IssuerMetadataRefresher) load(ctx context.Context, candidate IssuerMetadataRefreshCandidate) (repo.RemoteSessionIssuer, remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	var zero repo.RemoteSessionIssuer
	existing, err := repo.New(r.db).GetRemoteSessionIssuerForMetadataRefresh(ctx, repo.GetRemoteSessionIssuerForMetadataRefreshParams{
		ID:             candidate.ID,
		Issuer:         candidate.IssuerURL,
		ProjectID:      candidate.ProjectID,
		OrganizationID: candidate.OrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, nil
		}
		return zero, remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("get remote session issuer: %w", err)
	}
	return existing, "", nil
}

func decodeStoredIssuerDocument(existing repo.RemoteSessionIssuer) (rfc8414Document, error) {
	requested, err := url.Parse(existing.Issuer)
	if err != nil {
		return rfc8414Document{}, fmt.Errorf("parse issuer url: %w", err)
	}
	if len(existing.Metadata) == 0 {
		return rfc8414Document{}, errors.New("no stored metadata document")
	}
	doc, err := decodeIssuerDocument(existing.Metadata, requested)
	if err != nil {
		return rfc8414Document{}, err
	}
	if err := vetRefreshedDocument(doc, existing); err != nil {
		return rfc8414Document{}, err
	}
	return doc, nil
}

func (r *IssuerMetadataRefresher) record(ctx context.Context, host string, outcome remotesessionmetrics.IssuerMetadataRefreshOutcome) remotesessionmetrics.IssuerMetadataRefreshOutcome {
	r.metrics.Record(ctx, host, outcome)
	return outcome
}

// recordFailure stamps the error trio on the row; a retry URL marks the failure transient.
func (r *IssuerMetadataRefresher) recordFailure(ctx context.Context, existing repo.RemoteSessionIssuer, msg, retryURL string, outcome remotesessionmetrics.IssuerMetadataRefreshOutcome) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	rows, err := repo.New(r.db).RecordRemoteSessionIssuerMetadataRefreshFailure(ctx, repo.RecordRemoteSessionIssuerMetadataRefreshFailureParams{
		MetadataLastError:    msg,
		MetadataLastErrorUrl: retryURL,
		ID:                   existing.ID,
		Issuer:               existing.Issuer,
		ProjectID:            existing.ProjectID,
		OrganizationID:       existing.OrganizationID,
	})
	if err != nil {
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("record issuer metadata refresh failure: %w", err)
	}
	if rows == 0 {
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, nil
	}
	return outcome, nil
}

// apply runs the write and the system-actor audit entry in one transaction, snapshotting the row under lock.
func (r *IssuerMetadataRefresher) apply(ctx context.Context, logger *slog.Logger, existing repo.RemoteSessionIssuer, write func(*repo.Queries) (repo.RemoteSessionIssuer, error), success remotesessionmetrics.IssuerMetadataRefreshOutcome) (remotesessionmetrics.IssuerMetadataRefreshOutcome, error) {
	ctx = contextvalues.SetActingSurface(ctx, string(audit.SurfaceSystem))

	dbtx, err := r.db.Begin(ctx)
	if err != nil {
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	locked, err := txRepo.LockRemoteSessionIssuerForMetadataRefresh(ctx, repo.LockRemoteSessionIssuerForMetadataRefreshParams{
		ID:             existing.ID,
		Issuer:         existing.Issuer,
		ProjectID:      existing.ProjectID,
		OrganizationID: existing.OrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, nil
		}
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("lock remote session issuer: %w", err)
	}

	// A fetch that landed between the read and this lock, such as an operator refresh, wins; overwriting it would drop the newer document.
	if !sameTimestamp(locked.MetadataFetchedAt, existing.MetadataFetchedAt) {
		logger.InfoContext(ctx, "issuer metadata changed during refresh; leaving the newer document in place", attr.SlogOutcome(string(remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict)))
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, nil
	}

	updated, err := write(txRepo)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, nil
		}
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("write remote session issuer metadata: %w", err)
	}

	// Only a row with neither scope column is global; a legacy project row resolves its organization through the project.
	organizationID := updated.OrganizationID.String
	if !updated.OrganizationID.Valid && updated.ProjectID.Valid {
		organizationID, err = txRepo.GetProjectOrganizationID(ctx, updated.ProjectID.UUID)
		if err != nil {
			return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("resolve owning organization for project issuer: %w", err)
		}
	}

	if organizationID != "" {
		if err := r.auditLogger.LogRemoteSessionIssuerUpdate(ctx, dbtx, audit.LogRemoteSessionIssuerUpdateEvent{
			OrganizationID:         organizationID,
			ProjectID:              orgProjectID(updated.ProjectID),
			Actor:                  urn.NewSystemPrincipal(IssuerMetadataRefreshActor),
			ActorDisplayName:       conv.PtrEmpty(IssuerMetadataRefreshActorDisplayName),
			ActorSlug:              nil,
			RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(updated.ID),
			Slug:                   updated.Slug,
			IssuerURL:              updated.Issuer,
			Name:                   conv.FromPGText[string](updated.Name),
			SnapshotBefore:         mv.BuildRemoteSessionIssuerView(locked),
			SnapshotAfter:          mv.BuildRemoteSessionIssuerView(updated),
		}); err != nil {
			return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("log remote session issuer update: %w", err)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return remotesessionmetrics.IssuerMetadataRefreshOutcomeInternalError, fmt.Errorf("commit transaction: %w", err)
	}

	if organizationID == "" {
		logger.InfoContext(ctx, "global remote session issuer metadata refreshed",
			attr.SlogAuditAction("update"),
			attr.SlogAuditSubject("issuer"),
			attr.SlogAuditSubjectID(updated.ID.String()),
			attr.SlogOutcome(string(success)),
		)
	}
	return success, nil
}

func sameTimestamp(a, b pgtype.Timestamptz) bool {
	if a.Valid != b.Valid || a.InfinityModifier != b.InfinityModifier {
		return false
	}
	return !a.Valid || a.Time.Equal(b.Time)
}
