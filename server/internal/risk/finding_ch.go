package risk

import (
	"context"
	"encoding/base64"
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
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/maskdisplay"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// RiskFindingInserter writes a batch of findings to ClickHouse. *chrepo.Queries
// satisfies it; tests supply a fake.
type RiskFindingInserter interface {
	InsertRiskFindings(ctx context.Context, rows []chrepo.RiskFindingRow) error
}

// FindingCHWriter consumes Finding messages off the shared Pub/Sub topic and
// writes them to the ClickHouse risk_findings table. It never stores the raw
// matched value: only its length, a partial-mask display string (maskdisplay),
// and one-way fingerprints. The verbatim value stays in Postgres for the
// audited unmask path.
type FindingCHWriter struct {
	logger        *slog.Logger
	metrics       *metrics
	inserter      RiskFindingInserter
	fingerprinter Fingerprinter

	exclusionsCache *expirable.LRU[string, risk_analysis.ExclusionSet]

	// db backs the per-batch Postgres reads: exclusion sets and chat message
	// attribution. A read replica is fine — both are best-effort enrichment.
	db repo.DBTX
}

const (
	exclusionsSetCacheSize = 1000
	exclusionsSetCacheTTL  = time.Minute
)

func NewFindingCHWriter(logger *slog.Logger, db repo.DBTX, meterProvider metric.MeterProvider, inserter RiskFindingInserter, fingerprinter Fingerprinter) *FindingCHWriter {
	logger = logger.With(attr.SlogComponent("finding-ch-writer"))
	return &FindingCHWriter{
		logger:          logger,
		metrics:         newMetrics(meterProvider, logger),
		inserter:        inserter,
		fingerprinter:   fingerprinter,
		db:              db,
		exclusionsCache: expirable.NewLRU[string, risk_analysis.ExclusionSet](exclusionsSetCacheSize, nil, exclusionsSetCacheTTL),
	}
}

func (w *FindingCHWriter) HandleBatch(ctx context.Context, messages []*riskv1.Finding, _ []gcp.MessageMetadata) error {
	logger := w.logger

	// Cache per-tenant derived keys for the lifetime of this batch so repeated
	// findings from the same org don't each re-run HKDF.
	tenantKeyCache := make(map[string][]byte)

	// Batch-resolve the denormalized attribution (chat id, user ids) for every
	// finding that carries a well-formed anchor — one Postgres query per anchor
	// kind, best-effort.
	messageAttribution := w.chatMessageAttribution(ctx, messages)
	contentPartAttribution := w.chatContentPartAttribution(ctx, messages)

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

		// Compute the fingerprints. pepperVersion is captured from whichever
		// fingerprint runs so a global-only finding still records the version
		// needed to interpret it after a pepper rotation.
		pepperVersion := ""

		globalHS256 := ""
		if !deadLetter && match != "" {
			if sum, pepperver, err := w.fingerprinter.HS256([]byte(match)); err != nil {
				logger.ErrorContext(ctx, "failed to compute global fingerprint", attr.SlogError(err))
			} else {
				globalHS256 = base64.RawURLEncoding.EncodeToString(sum)
				pepperVersion = pepperver
			}
		}

		tenantHS256 := ""
		if !deadLetter && orgID != "" && match != "" {
			if sum, pepperver, err := w.fingerprinter.TenantedHS256(orgID, []byte(match), WithKeyCache(tenantKeyCache)); err != nil {
				logger.ErrorContext(ctx, "failed to compute tenant-qualified fingerprint", attr.SlogError(err))
			} else {
				tenantHS256 = base64.RawURLEncoding.EncodeToString(sum)
				pepperVersion = pepperver
			}
		}

		// Precompute the partial-mask display string via the shared maskdisplay
		// package, so live rows and the offline backfill stay byte-identical for
		// the same value. A dead-letter sentinel has no match, so its display
		// stays empty.
		matchRedacted := ""
		matchLen := uint32(0)
		if !deadLetter && match != "" {
			if n := len(match); n > math.MaxUint32 {
				matchLen = math.MaxUint32
			} else {
				matchLen = uint32(n)
			}
			matchRedacted = maskdisplay.Display(message.GetSource(), message.GetRuleId(), match)
		}

		tags := message.GetTags()
		if tags == nil {
			tags = []string{}
		}

		chatMessageID := message.GetChatMessageId()
		contentPartID := message.GetContentPartId()

		// Attribution is an enrichment: findings whose anchor is absent,
		// malformed, or unresolved keep empty strings rather than being dropped.
		// message_created_at falls back to the finding's own scan time so the
		// listing sort key is never zero, mirroring the column's DEFAULT for
		// pre-column rows.
		var chatID, userID, externalUserID, assistantID, chatSource, team, userEmail string
		messageCreatedAt := createdAt.UTC()
		if msgID, err := uuid.Parse(chatMessageID); err == nil {
			if a, ok := messageAttribution[msgID]; ok {
				chatID = a.ChatID.String()
				userID = a.UserID
				externalUserID = a.ExternalUserID
				chatSource = chat.CanonicalSource(a.ChatSource)
				team = a.Team
				userEmail = a.UserEmail
				if a.AssistantID != uuid.Nil {
					assistantID = a.AssistantID.String()
				}
				if a.MessageCreatedAt.Valid {
					messageCreatedAt = a.MessageCreatedAt.Time.UTC()
				}
			}
		} else if partID, err := uuid.Parse(contentPartID); err == nil {
			// Only attribute a part that belongs to the finding's own project,
			// so a wrong or forged anchor id cannot pull another tenant's chat
			// and user ids into this row. A NULL project_id (project deleted)
			// is unverifiable and so gets no attribution. The part attribution
			// query carries no assistant link or message timestamp, so both keep
			// the fallbacks above.
			if a, ok := contentPartAttribution[partID]; ok && a.ProjectID.Valid && a.ProjectID.UUID.String() == message.GetProjectId() {
				chatID = a.ChatID.String()
				userID = a.UserID
				externalUserID = a.ExternalUserID
				chatSource = chat.CanonicalSource(a.ChatSource)
				team = a.Team
				userEmail = a.UserEmail
			}
		}

		// Dead-letter sentinels carry no meaningful (source, rule_id), so they
		// get no category instead of the classifier's "custom" fallback.
		category := ""
		if !deadLetter {
			category = string(categories.Classify(message.GetSource(), message.GetRuleId()))
		}

		// Set only on messages republished by risk.markResultsFalsePositive /
		// risk.unmarkResultsFalsePositive to append a state-change row for an
		// already-persisted finding (see mirrorFalsePositiveToClickHouse). Empty
		// on every finding a scanner produces. A parse failure must skip the
		// message rather than fall through with falsePositiveAt left nil: this
		// row would still be appended with a fresh (and so dedup-winning)
		// inserted_at, silently un-dismissing a finding whose true state this
		// message never actually conveyed.
		var falsePositiveAt *time.Time
		if raw := message.GetFalsePositiveAt(); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				logger.ErrorContext(ctx, "finding has invalid false_positive_at timestamp", attr.SlogError(err), attr.SlogValueString(raw))
				w.metrics.RecordFindingCHSkipped(ctx, "invalid_false_positive_at")
				continue
			}
			utc := t.UTC()
			falsePositiveAt = &utc
		}

		rows = append(rows, chrepo.RiskFindingRow{
			ID:                       id,
			CreatedAt:                createdAt.UTC(),
			OrganizationID:           message.GetOrganizationId(),
			ProjectID:                message.GetProjectId(),
			RequestID:                message.GetRequestId(),
			ChatMessageID:            chatMessageID,
			ContentPartID:            contentPartID,
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
			ChatID:                   chatID,
			UserID:                   userID,
			ExternalUserID:           externalUserID,
			Category:                 category,
			MatchLen:                 matchLen,
			MatchRedacted:            matchRedacted,
			FingerprintPepperVersion: pepperVersion,
			FingerprintGlobalHS256:   globalHS256,
			FingerprintTenantHS256:   tenantHS256,
			ExcludedAt:               excludedAt,
			ExclusionID:              exclusionID,
			FalsePositiveAt:          falsePositiveAt,
			MessageCreatedAt:         messageCreatedAt,
			AssistantID:              assistantID,
			ChatSource:               chatSource,
			Team:                     team,
			UserEmail:                userEmail,
			Surface:                  message.GetSurface(),
			Field:                    message.GetField(),
			Path:                     message.GetPath(),
			ToolCallID:               message.GetToolCallId(),
		})
	}

	if len(rows) == 0 {
		return nil
	}

	err := w.inserter.InsertRiskFindings(ctx, rows)
	if err != nil {
		// Log the error rather than returning it: a failed analytics insert must
		// not nack and redrive the finding.
		logger.ErrorContext(ctx, "failed to insert batch into clickhouse", attr.SlogError(err))
	}

	w.metrics.RecordFindingCHInserts(ctx, len(rows), o11y.OutcomeFromError(err))

	return nil
}

