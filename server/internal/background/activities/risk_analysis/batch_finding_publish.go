package risk_analysis

import (
	"context"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
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
//
// Known gap, accepted for this bridge: account_identity findings that are
// later enriched in place (RefreshAccountIdentityFindingMatch patches the
// Postgres row's match/description once the account email arrives; the fresh
// scan result is then dropped by the rule-scoped dedupe) are NOT republished,
// so the ClickHouse mirror keeps the pre-enrichment match. Counts stay
// correct — the enrichment is per-finding metadata the CH read path does not
// aggregate on — and the mirror converges when the batch path's publishing
// authority moves to the stream pipeline.
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
// emission (findingCreatedPayloads). Publishes are issued for the whole batch
// first and the acks drained through drainPublishAcks, which caps each ack,
// survives activity cancellation, and heartbeats between acks — the same
// discipline every other publish in this activity uses.
// anchors carries one entry per findings slot, so the two stay index aligned.
// An anchor is either a chat message or a content part, so exactly one of the
// two published ids is set per finding.
func (a *AnalyzeBatch) publishBatchOnlyFindings(ctx context.Context, args AnalyzeBatchArgs, anchors []batchMessage, findings [][]scanners.Finding) {
	var results []gcp.PublishResult
	for i, anchor := range anchors {
		chatMessageID, contentPartID := anchor.anchorIDStrings()
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
			ChatMessageID:     derefOrEmpty(chatMessageID),
			ContentPartID:     derefOrEmpty(contentPartID),
			ProjectID:         args.ProjectID.String(),
			OrganizationID:    args.OrganizationID,
			RiskPolicyID:      args.RiskPolicyID.String(),
			RiskPolicyVersion: args.PolicyVersion,
		}
		messageResults, _ := scanners.StartPublishFindings(ctx, a.findingsPub, meta, toPublish)
		results = append(results, messageResults...)
	}

	drainPublishAcks(ctx, a.logger, "failed to publish batch-only finding", results)
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
