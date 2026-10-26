package catalogpromotion

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	delivery "github.com/speakeasy-api/gram/server/internal/thirdparty/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// evidenceWindow bounds observation to two weeks of the current source configuration.
	evidenceWindow = 14 * 24 * time.Hour
	// minimumSuccesses avoids promoting sources with only a one-off smoke test.
	minimumSuccesses = 10
	// minimumActiveDays requires successful use across more than one UTC day.
	minimumActiveDays = 2
	// recentActivityWindow excludes sources without recent direct usage.
	recentActivityWindow = 48 * time.Hour
	// evidenceLifetime keeps staff approval tied to a recently refreshed health check.
	evidenceLifetime = 15 * time.Minute
	// maxPublicText bounds staff-submitted descriptions sent to public systems.
	maxPublicText = 2000
	// maxPublicURL bounds both reviewed public endpoint and documentation URLs.
	maxPublicURL = 2048
)

var docsSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Service owns source qualification, atomic promotion, and durable deliveries.
type Service struct {
	// db stores evidence, publication receipts, and delivery intent.
	db *pgxpool.Pool
	// registry validates and publishes the public catalogue record.
	registry *mcpregistry.Service
	// features reads uncached consent to telemetry collection.
	features featureReader
	// health supplies observed outcomes for the exact deployment.
	health HealthReader
	// delivery integrates the public guide factory.
	delivery DeliveryClient
	// policy rejects private network destinations before publication.
	policy *guardian.Policy
	// now makes freshness and expiry checks deterministic in tests.
	now func() time.Time
	// effectTimeout bounds a documentation request and supports deterministic deadline tests.
	effectTimeout time.Duration
}

type featureReader interface {
	SnapshotStrict(context.Context, string) (productfeatures.ProductFeaturesSnapshot, error)
}

// New wires independently retryable delivery behind the authoritative Postgres record.
// A nil delivery permits qualification-only use; Dispatch rejects it before claiming jobs.
func New(db *pgxpool.Pool, registry *mcpregistry.Service, features *productfeatures.Client, health HealthReader, delivery DeliveryClient, policy *guardian.Policy) *Service {
	return &Service{db: db, registry: registry, features: features, health: health, delivery: delivery, policy: policy, now: time.Now, effectTimeout: DocsRecordTimeout}
}

