package risk

import (
	"context" // #nosec G505 - safe for use for k-anonymization of matches
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"go.opentelemetry.io/otel/metric"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/bq"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type FindingBQRow struct {
	ID                     bigquery.NullString    `bigquery:"id"`
	RequestID              bigquery.NullString    `bigquery:"request_id"`
	ChatMessageID          bigquery.NullString    `bigquery:"chat_message_id"`
	ProjectID              bigquery.NullString    `bigquery:"project_id"`
	OrganizationID         bigquery.NullString    `bigquery:"organization_id"`
	RiskPolicyID           bigquery.NullString    `bigquery:"risk_policy_id"`
	RiskPolicyVersion      bigquery.NullInt64     `bigquery:"risk_policy_version"`
	CreatedAt              bigquery.NullTimestamp `bigquery:"created_at"`
	RuleID                 bigquery.NullString    `bigquery:"rule_id"`
	Description            bigquery.NullString    `bigquery:"description"`
	Match                  bigquery.NullString    `bigquery:"match"`
	StartPos               bigquery.NullInt64     `bigquery:"start_pos"`
	EndPos                 bigquery.NullInt64     `bigquery:"end_pos"`
	Tags                   []string               `bigquery:"tags,nullable"`
	Source                 bigquery.NullString    `bigquery:"source"`
	Confidence             bigquery.NullFloat64   `bigquery:"confidence"`
	DeadLetterReason       bigquery.NullString    `bigquery:"dead_letter_reason"`
	FingerprintGlobalHS256 bigquery.NullString    `bigquery:"fingerprint_global_hs256"`
	FingerprintTenantHS256 bigquery.NullString    `bigquery:"fingerprint_tenant_hs256"`
}

type FindingBQWriter struct {
	logger        *slog.Logger
	metrics       *metrics
	table         bq.TableHandle
	features      feature.Provider
	fingerprinter Fingerprinter
}

func NewFindingBQWriter(logger *slog.Logger, meterProvider metric.MeterProvider, table bq.TableHandle, features feature.Provider, fingerprinter Fingerprinter) *FindingBQWriter {
	logger = logger.With(attr.SlogComponent("finding-bq-writer"))
	return &FindingBQWriter{
		logger:        logger,
		metrics:       newMetrics(meterProvider, logger),
		table:         table,
		features:      features,
		fingerprinter: fingerprinter,
	}
}

func (w *FindingBQWriter) HandleBatch(ctx context.Context, messages []*riskv1.Finding, metadata []gcp.MessageMetadata) error {
	logger := w.logger

	captureMatchByOrg := make(map[string]bool)

	items := make([]FindingBQRow, 0, len(messages))
	for _, message := range messages {
		orgID := strings.TrimSpace(message.GetOrganizationId())
		match := message.GetMatch()
		deadLetter := message.GetDeadLetterReason() != ""

		rawCreatedAt := message.GetCreatedAt()
		createdAt, err := time.Parse(time.RFC3339, rawCreatedAt)
		if err != nil {
			logger.ErrorContext(ctx, "finding has invalid rfc3339 timestamp", attr.SlogError(err), attr.SlogValueString(rawCreatedAt))
			continue
		}

		// Compute global sha256
		fgh := sha256.New()
		fingerprintGSHA256 := ""
		if !deadLetter && match != "" {
			if _, err := fmt.Fprintf(fgh, "%s", match); err != nil {
				logger.ErrorContext(ctx, "failed to compute global fingerprint", attr.SlogError(err))
			}
			fingerprintGSHA256 = strings.ToUpper(hex.EncodeToString(fgh.Sum(nil)))
		}

		// Compute tenant-qualified sha256
		fqh := sha256.New()
		fingerprintQSHA256 := ""
		if !deadLetter && orgID != "" && match != "" {
			if _, err := fmt.Fprintf(fqh, "%s:%s", orgID, match); err != nil {
				logger.ErrorContext(ctx, "failed to compute tenant-qualified fingerprint", attr.SlogError(err))
			}
			fingerprintQSHA256 = strings.ToUpper(hex.EncodeToString(fqh.Sum(nil)))
		}

		item := FindingBQRow{
			ID:                     bigquery.NullString{StringVal: message.GetId(), Valid: message.HasId()},
			RequestID:              bigquery.NullString{StringVal: message.GetRequestId(), Valid: message.HasRequestId()},
			ChatMessageID:          bigquery.NullString{StringVal: message.GetChatMessageId(), Valid: message.HasChatMessageId()},
			ProjectID:              bigquery.NullString{StringVal: message.GetProjectId(), Valid: message.HasProjectId()},
			OrganizationID:         bigquery.NullString{StringVal: message.GetOrganizationId(), Valid: message.HasOrganizationId()},
			RiskPolicyID:           bigquery.NullString{StringVal: message.GetRiskPolicyId(), Valid: message.HasRiskPolicyId()},
			RiskPolicyVersion:      bigquery.NullInt64{Int64: message.GetRiskPolicyVersion(), Valid: message.HasRiskPolicyVersion()},
			CreatedAt:              bigquery.NullTimestamp{Timestamp: createdAt, Valid: !createdAt.IsZero()},
			RuleID:                 bigquery.NullString{StringVal: message.GetRuleId(), Valid: message.HasRuleId()},
			Description:            bigquery.NullString{StringVal: message.GetDescription(), Valid: message.HasDescription()},
			StartPos:               bigquery.NullInt64{Int64: int64(message.GetStartPos()), Valid: message.HasStartPos()},
			EndPos:                 bigquery.NullInt64{Int64: int64(message.GetEndPos()), Valid: message.HasEndPos()},
			Tags:                   message.GetTags(),
			Source:                 bigquery.NullString{StringVal: message.GetSource(), Valid: message.HasSource()},
			Confidence:             bigquery.NullFloat64{Float64: message.GetConfidence(), Valid: message.HasConfidence()},
			DeadLetterReason:       bigquery.NullString{StringVal: message.GetDeadLetterReason(), Valid: message.HasDeadLetterReason()},
			FingerprintGlobalHS256: bigquery.NullString{StringVal: fingerprintGSHA256, Valid: fingerprintGSHA256 != ""},
			FingerprintTenantHS256: bigquery.NullString{StringVal: fingerprintQSHA256, Valid: fingerprintQSHA256 != ""},

			// Filled in below
			Match: bigquery.NullString{StringVal: "", Valid: false},
		}

		if orgID != "" {
			captureMatch, cached := captureMatchByOrg[orgID]
			if !cached {
				captureMatch, err = w.features.IsFlagEnabled(ctx, feature.FlagRiskFindingAnalytics, orgID, nil)
				if err != nil {
					captureMatch = false
					logger.ErrorContext(ctx, "failed to check feature flag for risk finding analytics", attr.SlogError(err), attr.SlogValueString(orgID))
				}
				captureMatchByOrg[orgID] = captureMatch
			}
			if captureMatch {
				item.Match = bigquery.NullString{StringVal: match, Valid: match != ""}
			}
		}

		items = append(items, item)
	}

	if len(items) == 0 {
		return nil
	}

	ins := w.table.Inserter(bq.InserterOptions{
		IgnoreUnknownValues: true,
		SkipInvalidRows:     true, // while in shadow mode
	})

	err := ins.Put(ctx, items)
	if err != nil {
		// For now log the error while we are in shadow mode. Eventually, return the error.
		logger.ErrorContext(ctx, "failed to insert batch into bigquery", attr.SlogError(err))
	}

	w.metrics.RecordFindingBQInserts(ctx, len(items), o11y.OutcomeFromError(err))

	return nil
}
