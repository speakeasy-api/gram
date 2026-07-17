package risk

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"go.opentelemetry.io/otel/metric"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// RiskFindingInserter writes a batch of findings to ClickHouse. *chrepo.Queries
// satisfies it; tests supply a fake.
type RiskFindingInserter interface {
	InsertRiskFindings(ctx context.Context, rows []chrepo.RiskFindingRow) error
}

// FindingCHWriter consumes Finding messages off the shared Pub/Sub topic and
// writes them to the ClickHouse risk_findings table. Unlike FindingBQWriter it
// never stores the raw matched value: only its length, a redacted display
// string, and one-way fingerprints. The verbatim value stays in Postgres for
// the audited unmask path.
type FindingCHWriter struct {
	logger        *slog.Logger
	metrics       *metrics
	inserter      RiskFindingInserter
	fingerprinter Fingerprinter

	exclusionsCache *expirable.LRU[string, risk_analysis.ExclusionSet]
	exclusionsDB    repo.DBTX
}

const (
	exclusionsSetCacheSize = 1000
	exclusionsSetCacheTTL  = time.Minute
)

func NewFindingCHWriter(logger *slog.Logger, exclusionsDB repo.DBTX, meterProvider metric.MeterProvider, inserter RiskFindingInserter, fingerprinter Fingerprinter) *FindingCHWriter {
	logger = logger.With(attr.SlogComponent("finding-ch-writer"))
	return &FindingCHWriter{
		logger:          logger,
		metrics:         newMetrics(meterProvider, logger),
		inserter:        inserter,
		fingerprinter:   fingerprinter,
		exclusionsDB:    exclusionsDB,
		exclusionsCache: expirable.NewLRU[string, risk_analysis.ExclusionSet](exclusionsSetCacheSize, nil, exclusionsSetCacheTTL),
	}
}

func (w *FindingCHWriter) HandleBatch(ctx context.Context, messages []*riskv1.Finding, _ []gcp.MessageMetadata) error {
	logger := w.logger

	// Cache per-tenant derived keys for the lifetime of this batch so repeated
	// findings from the same org don't each re-run HKDF.
	tenantKeyCache := make(map[string][]byte)

	rows := make([]chrepo.RiskFindingRow, 0, len(messages))
	for _, message := range messages {
		orgID := strings.TrimSpace(message.GetOrganizationId())
		match := message.GetMatch()
		deadLetter := message.GetDeadLetterReason() != ""

		// The id maps to a ClickHouse UUID column. Parse it here so a malformed
		// or missing id skips only that finding rather than failing the binding
		// for the whole multi-row batch insert.
		id, err := uuid.Parse(message.GetId())
		if err != nil {
			logger.ErrorContext(ctx, "finding has invalid uuid id", attr.SlogError(err), attr.SlogValueString(message.GetId()))
			w.metrics.RecordFindingCHSkipped(ctx, "invalid_id")
			continue
		}

		createdAt, err := time.Parse(time.RFC3339, message.GetCreatedAt())
		if err != nil {
			logger.ErrorContext(ctx, "finding has invalid rfc3339 timestamp", attr.SlogError(err), attr.SlogValueString(message.GetCreatedAt()))
			w.metrics.RecordFindingCHSkipped(ctx, "invalid_timestamp")
			continue
		}

		// Annotate findings suppressed by a going-forward exclusion instead of
		// dropping them, so excluded findings stay auditable and filterable at
		// read time. The shadow scan path that feeds this writer does not apply
		// exclusions, so we mirror the Postgres path here. Dead-letter sentinels
		// carry no rule/match to match against, so they bypass the check.
		var excludedAt *time.Time
		var exclusionID *uuid.UUID
		if !deadLetter {
			if exID, ok := w.matchedExclusion(ctx, message); ok {
				now := time.Now().UTC()
				excludedAt = &now
				exclusionID = &exID
				w.metrics.RecordFindingCHExcluded(ctx)
			}
		}

		// Compute the fingerprints. Keep the raw HMAC bytes around so the
		// redacted display string can reuse a keyed prefix (see below) instead of
		// an unkeyed hash. pepperVersion is captured from whichever fingerprint
		// runs so a global-only finding still records the version needed to
		// interpret it after a pepper rotation.
		pepperVersion := ""

		var globalSum []byte
		globalHS256 := ""
		if !deadLetter && match != "" {
			if sum, pepperver, err := w.fingerprinter.HS256([]byte(match)); err != nil {
				logger.ErrorContext(ctx, "failed to compute global fingerprint", attr.SlogError(err))
			} else {
				globalSum = sum
				globalHS256 = base64.RawURLEncoding.EncodeToString(sum)
				pepperVersion = pepperver
			}
		}

		var tenantSum []byte
		tenantHS256 := ""
		if !deadLetter && orgID != "" && match != "" {
			if sum, pepperver, err := w.fingerprinter.TenantedHS256(orgID, []byte(match), WithKeyCache(tenantKeyCache)); err != nil {
				logger.ErrorContext(ctx, "failed to compute tenant-qualified fingerprint", attr.SlogError(err))
			} else {
				tenantSum = sum
				tenantHS256 = base64.RawURLEncoding.EncodeToString(sum)
				pepperVersion = pepperver
			}
		}

		// Precompute the redacted display string. Every source is redacted here
		// including shadow_mcp and account_identity — CH must never hold a
		// plaintext match or PII. The disambiguator is a prefix of the keyed HMAC
		// fingerprint (tenant-qualified when available, else global) rather than
		// an unkeyed SHA-256, so a low-entropy match (e.g. an email) can't be
		// recovered offline from the stored value. A dead-letter sentinel has no
		// match, so its redaction stays empty.
		matchRedacted := ""
		matchLen := uint32(0)
		if !deadLetter && match != "" {
			if n := len(match); n > math.MaxUint32 {
				matchLen = math.MaxUint32
			} else {
				matchLen = uint32(n)
			}
			displaySum := tenantSum
			if displaySum == nil {
				displaySum = globalSum
			}
			if len(displaySum) >= 4 {
				matchRedacted = fmt.Sprintf("<redacted len=%d sha=%s>", matchLen, hex.EncodeToString(displaySum[:4]))
			} else {
				matchRedacted = fmt.Sprintf("<redacted len=%d>", matchLen)
			}
		}

		tags := message.GetTags()
		if tags == nil {
			tags = []string{}
		}

		rows = append(rows, chrepo.RiskFindingRow{
			ID:                       id,
			CreatedAt:                createdAt.UTC(),
			OrganizationID:           message.GetOrganizationId(),
			ProjectID:                message.GetProjectId(),
			RequestID:                message.GetRequestId(),
			ChatMessageID:            message.GetChatMessageId(),
			RiskPolicyID:             message.GetRiskPolicyId(),
			RiskPolicyVersion:        message.GetRiskPolicyVersion(),
			RuleID:                   message.GetRuleId(),
			Description:              message.GetDescription(),
			Source:                   message.GetSource(),
			Confidence:               message.GetConfidence(),
			Tags:                     tags,
			StartPos:                 message.GetStartPos(),
			EndPos:                   message.GetEndPos(),
			DeadLetterReason:         message.GetDeadLetterReason(),
			MatchLen:                 matchLen,
			MatchRedacted:            matchRedacted,
			FingerprintPepperVersion: pepperVersion,
			FingerprintGlobalHS256:   globalHS256,
			FingerprintTenantHS256:   tenantHS256,
			ExcludedAt:               excludedAt,
			ExclusionID:              exclusionID,
		})
	}

	if len(rows) == 0 {
		return nil
	}

	err := w.inserter.InsertRiskFindings(ctx, rows)
	if err != nil {
		// Log the error while in shadow mode rather than returning it, matching
		// the BigQuery writer — a failed analytics insert must not nack and
		// redrive the finding.
		logger.ErrorContext(ctx, "failed to insert batch into clickhouse", attr.SlogError(err))
	}

	w.metrics.RecordFindingCHInserts(ctx, len(rows), o11y.OutcomeFromError(err))

	return nil
}

