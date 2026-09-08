package scanners

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

type FindingMetadata struct {
	RequestID         string
	ChatMessageID     string
	ContentPartID     string
	ProjectID         string
	OrganizationID    string
	RiskPolicyID      string
	RiskPolicyVersion int64
}

func PublishFindings(ctx context.Context, logger *slog.Logger, pub gcp.Publisher[*riskv1.Finding], meta FindingMetadata, findings []Finding, logPrefix string) (int, []string, error) {
	results, ruleIDs := StartPublishFindings(ctx, pub, meta, findings)

	published := 0
	var publishErr error
	for _, res := range results {
		if _, err := res.Get(ctx); err != nil {
			logger.WarnContext(ctx, "failed to publish "+logPrefix+" finding", attr.SlogError(err))
			publishErr = errors.Join(publishErr, err)
			continue
		}
		published++
	}
	if publishErr != nil {
		return published, ruleIDs, fmt.Errorf("publish %s findings: %w", logPrefix, publishErr)
	}

	return published, ruleIDs, nil
}

// StartPublishFindings issues the publishes without waiting on their acks,
// returning one PublishResult per finding (in finding order) alongside the
// rule ids. Callers that run inside Temporal activities drain the results with
// their own timeout/heartbeat discipline (see risk_analysis.drainPublishAcks)
// instead of blocking on bare Get calls.
func StartPublishFindings(ctx context.Context, pub gcp.Publisher[*riskv1.Finding], meta FindingMetadata, findings []Finding) ([]gcp.PublishResult, []string) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	results := make([]gcp.PublishResult, 0, len(findings))
	ruleIDs := make([]string, 0, len(findings))

	// Occurrence counter per base id: a batch can legitimately carry findings
	// whose id inputs are identical (e.g. two tool calls in one message hitting
	// the same cli_destructive rule, or two calls to the same shadow MCP server
	// — Match is the tool/server name and positions are zero). Without a
	// discriminator they'd publish under one id and ClickHouse's uniqExact(id)
	// would collapse distinct persisted findings. Suffixing only repeats keeps
	// every previously-minted id stable while making re-publishes of the same
	// finding set deterministic (finding order follows tool-call order, which
	// is stable for a given message). Note this discriminator changed observed
	// counts for the stream sources too (llm_judge, prompt_injection): colliding
	// findings that previously collapsed to one ClickHouse row now count as N.
	occurrences := make(map[uuid.UUID]int, len(findings))

	for _, finding := range findings {
		baseID := deterministicFindingID(meta, finding)
		id := baseID
		if n := occurrences[baseID]; n > 0 {
			id = duplicateFindingID(baseID, n)
		}
		occurrences[baseID]++
		startPos := conv.SafeInt32(finding.StartPos)
		endPos := conv.SafeInt32(finding.EndPos)
		surface := FindingSurface(finding.Source, finding.Field, finding.Path)
		msg := riskv1.Finding_builder{
			Id:                new(id.String()),
			RequestId:         &meta.RequestID,
			ChatMessageId:     conv.PtrEmpty(meta.ChatMessageID),
			ContentPartId:     conv.PtrEmpty(meta.ContentPartID),
			ProjectId:         &meta.ProjectID,
			OrganizationId:    &meta.OrganizationID,
			RiskPolicyId:      &meta.RiskPolicyID,
			RiskPolicyVersion: &meta.RiskPolicyVersion,
			CreatedAt:         &createdAt,
			RuleId:            &finding.RuleID,
			Description:       &finding.Description,
			Match:             &finding.Match,
			StartPos:          &startPos,
			EndPos:            &endPos,
			Tags:              finding.Tags,
			Source:            &finding.Source,
			Confidence:        &finding.Confidence,
			Surface:           &surface,
			Field:             &finding.Field,
			Path:              &finding.Path,
			ToolCallId:        &finding.McpLookupToolCallID,
			// Scanner output is always a finding copy. State-change republishes
			// (suppression/unsuppression) are built elsewhere and never pass
			// through here.
			EventKind: new(chrepo.EventKindFinding),
		}.Build()

		results = append(results, pub.Publish(ctx, msg))
		ruleIDs = append(ruleIDs, finding.RuleID)
	}

	return results, ruleIDs
}

// duplicateFindingID derives the id for the n-th repeat (n >= 1) of a finding
// whose deterministic id inputs collide with an earlier finding in the same
// batch. Derived from the base id rather than re-joining the parts so it can
// never collide with any base id.
func duplicateFindingID(base uuid.UUID, n int) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:finding:dup:"+base.String()+":"+strconv.Itoa(n)))
}

func deterministicFindingID(meta FindingMetadata, finding Finding) uuid.UUID {
	parts := []string{
		meta.RequestID,
		meta.ChatMessageID,
	}
	if meta.ContentPartID != "" {
		parts = append(parts, meta.ContentPartID)
	}
	parts = append(parts,
		meta.ProjectID,
		meta.OrganizationID,
		meta.RiskPolicyID,
		strconv.FormatInt(meta.RiskPolicyVersion, 10),
		finding.Source,
		finding.RuleID,
		strconv.Itoa(finding.StartPos),
		strconv.Itoa(finding.EndPos),
		finding.Match,
	)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:finding:"+strings.Join(parts, "\x00")))
}