// GetCandidate refreshes a canonical source and supplies editable staff-only drafts.
// Suggestions exclude credentials and unsafe endpoints.
func (s *Service) GetCandidate(ctx context.Context, target Target) (Candidate, error) {
	source, err := repo.New(s.db).GetSource(ctx, repo.GetSourceParams{OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, McpServerID: target.MCPServerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("read promotion source: %w", err)
	}
	now := s.now().UTC()
	evidence := s.observe(ctx, source, now)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Candidate{}, fmt.Errorf("begin candidate refresh: %w", err)
	}
	defer o11y.NoLogDefer(func() error {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), receiptTimeout)
		defer cancel()
		return tx.Rollback(cleanupCtx)
	})
	current, err := lockSource(ctx, tx, target)
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("recheck promotion source: %w", err)
	}
	if current.SourceRevision != source.SourceRevision {
		return Candidate{}, ErrStale
	}
	row, err := repo.New(tx).StoreEvidence(ctx, repo.StoreEvidenceParams{
		OrganizationID: target.OrganizationID, ProjectID: uuid.NullUUID{UUID: target.ProjectID, Valid: true}, McpServerID: uuid.NullUUID{UUID: source.McpServerID, Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: source.RemoteMcpServerID, Valid: true}, SourceRevision: source.SourceRevision,
		Eligible: evidence.reason == "eligible", Reason: evidence.reason, ObservedSuccesses: evidence.success, ObservedFailures: evidence.failures,
		ObservedDays: conv.SafeInt32(evidence.days), CheckedAt: conv.ToPGTimestamptz(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = repo.New(tx).GetCandidate(ctx, repo.GetCandidateParams{OrganizationID: target.OrganizationID, ProjectID: uuid.NullUUID{UUID: target.ProjectID, Valid: true}, McpServerID: uuid.NullUUID{UUID: target.MCPServerID, Valid: true}})
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("store promotion evidence: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Candidate{}, fmt.Errorf("commit promotion evidence: %w", err)
	}
	result := s.candidate(row)
	result.SourceName = current.SourceName
	if row.CurationStatus == "active" && !row.PromotedAt.Valid {
		result.SuggestedMetadata = suggestedMetadata(current)
	}
	return result, nil
}

type observation struct {
	reason   string
	success  int64
	failures int64
	days     int
}

func (s *Service) observe(ctx context.Context, source repo.GetSourceRow, now time.Time) observation {
	out := observation{reason: "insufficient_evidence", success: 0, failures: 0, days: 0}
	if source.OrganizationID == constants.DemoOrganizationID {
		out.reason = "unsupported_configuration"
		return out
	}
	endpoint, err := publicURL(source.Url)
	if err != nil {
		out.reason = "unsupported_configuration"
		return out
	}
	exists, err := repo.New(s.db).FindRegistryEndpoint(ctx, endpoint.String())
	if err != nil {
		out.reason = "telemetry_unavailable"
		return out
	}
	if exists {
		out.reason = "already_catalogued"
		return out
	}
	if source.Visibility == "disabled" {
		out.reason = "disabled"
		return out
	}
	if source.HeaderCount != 0 || (source.TransportType != "streamable-http" && source.TransportType != "sse") {
		out.reason = "unsupported_configuration"
		return out
	}
	if s.features == nil || s.health == nil {
		out.reason = "telemetry_unavailable"
		return out
	}
	features, err := s.features.SnapshotStrict(ctx, source.OrganizationID)
	if err != nil {
		out.reason = "telemetry_unavailable"
		return out
	}
	if !features.LogsEnabled {
		out.reason = "logging_disabled"
		return out
	}
	from := now.Add(-evidenceWindow)
	if source.ChangedAt.Time.After(from) {
		from = source.ChangedAt.Time
	}
	target := telemetry.MCPServerTelemetryTarget{ProjectID: source.ProjectID.String(), MCPServerID: source.McpServerID.String(), ToolsetSlug: "", URLSlug: ""}
	outcomes, err := s.health.Outcomes(ctx, target, from, now)
	if err != nil || outcomes == nil {
		out.reason = "telemetry_unavailable"
		return out
	}
	points, err := s.health.Series(ctx, target, from, now, 24*time.Hour)
	if err != nil {
		out.reason = "telemetry_unavailable"
		return out
	}
	recent := outcomes
	if cutoff := now.Add(-recentActivityWindow); from.Before(cutoff) {
		recent, err = s.health.Outcomes(ctx, target, cutoff, now)
		if err != nil || recent == nil {
			out.reason = "telemetry_unavailable"
			return out
		}
	}
	return qualify(outcomes, points, recent)
}

func qualify(outcomes *telemetry.MCPServerOutcomes, points []telemetry.MCPServerSeriesPoint, recent *telemetry.MCPServerOutcomes) observation {
	out := observation{reason: "insufficient_evidence", success: outcomes.Success, failures: 0, days: 0}
	if outcomes.Success < 0 || recent.Success < 0 {
		out.reason = "telemetry_unavailable"
		return out
	}
	for _, counts := range []*telemetry.MCPServerOutcomes{outcomes, recent} {
		var failures int64
		for _, count := range []int64{counts.Unauthorized, counts.ClientError, counts.ServerError, counts.Blocked, counts.Failed, counts.Unknown} {
			if count < 0 || count > int64(^uint64(0)>>1)-failures {
				out.reason = "telemetry_unavailable"
				return out
			}
			failures += count
		}
		out.failures = max(out.failures, failures)
	}
	var seriesFailures int64
	days := map[string]bool{}
	for _, point := range points {
		if point.Total < 0 || point.Failed < 0 || point.Failed > point.Total {
			out.reason = "telemetry_unavailable"
			return out
		}
		if point.Failed > int64(^uint64(0)>>1)-seriesFailures {
			out.reason = "telemetry_unavailable"
			return out
		}
		seriesFailures += point.Failed
		if point.Total > point.Failed {
			days[point.BucketStart.UTC().Format(time.DateOnly)] = true
		}
	}
	out.days = len(days)
	out.failures = max(out.failures, seriesFailures)
	if out.failures > 0 {
		out.reason = "errors_observed"
		return out
	}
	if out.success >= minimumSuccesses && out.days >= minimumActiveDays && recent.Success > 0 {
		out.reason = "eligible"
	}
	return out
}

// candidateCursor pins ordering and filter, so changing tabs cannot reuse another queue's cursor.
type candidateCursor struct {
	// CreatedAt is the immutable ordering timestamp.
	CreatedAt time.Time `json:"created_at"`
	// ID breaks timestamp ties deterministically.
	ID uuid.UUID `json:"id"`
	// Status binds pagination to the selected queue.
	Status string `json:"status"`
}

// ListCandidates returns a bounded staff-only page in newest-created order.
func (s *Service) ListCandidates(ctx context.Context, cursor string, limit int, status string) ([]Candidate, string, error) {
	if limit == 0 {
		limit = 25
	}
	if status == "" {
		status = "active"
	}
	switch status {
	case "active", "all", "candidate", "rejected", "archived", "promoted":
	default:
		return nil, "", ErrInvalid
	}
	if limit < 1 || limit > 50 || len(cursor) > 2048 {
		return nil, "", ErrInvalid
	}
	var position candidateCursor
	if cursor != "" {
		encoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(encoded, &position) != nil || position.ID == uuid.Nil || position.CreatedAt.IsZero() || position.Status != status {
			return nil, "", ErrInvalid
		}
	}
	rows, err := repo.New(s.db).ListCandidates(ctx, repo.ListCandidatesParams{HasCursor: cursor != "", CursorID: position.ID, CursorCreatedAt: conv.ToPGTimestamptz(position.CreatedAt), PageLimit: conv.SafeInt32(limit + 1), StatusFilter: status})
	if err != nil {
		return nil, "", fmt.Errorf("list promotion candidates: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1].RegistryPromotionCandidate
		encoded, err := json.Marshal(candidateCursor{CreatedAt: last.CreatedAt.Time, ID: last.ID, Status: status})
		if err != nil {
			return nil, "", fmt.Errorf("encode promotion cursor: %w", err)
		}
		next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	result := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		view := s.candidate(row.RegistryPromotionCandidate)
		view.SourceName = row.SourceName
		result = append(result, view)
	}
	return result, next, nil
}

// lockSource fences attachments and header edits before reading their revision.
func lockSource(ctx context.Context, tx pgx.Tx, target Target) (repo.GetSourceRow, error) {
	q := repo.New(tx)
	params := repo.GetSourceParams{OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, McpServerID: target.MCPServerID}
	if _, err := q.LockSource(ctx, repo.LockSourceParams(params)); err != nil {
		return repo.GetSourceRow{}, fmt.Errorf("lock source rows: %w", err)
	}
	if err := q.LockSourceHeaders(ctx, repo.LockSourceHeadersParams(params)); err != nil {
		return repo.GetSourceRow{}, fmt.Errorf("lock source headers: %w", err)
	}
	if err := q.LockSourceAuthClients(ctx, repo.LockSourceAuthClientsParams{OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, McpServerID: target.MCPServerID}); err != nil {
		return repo.GetSourceRow{}, fmt.Errorf("lock source auth clients: %w", err)
	}
	if err := q.LockSourceAuthClientHeaders(ctx, repo.LockSourceAuthClientHeadersParams{OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, McpServerID: target.MCPServerID}); err != nil {
		return repo.GetSourceRow{}, fmt.Errorf("lock source auth headers: %w", err)
	}
	source, err := q.GetSource(ctx, params)
	if err != nil {
		return repo.GetSourceRow{}, fmt.Errorf("read locked source: %w", err)
	}
	return source, nil
}

// ValidateTx locks the source and candidate and rejects stale or insufficient evidence.
func (s *Service) ValidateTx(ctx context.Context, tx pgx.Tx, target Target, revision, curationRevision string, public PublicMetadata) (Candidate, error) {
	source, err := lockSource(ctx, tx, target)
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("lock promotion source: %w", err)
	}
	row, err := repo.New(tx).GetCandidate(ctx, repo.GetCandidateParams{OrganizationID: target.OrganizationID, ProjectID: uuid.NullUUID{UUID: target.ProjectID, Valid: true}, McpServerID: uuid.NullUUID{UUID: target.MCPServerID, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("lock promotion candidate: %w", err)
	}
	if row.PromotedAt.Valid || row.CurationStatus != "active" {
		return s.candidate(row), ErrConflict
	}
	if row.CurationRevision.String() != curationRevision || revision != source.SourceRevision || revision != row.SourceRevision || !row.CheckedAt.Valid || s.now().Sub(row.CheckedAt.Time) > evidenceLifetime {
		return Candidate{}, ErrStale
	}
	if !row.Eligible || source.Visibility == "disabled" || source.HeaderCount != 0 {
		return Candidate{}, ErrIneligible
	}
	if err := validatePublic(public); err != nil {
		return Candidate{}, err
	}
	record, err := buildRecord(public, source.TransportType)
	if err != nil || len(s.registry.Validate(record)) > 0 {
		return Candidate{}, ErrInvalid
	}
	sourceURL, err := publicURL(source.Url)
	if err != nil {
		return Candidate{}, ErrInvalid
	}
	reviewedURL, _ := publicURL(public.URL)
	if sourceURL.String() != reviewedURL.String() {
		return Candidate{}, ErrInvalid
	}
	// These DNS checks never fetch source content or send customer credentials.
	for _, raw := range []string{public.URL, public.DocumentationURL} {
		if _, err := s.policy.ValidateHTTPSURL(ctx, raw); err != nil {
			var dnsErr *net.DNSError
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &dnsErr) && (dnsErr.Timeout() || dnsErr.Temporary())) {
				return Candidate{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
			}
			return Candidate{}, ErrInvalid
		}
	}
	return s.candidate(row), nil
}

// RequestDigest binds a retry key to the exact canonical target and reviewed input.
func RequestDigest(request Request) (string, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("encode promotion request: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// PromoteTx commits publication, receipt, delivery intent, and audit in one transaction.
func (s *Service) PromoteTx(ctx context.Context, tx pgx.Tx, request Request, actor string) (Candidate, error) {
	if !request.PublicMetadataConfirmed || len(request.RetryKey) == 0 || len(request.RetryKey) > 128 {
		return Candidate{}, ErrInvalid
	}
	digest, err := RequestDigest(request)
	if err != nil {
		return Candidate{}, err
	}
	// Lock the source first, matching refresh and approval lock order.
	_, err = repo.New(tx).GetSource(ctx, repo.GetSourceParams{OrganizationID: request.Target.OrganizationID, ProjectID: request.Target.ProjectID, McpServerID: request.Target.MCPServerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("read promotion target: %w", err)
	}
	prior, err := repo.New(tx).GetCandidate(ctx, repo.GetCandidateParams{OrganizationID: request.Target.OrganizationID, ProjectID: uuid.NullUUID{UUID: request.Target.ProjectID, Valid: true}, McpServerID: uuid.NullUUID{UUID: request.Target.MCPServerID, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("read promotion target: %w", err)
	}
	if prior.PromotedAt.Valid {
		if prior.RequestDigest.String == digest && prior.RetryKey.String == request.RetryKey {
			return s.candidate(prior), nil
		}
		return Candidate{}, ErrConflict
	}
	if _, err := s.ValidateTx(ctx, tx, request.Target, request.SourceRevision, request.CurationRevision, request.Public); err != nil {
		return Candidate{}, err
	}
	principal, err := urn.ParsePrincipal(actor)
	if err != nil {
		return Candidate{}, ErrInvalid
	}
	// Serialize promotion identities before checking catalogue endpoints. The unique
	// indexes also protect concurrent requests that use different source deployments.
	if err := repo.New(tx).LockEndpoint(ctx, request.Public.URL); err != nil {
		return Candidate{}, fmt.Errorf("lock catalogue endpoint: %w", err)
	}
	exists, err := repo.New(tx).FindRegistryEndpoint(ctx, request.Public.URL)
	if err != nil {
		return Candidate{}, fmt.Errorf("check catalogue endpoint: %w", err)
	}
	if exists {
		return Candidate{}, ErrConflict
	}
	source, err := repo.New(tx).GetSource(ctx, repo.GetSourceParams{OrganizationID: request.Target.OrganizationID, ProjectID: request.Target.ProjectID, McpServerID: request.Target.MCPServerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("read promotion target: %w", err)
	}
	record, err := buildRecord(request.Public, source.TransportType)
	if err != nil {
		return Candidate{}, err
	}
	entry, err := s.registry.CreateTx(ctx, tx, record)
	if errors.Is(err, mcpregistry.ErrConflict) {
		return Candidate{}, ErrConflict
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("publish reviewed catalogue entry: %w", err)
	}
	publicJSON, err := json.Marshal(request.Public)
	if err != nil {
		return Candidate{}, fmt.Errorf("encode public promotion metadata: %w", err)
	}
	row, err := repo.New(tx).PromoteCandidate(ctx, repo.PromoteCandidateParams{
		OrganizationID: request.Target.OrganizationID, RegistryEntryID: uuid.NullUUID{UUID: entry.ID, Valid: true}, PublicMetadata: publicJSON, EndpointIdentity: conv.ToPGText(request.Public.URL),
		RequestDigest: conv.ToPGText(digest), RetryKey: conv.ToPGText(request.RetryKey), PromotedBy: conv.ToPGText(actor), ID: prior.ID, ProjectID: uuid.NullUUID{UUID: request.Target.ProjectID, Valid: true},
	})
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == pgerrcode.UniqueViolation {
		return Candidate{}, ErrConflict
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("record catalogue promotion: %w", err)
	}
	if err := audit.NewLogger().LogMcpServerPromoteCatalogue(ctx, tx, audit.LogMcpServerPromoteCatalogueEvent{
		OrganizationID: request.Target.OrganizationID, ProjectID: request.Target.ProjectID, Actor: principal, McpServerURN: urn.NewMcpServer(request.Target.MCPServerID),
	}); err != nil {
		return Candidate{}, fmt.Errorf("audit catalogue promotion: %w", err)
	}
	return s.candidate(row), nil
}

func validatePublic(public PublicMetadata) error {
	if len(public.Title) > 200 || strings.ContainsAny(public.Title, "\r\n") {
		return ErrInvalid
	}
	if public.Name != strings.TrimSpace(public.Name) || public.Title != strings.TrimSpace(public.Title) || public.Description != strings.TrimSpace(public.Description) {
		return ErrInvalid
	}
	for _, value := range []string{public.Name, public.Title, public.Description} {
		if value == "" || len(value) > maxPublicText || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r") {
			return ErrInvalid
		}
	}
	if len(public.DocsSlug) > 80 || !docsSlugPattern.MatchString(public.DocsSlug) || (public.AuthMode != "none" && public.AuthMode != "oauth") {
		return ErrInvalid
	}
	endpoint, err := publicURL(public.URL)
	if err != nil || endpoint.String() != public.URL {
		return ErrInvalid
	}
	documentation, err := publicURL(public.DocumentationURL)
	if err != nil || documentation.String() != public.DocumentationURL {
		return ErrInvalid
	}
	return nil
}

func publicURL(raw string) (*url.URL, error) {
	if delivery.ValidatePublicURL(raw) != nil {
		return nil, ErrInvalid
	}
	parsed, err := url.Parse(raw)
	if err != nil || raw != strings.TrimSpace(raw) || len(raw) > maxPublicURL {
		return nil, ErrInvalid
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, ErrInvalid
	}
	host := strings.ToLower(parsed.Hostname())

	if parsed.Port() != "" && parsed.Port() != "443" {
		return nil, ErrInvalid
	}
	parsed.Host = host
	return parsed, nil
}

type registryRecord struct {
	// Server contains only the reviewed public MCP record.
	Server registryServer `json:"server"`
}
type registryServer struct {
	// Schema identifies the pinned public record schema.
	Schema string `json:"$schema"`
	// Name is the immutable catalogue identity.
	Name string `json:"name"`
	// Title is the public display name.
	Title string `json:"title"`
	// Description is reviewed public prose.
	Description string `json:"description"`
	// Version marks the initial reviewed catalogue record.
	Version string `json:"version"`
	// WebsiteURL links the primary setup reference.
	WebsiteURL string `json:"websiteUrl"`
	// Remotes contains the reviewed public transport endpoint.
	Remotes []registryRemote `json:"remotes"`
}
type registryRemote struct {
	// Type preserves the observed upstream transport.
	Type string `json:"type"`
	// URL is the reviewed public endpoint without source authentication data.
	URL string `json:"url"`
}

func buildRecord(public PublicMetadata, transport string) (json.RawMessage, error) {
	encoded, err := json.Marshal(registryRecord{Server: registryServer{Schema: "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json", Name: public.Name, Title: public.Title, Description: public.Description, Version: "1.0.0", WebsiteURL: public.DocumentationURL, Remotes: []registryRemote{{Type: transport, URL: public.URL}}}})
	if err != nil {
		return nil, fmt.Errorf("encode public catalogue record: %w", err)
	}
	return encoded, nil
}

func (s *Service) candidate(row repo.RegistryPromotionCandidate) Candidate {
	var out Candidate
	out.ID = row.ID.String()
	out.CreatedAt = row.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
	out.CurationRevision = row.CurationRevision.String()
	out.OrganizationID = row.OrganizationID
	if row.ProjectID.Valid {
		out.ProjectID = row.ProjectID.UUID.String()
	}
	if row.McpServerID.Valid {
		out.MCPServerID = row.McpServerID.UUID.String()
	}
	if row.RemoteMcpServerID.Valid {
		out.RemoteMCPServerID = row.RemoteMcpServerID.UUID.String()
	}
	out.SourceRevision = row.SourceRevision
	out.Eligible = row.Eligible
	out.Reason = row.Reason
	out.Status = "candidate"
	if row.CurationStatus != "active" {
		out.Status = row.CurationStatus
	}
	out.ObservedSuccesses = row.ObservedSuccesses
	out.ObservedFailures = row.ObservedFailures
	out.ObservedDays = int(row.ObservedDays)
	out.CheckedAt = row.CheckedAt.Time.UTC().Format(time.RFC3339)
	out.DocsRevision = row.DocsRevision.String()
	out.DocsAttempts = int(row.DocsAttempts)
	out.DeliveryError = row.DeliveryError.String
	if row.PromotedAt.Valid {
		out.Status = "promoted"
		out.Eligible = false
		out.Reason = "already_promoted"
		out.PromotedAt = row.PromotedAt.Time.UTC().Format(time.RFC3339)
		if row.RegistryEntryID.Valid {
			out.RegistryEntryID = row.RegistryEntryID.UUID.String()
		}
		var public PublicMetadata
		if json.Unmarshal(row.PublicMetadata, &public) == nil {
			out.PublicTitle = public.Title
		}
		out.DocsStatus = "pending"
		if row.DocsAttempts > 0 {
			out.DocsStatus = "retrying"
		}
		if row.DocsLastAttemptAt.Valid {
			out.DocsLastAttemptAt = row.DocsLastAttemptAt.Time.UTC().Format(time.RFC3339)
		}
		if row.DocsNextAttemptAt.Valid {
			out.DocsNextAttemptAt = row.DocsNextAttemptAt.Time.UTC().Format(time.RFC3339)
		}
		if row.DocsNeedsAttention {
			out.DocsStatus = "needs_attention"
		}
		out.DocsRetryAllowed = row.DocsAttempts > 0 && !row.DocsQueuedAt.Valid && (!row.DocsLeaseUntil.Valid || !row.DocsLeaseUntil.Time.After(s.now())) && (!row.DocsNextAttemptAt.Valid || !row.DocsNextAttemptAt.Time.After(s.now()))
		if row.DocsQueuedAt.Valid {
			out.DocsStatus = "queued"
			out.DocsURL = row.DocsUrl.String
			out.DocsQueuedAt = row.DocsQueuedAt.Time.UTC().Format(time.RFC3339)
		}
	}
	return out
}