// matchedExclusion returns the id of the going-forward exclusion that
// suppresses the finding, and whether one matched, reusing the same matching
// logic (ExclusionSet) as the Postgres scan path.
func (w *FindingCHWriter) matchedExclusion(ctx context.Context, message *riskv1.Finding) (uuid.UUID, bool) {
	set := w.exclusionSetFor(ctx, message.GetProjectId(), message.GetRiskPolicyId())
	if set.Empty() {
		return uuid.UUID{}, false
	}
	// ExclusionSet.ExcludedBy matches on RuleID, Source and Match only; the
	// remaining fields are set for completeness (exhaustruct) but unused.
	return set.ExcludedBy(scanners.Finding{
		RuleID:              message.GetRuleId(),
		Description:         message.GetDescription(),
		Match:               message.GetMatch(),
		StartPos:            int(message.GetStartPos()),
		EndPos:              int(message.GetEndPos()),
		Tags:                message.GetTags(),
		Source:              message.GetSource(),
		Confidence:          message.GetConfidence(),
		DeadLetterReason:    message.GetDeadLetterReason(),
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	})
}

// exclusionSetFor resolves the enabled exclusions (the policy's own plus every
// global one) that apply to a finding's policy, cached per (project, policy)
// with a TTL so exclusion edits take effect within exclusionsSetCacheTTL
// without a Postgres read per batch.
//
// Fail-open: an empty/unparseable project or policy id, or a lookup error,
// returns an empty set (nothing excluded) rather than dropping findings. On a
// lookup error the result is not cached, so the next batch retries.
func (w *FindingCHWriter) exclusionSetFor(ctx context.Context, projectID, policyID string) risk_analysis.ExclusionSet {
	if projectID == "" || policyID == "" {
		return risk_analysis.ExclusionSet{}
	}

	key := projectID + "#" + policyID
	if set, ok := w.exclusionsCache.Get(key); ok {
		return set
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		w.logger.ErrorContext(ctx, "finding has invalid project id", attr.SlogError(err), attr.SlogValueString(projectID))
		return risk_analysis.ExclusionSet{}
	}
	policyUUID, err := uuid.Parse(policyID)
	if err != nil {
		w.logger.ErrorContext(ctx, "finding has invalid risk policy id", attr.SlogError(err), attr.SlogValueString(policyID))
		return risk_analysis.ExclusionSet{}
	}

	exclusions, err := repo.New(w.exclusionsDB).ListEnabledExclusionsForPolicy(ctx, repo.ListEnabledExclusionsForPolicyParams{
		ProjectID:    projectUUID,
		RiskPolicyID: uuid.NullUUID{UUID: policyUUID, Valid: true},
	})
	if err != nil {
		w.logger.ErrorContext(ctx, "list exclusions for policy", attr.SlogError(err), attr.SlogValueString(policyID))
		return risk_analysis.ExclusionSet{}
	}

	set := risk_analysis.NewExclusionSet(exclusions)
	w.exclusionsCache.Add(key, set)
	return set
}
