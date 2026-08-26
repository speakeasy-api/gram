package risk

import (
	"context"
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
//
// Delivery contract: at-least-once into ClickHouse. A failed insert or a
// failed attribution read nacks the whole batch for redelivery; a message the
// writer can never persist — malformed id or timestamp — nacks only itself so
// it retries alone without dragging the rest of the batch along. There is no
// dead-letter queue by design: transient failures self-heal under the
// subscription's retry backoff, and a poison message (always an internal
// producer bug — every publisher is ours) redelivers within the
// subscription's retention window, surfaced by the skipped metric and
// oldest-unacked-age monitoring so the bug is fixed and the still-retained
// message then processes. Redelivered duplicates are expected and converge at
// read time: rows share their deterministic id and the read paths resolve
// each id to one winning copy.
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

// HandleBatchWithResult adapts ProcessBatch to the streams runner: per-message
// failures are staged as individual nacks, a batch-level error nacks the whole
// batch.
func (w *FindingCHWriter) HandleBatchWithResult(ctx context.Context, batch []gcp.BatchMessage[*riskv1.Finding]) error {
	messages := make([]*riskv1.Finding, len(batch))
	for i, m := range batch {
		messages[i] = m.Message
	}
	failed, err := w.ProcessBatch(ctx, messages)
	if err != nil {
		return err
	}
	for i, ferr := range failed {
		if ferr != nil {
			batch[i].Fail(ferr)
		}
	}
	return nil
}

// ProcessBatch writes one batch of findings to ClickHouse. The returned slice
// is parallel to messages: a non-nil entry is a per-message rejection (the
// message can never be persisted and should redeliver on its own). A non-nil
// error means the whole batch failed (attribution read or insert) and must be
// redelivered.
func (w *FindingCHWriter) ProcessBatch(ctx context.Context, messages []*riskv1.Finding) ([]error, error) {
	logger := w.logger
	failed := make([]error, len(messages))

	// Cache per-tenant derived keys for the lifetime of this batch so repeated
	// findings from the same org don't each re-run HKDF.
	tenantKeyCache := make(map[string][]byte)

	// Batch-resolve the denormalized attribution (chat id, user ids) for every
	// finding that carries a well-formed anchor — one Postgres query per anchor
	// kind. Both reads are bounded to the projects present in the batch;
	// findings whose project id is unparseable contribute nothing, so an
	// anchor they carry simply resolves no attribution. A query error fails
	// the batch: attribution is stamped once at ingest, so proceeding through
	// a transient Postgres blip would persist permanently unattributed rows.
	projectIDs := findingAnchorIDs(messages, func(message *riskv1.Finding) string {
		return message.GetProjectId()
	})
	messageAttribution, err := w.chatMessageAttribution(ctx, messages, projectIDs)
	if err != nil {
		return nil, err
	}
	contentPartAttribution, err := w.chatContentPartAttribution(ctx, messages, projectIDs)
	if err != nil {
		return nil, err
	}

	rows := make([]chrepo.RiskFindingRow, 0, len(messages))
	for msgIdx, message := range messages {
		orgID := strings.TrimSpace(message.GetOrganizationId())
		match := message.GetMatch()
		deadLetter := message.GetDeadLetterReason() != ""

		// The id maps to a ClickHouse UUID column. Parse it here so a malformed
		// or missing id rejects only that finding (nacked to retry on its own)
		// rather than failing the binding for the whole multi-row batch
		// insert.
		id, err := uuid.Parse(message.GetId())
		if err != nil {
			logger.ErrorContext(ctx, "finding has invalid uuid id", attr.SlogError(err), attr.SlogValueString(message.GetId()))
			w.metrics.RecordFindingCHSkipped(ctx, "invalid_id")
			failed[msgIdx] = fmt.Errorf("parse finding id: %w", err)
			continue
		}

		createdAt, err := time.Parse(time.RFC3339, message.GetCreatedAt())
		if err != nil {
			logger.ErrorContext(ctx, "finding has invalid rfc3339 timestamp", attr.SlogError(err), attr.SlogValueString(message.GetCreatedAt()))
			w.metrics.RecordFindingCHSkipped(ctx, "invalid_timestamp")
			failed[msgIdx] = fmt.Errorf("parse finding created_at: %w", err)
			continue
		}

		// Resolve the event-log kind before any ingest-time exclusion stamping:
		// deriving suppression must key off the message-carried state only — a
		// rule exclusion the writer stamps below is per-ingest recomputed state
		// on a finding copy, not a state change. Producers that predate the
		// field (and any unknown value) derive: a carried suppression stamp
		// means a state-change republish, anything else is scanner output.
		eventKind := message.GetEventKind()
		switch eventKind {
		case chrepo.EventKindFinding, chrepo.EventKindSuppression, chrepo.EventKindUnsuppression:
		default:
			if eventKind != "" {
				logger.WarnContext(ctx, "finding has unknown event kind, deriving from suppression state", attr.SlogValueString(eventKind))
			}
			if message.GetExcludedAt() != "" || message.GetFalsePositiveAt() != "" {
				eventKind = chrepo.EventKindSuppression
			} else {
				eventKind = chrepo.EventKindFinding
			}
		}

		// Suppression annotation. A message-carried excluded_at (set only on
		// republished findings recording a manual suppression state change, see
		// Finding.excluded_at) wins verbatim, reason and detail included. A
		// parse failure must skip the message rather than fall through
		// unsuppressed — the same clobber hazard as false_positive_at below.
		//
		// Otherwise annotate findings suppressed by a going-forward exclusion
		// instead of dropping them, so excluded findings stay auditable and
		// filterable at read time. The shadow scan path that feeds this writer
		// does not apply exclusions, so we mirror the Postgres path here.
		// Dead-letter sentinels carry no rule/match to match against, so they
		// bypass the check. This branch also runs for an unmark republish
		// (empty excluded_at): a finding still matching an active exclusion is
		// re-stamped as rule-suppressed instead of resurfacing.
		var excludedAt *time.Time
		var exclusionID *uuid.UUID
		excludedReason := ""
		excludedDetail := ""
		if raw := message.GetExcludedAt(); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				logger.ErrorContext(ctx, "finding has invalid excluded_at timestamp", attr.SlogError(err), attr.SlogValueString(raw))
				w.metrics.RecordFindingCHSkipped(ctx, "invalid_excluded_at")
				failed[msgIdx] = fmt.Errorf("parse finding excluded_at: %w", err)
				continue
			}
			utc := t.UTC()
			excludedAt = &utc
			excludedReason = message.GetExcludedReason()
			excludedDetail = message.GetExcludedDetail()
		} else if !deadLetter {
			if exID, ok := w.matchedExclusion(ctx, message); ok {
				now := time.Now().UTC()
				excludedAt = &now
				exclusionID = &exID
				excludedReason = chrepo.ExcludedReasonRule
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
				globalHS256 = EncodeFingerprint(sum)
				pepperVersion = pepperver
			}
		}

		tenantHS256 := ""
		if !deadLetter && orgID != "" && match != "" {
			if sum, pepperver, err := w.fingerprinter.TenantedHS256(orgID, []byte(match), WithKeyCache(tenantKeyCache)); err != nil {
				logger.ErrorContext(ctx, "failed to compute tenant-qualified fingerprint", attr.SlogError(err))
			} else {
				tenantHS256 = EncodeFingerprint(sum)
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
		// Only attribute an anchor that belongs to the finding's own project,
		// so a wrong or forged anchor id cannot pull another tenant's chat and
		// user ids into this row. A NULL row project_id (project deleted) or
		// unparseable finding project id is unverifiable and so gets no
		// attribution.
		findingProjectID, findingProjectErr := uuid.Parse(message.GetProjectId())
		if msgID, err := uuid.Parse(chatMessageID); err == nil {
			if a, ok := messageAttribution[msgID]; ok && findingProjectErr == nil && a.ProjectID.Valid && a.ProjectID.UUID == findingProjectID {
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
			// The part attribution query carries no assistant link or message
			// timestamp, so both keep the fallbacks above.
			if a, ok := contentPartAttribution[partID]; ok && findingProjectErr == nil && a.ProjectID.Valid && a.ProjectID.UUID == findingProjectID {
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
		// already-persisted finding (see enqueueFalsePositiveMirror). Empty
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
				failed[msgIdx] = fmt.Errorf("parse finding false_positive_at: %w", err)
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
			ExcludedReason:           excludedReason,
			ExcludedDetail:           excludedDetail,
			MessageCreatedAt:         messageCreatedAt,
			AssistantID:              assistantID,
			ChatSource:               chatSource,
			Team:                     team,
			UserEmail:                userEmail,
			Surface:                  message.GetSurface(),
			Field:                    message.GetField(),
			Path:                     message.GetPath(),
			ToolCallID:               message.GetToolCallId(),
			EventKind:                eventKind,
		})
	}

	if len(rows) == 0 {
		return failed, nil
	}

	// Return the insert error so the whole batch nacks and redelivers:
	// ClickHouse is the findings store of record, so an acked message must
	// mean a durably written row. Redelivered rows that did land converge at
	// read time via their shared id.
	err = w.inserter.InsertRiskFindings(ctx, rows)
	w.metrics.RecordFindingCHInserts(ctx, len(rows), o11y.OutcomeFromError(err))
	if err != nil {
		return nil, fmt.Errorf("insert risk findings batch: %w", err)
	}

	return failed, nil
}

// chatMessageAttribution batch-fetches the denormalized attribution stamped
// onto risk_findings rows, keyed by chat message id and bounded to the given
// batch project ids. Anchor ids that are empty, malformed, or fail to resolve
// simply get no attribution, but a query error fails the batch for
// redelivery: attribution is stamped once at ingest and a transient Postgres
// blip must not persist permanently unattributed rows.
func (w *FindingCHWriter) chatMessageAttribution(ctx context.Context, messages []*riskv1.Finding, projectIDs []uuid.UUID) (map[uuid.UUID]repo.GetChatMessageAttributionRow, error) {
	ids := findingAnchorIDs(messages, func(message *riskv1.Finding) string {
		return message.GetChatMessageId()
	})
	if len(ids) == 0 || len(projectIDs) == 0 {
		return nil, nil
	}

	rows, err := repo.New(w.db).GetChatMessageAttribution(ctx, repo.GetChatMessageAttributionParams{
		Ids:        ids,
		ProjectIds: projectIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve chat message attribution: %w", err)
	}

	out := make(map[uuid.UUID]repo.GetChatMessageAttributionRow, len(rows))
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}

// chatContentPartAttribution batch-fetches denormalized attribution for
// content-part findings, keyed by content part id and bounded to the given
// batch project ids. It resolves via the content part's parent message when
// present and falls back to the chat.
func (w *FindingCHWriter) chatContentPartAttribution(ctx context.Context, messages []*riskv1.Finding, projectIDs []uuid.UUID) (map[uuid.UUID]repo.GetChatContentPartAttributionRow, error) {
	ids := findingAnchorIDs(messages, func(message *riskv1.Finding) string {
		return message.GetContentPartId()
	})
	if len(ids) == 0 || len(projectIDs) == 0 {
		return nil, nil
	}

	rows, err := repo.New(w.db).GetChatContentPartAttribution(ctx, repo.GetChatContentPartAttributionParams{
		Ids:        ids,
		ProjectIds: projectIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve chat content part attribution: %w", err)
	}

	out := make(map[uuid.UUID]repo.GetChatContentPartAttributionRow, len(rows))
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
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
