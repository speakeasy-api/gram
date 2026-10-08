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

// batchPublishedFindingSources: sources the batch publishes to the findings
// topic. Content sources publish from their stream handlers with their own
// ids. Judge sources are here because their stream handlers run the real
// judge only for the risk-async-scan-shadow sample. account_identity
// enrichment is not republished (known gap).
var batchPublishedFindingSources = map[string]struct{}{
	SourceAccountIdentity:  {},
	shadowmcpscan.Source:   {},
	clidestructive.Source:  {},
	destructivetool.Source: {},
	SourcePromptInjection:  {},
	promptpolicy.Source:    {},
}

// publishBatchFindings publishes allowlisted findings after a committed
// Postgres write; a failed ack fails the activity so Temporal redrives it.
// Dead-letter sentinels are skipped. anchors and findings are index aligned.
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
