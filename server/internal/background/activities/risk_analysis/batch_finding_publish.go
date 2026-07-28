package risk_analysis

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/scanners/destructivetool"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
)

// batchOnlyFindingSources is the explicit allowlist of sources whose findings
// the batch path mirrors onto the shared findings topic (the one the
// ClickHouse risk_findings writer consumes). A source belongs here ONLY when
// it has no stream-path publisher: content sources (presidio, gitleaks,
// custom rules) and the judge sources (llm_judge, prompt_injection) already
// publish from their stream handlers, and publishing them here too would
// double-write rows with different ids (batch ids come from
// deterministicFindingID over an empty request id, stream ids from the
// scanner's own metadata), which uniqExact(id) cannot dedupe.
//
// The four sources here are all computed only inside AnalyzeBatch — the
// session-scoped account_identity detector and the three tool-call scanners
// (which never got async request topics) — so without this publish ClickHouse
// never sees them and the dashboard silently loses those findings after the
// read-path cutover. Their ClickHouse presence to date is entirely the ops
// backfill's doing; anything found after a backfill run was missing until
// this publish existed.
var batchOnlyFindingSources = map[string]struct{}{
	SourceAccountIdentity:  {},
	shadowmcpscan.Source:   {},
	clidestructive.Source:  {},
	destructivetool.Source: {},
}

// publishBatchOnlyFindings mirrors allowlisted batch findings onto the
// findings topic after a committed Postgres write. Best-effort like the
// ClickHouse finding writer: a publish failure logs and never fails the
// activity — the Postgres write already succeeded and redriving the whole
// batch for an analytics publish would re-run the scanners.
//
// ids and findings are the message-aligned pair buildRows consumed, so the
// published set matches what was written (exclusions and disabled rules
// already applied). Dead-letter sentinels are skipped, mirroring the outbox
// emission (findingCreatedPayloads).
func (a *AnalyzeBatch) publishBatchOnlyFindings(ctx context.Context, args AnalyzeBatchArgs, ids []uuid.UUID, findings [][]scanners.Finding) {
	for i, id := range ids {
		var toPublish []scanners.Finding
		for _, f := range findings[i] {
			if _, ok := batchOnlyFindingSources[f.Source]; !ok || f.DeadLetterReason != "" {
				continue
			}
			toPublish = append(toPublish, f)
		}
		if len(toPublish) == 0 {
			continue
		}

		meta := scanners.FindingMetadata{
			RequestID:         "",
			ChatMessageID:     id.String(),
			ProjectID:         args.ProjectID.String(),
			OrganizationID:    args.OrganizationID,
			RiskPolicyID:      args.RiskPolicyID.String(),
			RiskPolicyVersion: args.PolicyVersion,
		}
		if _, _, err := scanners.PublishFindings(ctx, a.logger, a.findingsPub, meta, toPublish, "batch-only"); err != nil {
			a.logger.WarnContext(ctx, "failed to publish batch-only findings",
				attr.SlogError(err),
				attr.SlogOrganizationID(args.OrganizationID),
				attr.SlogRiskPolicyID(args.RiskPolicyID.String()),
			)
		}
	}
}
