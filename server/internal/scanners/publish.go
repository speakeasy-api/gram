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
)

type FindingMetadata struct {
	RequestID         string
	ChatMessageID     string
	ProjectID         string
	OrganizationID    string
	RiskPolicyID      string
	RiskPolicyVersion int64
}

func PublishFindings(ctx context.Context, logger *slog.Logger, pub gcp.Publisher[*riskv1.Finding], meta FindingMetadata, findings []Finding, logPrefix string) (int, []string, error) {
	createdAt := time.Now().UTC().Format(time.RFC3339)
	results := make([]gcp.PublishResult, 0, len(findings))
	ruleIDs := make([]string, 0, len(findings))

	for _, finding := range findings {
		id := deterministicFindingID(meta, finding)
		startPos := conv.SafeInt32(finding.StartPos)
		endPos := conv.SafeInt32(finding.EndPos)
		msg := riskv1.Finding_builder{
			Id:                new(id.String()),
			RequestId:         &meta.RequestID,
			ChatMessageId:     &meta.ChatMessageID,
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
		}.Build()

		results = append(results, pub.Publish(ctx, msg))
		ruleIDs = append(ruleIDs, finding.RuleID)
	}

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

func deterministicFindingID(meta FindingMetadata, finding Finding) uuid.UUID {
	parts := []string{
		meta.RequestID,
		meta.ChatMessageID,
		meta.ProjectID,
		meta.OrganizationID,
		meta.RiskPolicyID,
		strconv.FormatInt(meta.RiskPolicyVersion, 10),
		finding.Source,
		finding.RuleID,
		strconv.Itoa(finding.StartPos),
		strconv.Itoa(finding.EndPos),
		finding.Match,
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:finding:"+strings.Join(parts, "\x00")))
}
