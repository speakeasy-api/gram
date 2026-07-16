package risk

import (
	"context"
	"encoding/base64"
	"log/slog"
	"math"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
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
}

func NewFindingCHWriter(logger *slog.Logger, meterProvider metric.MeterProvider, inserter RiskFindingInserter, fingerprinter Fingerprinter) *FindingCHWriter {
	logger = logger.With(attr.SlogComponent("finding-ch-writer"))
	return &FindingCHWriter{
		logger:        logger,
		metrics:       newMetrics(meterProvider, logger),
		inserter:      inserter,
		fingerprinter: fingerprinter,
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

		createdAt, err := time.Parse(time.RFC3339, message.GetCreatedAt())
		if err != nil {
			logger.ErrorContext(ctx, "finding has invalid rfc3339 timestamp", attr.SlogError(err), attr.SlogValueString(message.GetCreatedAt()))
			w.metrics.RecordFindingSkipped(ctx, "invalid_timestamp")
			continue
		}

		// Compute global hmac-sha256.
		globalHS256 := ""
		if !deadLetter && match != "" {
			if sum, _, err := w.fingerprinter.HS256([]byte(match)); err != nil {
				logger.ErrorContext(ctx, "failed to compute global fingerprint", attr.SlogError(err))
			} else {
				globalHS256 = base64.RawURLEncoding.EncodeToString(sum)
			}
		}

		// Compute tenant-qualified hmac-sha256.
		pepperVersion := ""
		tenantHS256 := ""
		if !deadLetter && orgID != "" && match != "" {
			if sum, pepperver, err := w.fingerprinter.TenantedHS256(orgID, []byte(match), WithKeyCache(tenantKeyCache)); err != nil {
				logger.ErrorContext(ctx, "failed to compute tenant-qualified fingerprint", attr.SlogError(err))
			} else {
				pepperVersion = pepperver
				tenantHS256 = base64.RawURLEncoding.EncodeToString(sum)
			}
		}

		// Precompute the redacted display string. Every source is redacted here
		// including shadow_mcp and account_identity — CH must never hold a
		// plaintext match or PII. A dead-letter sentinel has no match, so its
		// redaction stays empty.
		matchRedacted := ""
		matchLen := uint32(0)
		if !deadLetter && match != "" {
			matchRedacted = fingerprintRedactedMatch(orgID, match)
			if n := len(match); n > math.MaxUint32 {
				matchLen = math.MaxUint32
			} else {
				matchLen = uint32(n)
			}
		}

		tags := message.GetTags()
		if tags == nil {
			tags = []string{}
		}

		rows = append(rows, chrepo.RiskFindingRow{
			ID:                       message.GetId(),
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
