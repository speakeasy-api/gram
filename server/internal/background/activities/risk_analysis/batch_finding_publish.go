package risk_analysis

import (
	"context"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/scanners/destructivetool"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
)

// batchPublishedFindingSources is the explicit allowlist of sources whose
// findings the batch path publishes onto the shared findings topic (the one
// the ClickHouse risk_findings writer consumes). Content sources (presidio,
// gitleaks, custom rules) are deliberately absent: their stream handlers
// publish every finding with their own ids, and publishing them here too
// would double-write rows that uniqExact(id) cannot dedupe.
//
// Two groups belong here:
//
//   - The four sources computed only inside AnalyzeBatch — the session-scoped
//     account_identity detector and the three tool-call scanners — which have
//     no stream publisher at all.
//   - The judge sources, prompt_injection and llm_judge. Their stream handlers
//     run the real judge only for the risk-async-scan-shadow sample (today the
//     internal org, 10% of messages — the legacy baseline of the LLM-analyzer
//     experiment) and the stub otherwise, so without this publish ClickHouse
//     never sees any other org's judge findings while every read path serves
//     from ClickHouse. The sampled slice gets a second copy under a different
//     id (stream ids carry the request id, batch ids do not); accepted while
//     the experiment runs, and invisible to the comparison query, which is
//     presence-based.
//
// Known gap, accepted for this bridge: account_identity findings that are
// later enriched in place (RefreshAccountIdentityFindingMatch patches the
// Postgres row's match/description once the account email arrives; the fresh
// scan result is then dropped by the rule-scoped dedupe) are NOT republished,
// so the ClickHouse mirror keeps the pre-enrichment match. Counts stay
// correct — the enrichment is per-finding metadata the CH read path does not
// aggregate on.
var batchPublishedFindingSources = map[string]struct{}{
	SourceAccountIdentity:  {},
	shadowmcpscan.Source:   {},
	clidestructive.Source:  {},
	destructivetool.Source: {},
	SourcePromptInjection:  {},
	promptpolicy.Source:    {},
}

// publishBatchFindings mirrors allowlisted batch findings onto the findings
// topic after a committed Postgres write. A publish failure fails the
// activity so Temporal redrives the batch: the topic feeds the ClickHouse
// findings store, whose delivery contract is at-least-once. The redrive
// re-runs the scanners, but every write it repeats is idempotent — the
// Postgres write replaces per message set and the published finding ids are
// deterministic, so replays converge instead of duplicating.
//
// ids and findings are the message-aligned pair buildRows consumed, so the
// published set matches what was written (exclusions and disabled rules
// already applied). Dead-letter sentinels are skipped, mirroring the outbox
// emission (findingCreatedEvents). Publishes are issued for the whole batch
// first and the acks drained through drainPublishAcks, which caps each ack,
// survives activity cancellation, and heartbeats between acks — the same
// discipline every other publish in this activity uses.
// anchors carries one entry per findings slot, so the two stay index aligned.
// An anchor is either a chat message or a content part, so exactly one of the
// two published ids is set per finding.
func (a *AnalyzeBatch) publishBatchFindings(ctx context.Context, args AnalyzeBatchArgs, anchors []batchMessage, findings [][]scanners.Finding) error {
	var results []gcp.PublishResult
	for i, anchor := range anchors {
		chatMessageID, contentPartID := anchor.anchorIDStrings()
		var toPublish []scanners.Finding
		for _, f := range findings[i] {
			if _, ok := batchPublishedFindingSources[f.Source]; !ok || f.DeadLetterReason != "" {
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
			Shadow:            false,
		}
		messageResults, _ := scanners.StartPublishFindings(ctx, a.findingsPub, meta, toPublish)
		results = append(results, messageResults...)
	}

	return drainPublishAcks(ctx, "publish batch findings", results)
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