// chatMessageAttribution batch-fetches the denormalized attribution stamped
// onto risk_findings rows, keyed by chat message id. Fail-open like the
// exclusion lookup: message ids that are empty, malformed, or fail to resolve
// simply get no attribution; a query error logs and enriches nothing rather
// than dropping or redriving findings.
func (w *FindingCHWriter) chatMessageAttribution(ctx context.Context, messages []*riskv1.Finding) map[uuid.UUID]repo.GetChatMessageAttributionRow {
	ids := findingAnchorIDs(messages, func(message *riskv1.Finding) string {
		return message.GetChatMessageId()
	})
	if len(ids) == 0 {
		return nil
	}

	rows, err := repo.New(w.db).GetChatMessageAttribution(ctx, ids)
	if err != nil {
		w.logger.ErrorContext(ctx, "resolve chat message attribution", attr.SlogError(err))
		return nil
	}

	out := make(map[uuid.UUID]repo.GetChatMessageAttributionRow, len(rows))
	for _, row := range rows {
		out[row.ID] = row
	}
	return out
}

// chatContentPartAttribution batch-fetches denormalized attribution for
// content-part findings, keyed by content part id. It resolves via the content
// part's parent message when present and falls back to the chat.
func (w *FindingCHWriter) chatContentPartAttribution(ctx context.Context, messages []*riskv1.Finding) map[uuid.UUID]repo.GetChatContentPartAttributionRow {
	ids := findingAnchorIDs(messages, func(message *riskv1.Finding) string {
		return message.GetContentPartId()
	})
	if len(ids) == 0 {
		return nil
	}

	// Bound the read to the projects present in this batch. Findings whose
	// project id is unparseable contribute nothing, so a part they anchor to
	// simply resolves no attribution.
	projectIDs := findingAnchorIDs(messages, func(message *riskv1.Finding) string {
		return message.GetProjectId()
	})
	if len(projectIDs) == 0 {
		return nil
	}

	rows, err := repo.New(w.db).GetChatContentPartAttribution(ctx, repo.GetChatContentPartAttributionParams{
		Ids:        ids,
		ProjectIds: projectIDs,
	})
	if err != nil {
		w.logger.ErrorContext(ctx, "resolve chat content part attribution", attr.SlogError(err))
		return nil
	}

	out := make(map[uuid.UUID]repo.GetChatContentPartAttributionRow, len(rows))
	for _, row := range rows {
		out[row.ID] = row
	}
	return out
}

func findingAnchorIDs(messages []*riskv1.Finding, getRawID func(*riskv1.Finding) string) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(messages))
	seen := make(map[uuid.UUID]struct{}, len(messages))
	for _, message := range messages {
		id, err := uuid.Parse(getRawID(message))
		if err != nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
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

	exclusions, err := repo.New(w.db).ListEnabledExclusionsForPolicy(ctx, repo.ListEnabledExclusionsForPolicyParams{
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
