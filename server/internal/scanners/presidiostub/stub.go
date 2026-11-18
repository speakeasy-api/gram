// Package presidiostub stands in for the Python pystreams consumers during
// local development, so the stack does not need to load Presidio and spaCy.
//
// It consumes the subscriptions pystreams owns: analysis and ping messages are
// acknowledged without work, and enforcement requests get an OK reply with no
// findings so inline callers do not wait for the lane's deadline.
package presidiostub

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	enforce "github.com/speakeasy-api/gram/server/internal/scanners/enforcement"
)

// Reason is attached to every stubbed enforcement reply.
const Reason = "presidio stubbed: pystreams is not running"

// EnforceHandler answers Presidio enforcement requests with an empty result.
type EnforceHandler struct {
	logger     *slog.Logger
	writer     requestreply.ReplyBroker[*riskv1.EnforcementReply]
	consumerID string
}

// NewEnforceHandler builds the stub Presidio enforcement handler.
func NewEnforceHandler(logger *slog.Logger, writer requestreply.ReplyBroker[*riskv1.EnforcementReply]) *EnforceHandler {
	return &EnforceHandler{
		logger:     logger.With(attr.SlogComponent("presidio-stub-enforcer")),
		writer:     writer,
		consumerID: uuid.NewString(),
	}
}

// Handle writes an OK reply with no findings. Stale requests are dropped and
// malformed ones are returned as errors, matching the real enforcers.
func (h *EnforceHandler) Handle(ctx context.Context, m *riskv1.PresidioEnforcement, meta gcp.MessageMetadata) error {
	replyURN := enforce.ReplyURN(meta.Attributes)
	correlationID, stale, err := enforce.ValidateRequest(enforce.Request{
		CreatedAt:      m.GetCreatedAt(),
		OrganizationID: m.GetOrganizationId(),
		ProjectID:      m.GetProjectId(),
		ReplyURN:       replyURN,
		MaxRequestAge:  enforce.DefaultMaxRequestAge,
	})
	if err != nil {
		return fmt.Errorf("validate presidio enforcement request: %w", err)
	}
	if stale {
		return nil
	}

	deliveryAttempt := int32(0)
	if meta.DeliveryAttempt != nil {
		deliveryAttempt = conv.SafeInt32(*meta.DeliveryAttempt)
	}
	reply := riskv1.EnforcementReply_builder{
		CorrelationId: new(correlationID),
		Scanner:       new(riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PRESIDIO),
		Status:        new(riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK),
		Reason:        new(Reason),
		Findings:      nil,
		Diagnostics: riskv1.EnforcementDiagnostics_builder{
			ScanDurationMs:  new(int64(0)),
			ConsumerId:      new(h.consumerID),
			DeliveryAttempt: new(deliveryAttempt),
		}.Build(),
		ContentTruncated: new(m.GetContentTruncated()),
		Incomplete:       new(false),
	}.Build()
	if err := h.writer.Reply(ctx, replyURN, reply); err != nil {
		// Acknowledge anyway: a redelivery cannot reach a caller that has gone.
		h.logger.ErrorContext(ctx, "write stub presidio enforcement reply", attr.SlogError(err))
	}
	return nil
}
