package accountidentity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

type Handler struct {
	logger      *slog.Logger
	findingsPub gcp.Publisher[*riskv1.Finding]
	scanner     *Scanner
}

func NewHandler(logger *slog.Logger, findingsPub gcp.Publisher[*riskv1.Finding]) *Handler {
	return &Handler{
		logger:      logger.With(attr.SlogComponent("account-identity-analyzer")),
		findingsPub: findingsPub,
		scanner:     NewScanner(),
	}
}

func (h *Handler) Handle(ctx context.Context, m *riskv1.AccountIdentityAnalysis, _ gcp.MessageMetadata) error {
	chatID, err := uuid.Parse(m.GetChatId())
	if err != nil {
		return fmt.Errorf("parse chat id: %w", err)
	}
	policyID, err := uuid.Parse(m.GetRiskPolicyId())
	if err != nil {
		return fmt.Errorf("parse risk policy id: %w", err)
	}

	findings := h.scanner.Scan(ctx, ScanRequest{
		ApprovedDomains: m.GetApprovedDomains(),
		AccountType:     m.GetAccountType(),
		Email:           m.GetEmail(),
	})
	meta := scanners.FindingMetadata{
		RequestID:         m.GetRequestId(),
		ChatMessageID:     m.GetChatMessageId(),
		ContentPartID:     "",
		ProjectID:         m.GetProjectId(),
		OrganizationID:    m.GetOrganizationId(),
		RiskPolicyID:      m.GetRiskPolicyId(),
		RiskPolicyVersion: m.GetRiskPolicyVersion(),
		Shadow:            false,
	}

	results := make([]gcp.PublishResult, 0, len(findings))
	ruleIDs := make([]string, 0, len(findings))
	for _, finding := range findings {
		published, _ := scanners.StartPublishFindings(ctx, h.findingsPub, meta, []scanners.Finding{finding}, scanners.WithFindingID(identityFindingID(chatID, finding.RuleID, policyID, m.GetRiskPolicyVersion())))
		results = append(results, published...)
		ruleIDs = append(ruleIDs, finding.RuleID)
	}

	published := 0
	var publishErr error
	for _, result := range results {
		if _, err := result.Get(ctx); err != nil {
			publishErr = errors.Join(publishErr, err)
			continue
		}
		published++
	}

	h.logger.InfoContext(ctx, "account identity scan complete", attr.SlogValueAny(map[string]any{
		"request_id": m.GetRequestId(),
		"chat_id":    m.GetChatId(),
		"detections": len(findings),
		"published":  published,
		"rule_ids":   ruleIDs,
	}))

	if publishErr != nil {
		return fmt.Errorf("publish account identity findings: %w", publishErr)
	}
	return nil
}

func identityFindingID(chatID uuid.UUID, ruleID string, policyID uuid.UUID, policyVersion int64) uuid.UUID {
	parts := []string{chatID.String(), ruleID, policyID.String(), strconv.FormatInt(policyVersion, 10)}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:account-identity:"+strings.Join(parts, "\x00")))
}
