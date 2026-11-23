package risk_analysis

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func (a *AnalyzeBatch) publishPresidioScanRequests(ctx context.Context, args AnalyzeBatchArgs, messages []batchMessage, scoreThreshold float64) error {
	createdAt := time.Now().UTC()
	createdAtText := createdAt.Format(time.RFC3339)
	publishResults := make([]gcp.PublishResult, 0, len(messages))
	for _, msg := range messages {
		requestID := messageScanRequestID(args, scanLaneStandard, msg)
		provenance := batchRiskProvenance(args, msg, shadowStreamExecutionPath, requestID)
		content := msg.scanSurface()
		findingSurface := scanners.SurfaceContent
		if content != msg.Content {
			findingSurface = "scan_surface"
		}
		stokenCount, err := a.stokenCodec.Count(ctx, content)
		if err != nil {
			return fmt.Errorf("count presidio request content: %w", err)
		}
		reading, err := metering.PrepareRiskReading(metering.RiskPresidio(), provenance, int64(stokenCount), createdAt)
		if err != nil {
			return fmt.Errorf("prepare presidio meter reading: %w", err)
		}
		var meterReading []byte
		if reading != nil {
			meterReading, err = proto.Marshal(reading)
			if err != nil {
				return fmt.Errorf("marshal presidio meter reading: %w", err)
			}
		}

		chatMessageID, contentPartID := msg.anchorIDStrings()
		publishResults = append(publishResults, a.presidioPub.Publish(ctx, riskv1.PresidioAnalysis_builder{
			RequestId:               &requestID,
			ChatMessageId:           chatMessageID,
			ContentPartId:           contentPartID,
			ProjectId:               new(args.ProjectID.String()),
			OrganizationId:          &args.OrganizationID,
			RiskPolicyId:            new(args.RiskPolicyID.String()),
			RiskPolicyVersion:       &args.PolicyVersion,
			CreatedAt:               &createdAtText,
			ChatId:                  nilUUIDString(msg.ChatID),
			ParentChatMessageId:     nilUUIDString(msg.ParentChatMessageID),
			OriginRiskPolicyId:      new(args.RiskPolicyID.String()),
			OriginRiskPolicyVersion: &args.PolicyVersion,
			MessageLinkReason:       &provenance.MessageLinkReason,
			ExecutionPath:           new(shadowStreamExecutionPath),
			ToolCallId:              &provenance.ToolCallID,
			ToolName:                &provenance.ToolName,
			HookSource:              &msg.Source,
			UserId:                  &msg.UserID,
			MessageType:             new(msg.Type),
			MeterReading:            meterReading,
			FindingSurface:          &findingSurface,

			ReplyUrn:       nil,
			Content:        &content,
			Entities:       args.PresidioEntities,
			ScoreThreshold: &scoreThreshold,
		}.Build()))
	}
	return drainPublishAcks(ctx, "publish presidio scan requests", publishResults)
}
