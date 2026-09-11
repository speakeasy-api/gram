package metering_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func riskProvenance() metering.RiskProvenance {
	return metering.RiskProvenance{
		OrganizationID:         "org-test",
		ProjectID:              uuid.New(),
		RiskPolicyID:           uuid.New(),
		RiskPolicyVersion:      3,
		PolicyLinkReason:       "",
		ChatID:                 uuid.New(),
		ExternalConversationID: "",
		ChatMessageID:          uuid.New(),
		ContentPartID:          uuid.Nil,
		MessageLinkReason:      "",
		OperationID:            "inline:policy-version:message",
		ExecutionPath:          "batch_inline",
		RequestID:              "request-test",
		MessageType:            "user",
		HookSource:             "",
		UserID:                 "",
		ToolCallID:             "",
		ToolName:               "",
		Model:                  "",
		Provider:               "",
	}
}

func TestRiskReadingIdentitySeparatesScannersAndRetainsProvenance(t *testing.T) {
	t.Parallel()
	provenance := riskProvenance()
	occurredAt := time.Now().UTC()
	seen := make(map[string]struct{})
	for _, definition := range []metering.Definition{
		metering.RiskGitleaks(), metering.RiskPresidio(),
		metering.RiskPromptInjection(), metering.RiskPromptPolicy(),
		metering.RiskCustomRules(), metering.RiskCLIDestructive(),
	} {
		reading, err := metering.PrepareRiskReading(definition, provenance, 23, occurredAt)
		require.NoError(t, err)
		require.NotNil(t, reading)
		require.NotContains(t, seen, reading.GetId(), "different scanners must not collapse into one ledger entry")
		seen[reading.GetId()] = struct{}{}
		retry, err := metering.PrepareRiskReading(definition, provenance, 23, occurredAt)
		require.NoError(t, err)
		require.Equal(t, reading.GetId(), retry.GetId(), "delivery retry must not create more usage")
		require.Equal(t, provenance.OrganizationID, reading.GetOrganizationId())
		require.Equal(t, provenance.ProjectID.String(), reading.GetProjectId())
		require.Equal(t, provenance.RiskPolicyID.String(), reading.GetAttributes()[metering.AttributeRiskPolicyID])
		require.Equal(t, "3", reading.GetAttributes()[metering.AttributeRiskPolicyVersion])
		require.Equal(t, provenance.ChatMessageID.String(), reading.GetAttributes()[metering.AttributeChatMessageID])
		require.Equal(t, "linked", reading.GetAttributes()[metering.AttributeMessageLinkStatus])
		require.Equal(t, string(metering.UnitSTokens), reading.GetUnit())
		require.Equal(t, string(metering.MeasurementTiktokenO200kBase), reading.GetMeasurementMethod())
	}
}

func TestRiskReadingNormalizesExecutionTimeToUTC(t *testing.T) {
	t.Parallel()
	occurredAt := time.Date(2026, time.September, 8, 16, 0, 0, 0, time.FixedZone("scanner-local", 3600))
	reading, err := metering.PrepareRiskReading(metering.RiskGitleaks(), riskProvenance(), 4, occurredAt)
	require.NoError(t, err)
	actual, err := time.Parse(time.RFC3339Nano, reading.GetOccurredAt())
	require.NoError(t, err)
	require.Equal(t, time.UTC, actual.Location())
	require.True(t, actual.Equal(occurredAt), "UTC conversion must preserve the execution instant")
}

func TestRiskReadingRequiresExplicitUnlinkedMessageProvenance(t *testing.T) {
	t.Parallel()
	provenance := riskProvenance()
	provenance.ChatMessageID = uuid.Nil
	_, err := metering.PrepareRiskReading(metering.RiskGitleaks(), provenance, 4, time.Now().UTC())
	require.Error(t, err)

	provenance.MessageLinkReason = "realtime_not_persisted"
	reading, err := metering.PrepareRiskReading(metering.RiskGitleaks(), provenance, 4, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "unlinked", reading.GetAttributes()[metering.AttributeMessageLinkStatus])
	require.Equal(t, "realtime_not_persisted", reading.GetAttributes()[metering.AttributeMessageLinkReason])
	require.NotContains(t, reading.GetAttributes(), metering.AttributeChatMessageID)
	require.Equal(t, provenance.ChatID.String(), reading.GetAttributes()[metering.AttributeChatID])
}

func TestRiskReadingSeparatesExternalConversationFromPersistedChat(t *testing.T) {
	t.Parallel()
	provenance := riskProvenance()
	provenance.ChatID = uuid.Nil
	provenance.ChatMessageID = uuid.Nil
	provenance.MessageLinkReason = "realtime_message_not_resolved"
	provenance.ExternalConversationID = uuid.NewString()
	occurredAt := time.Now().UTC()
	reading, err := metering.PrepareRiskReading(metering.RiskGitleaks(), provenance, 4, occurredAt)
	require.NoError(t, err)
	require.Equal(t, provenance.ExternalConversationID, reading.GetAttributes()[metering.AttributeExternalConversationID])
	require.NotContains(t, reading.GetAttributes(), metering.AttributeChatID)
	require.NotContains(t, reading.GetAttributes(), metering.AttributeChatMessageID)

	provenance.ChatID = uuid.New()
	linkedChat, err := metering.PrepareRiskReading(metering.RiskGitleaks(), provenance, 4, occurredAt)
	require.NoError(t, err)
	require.Equal(t, provenance.ChatID.String(), linkedChat.GetAttributes()[metering.AttributeChatID])
	require.Equal(t, provenance.ExternalConversationID, linkedChat.GetAttributes()[metering.AttributeExternalConversationID])
	require.Equal(t, reading.GetId(), linkedChat.GetId(), "enriching provenance must not change usage identity")
}

func TestRiskReadingRejectsMissingOriginPolicy(t *testing.T) {
	t.Parallel()
	provenance := riskProvenance()
	provenance.RiskPolicyID = uuid.Nil
	_, err := metering.PrepareRiskReading(metering.RiskPresidio(), provenance, 4, time.Now().UTC())
	require.Error(t, err)
}

func TestRiskReadingAllowsExplicitNonPolicyExecution(t *testing.T) {
	t.Parallel()
	provenance := riskProvenance()
	provenance.RiskPolicyID = uuid.Nil
	provenance.RiskPolicyVersion = 0
	provenance.PolicyLinkReason = "draft_rule_test"
	reading, err := metering.PrepareRiskReading(metering.RiskCustomRules(), provenance, 4, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "unlinked", reading.GetAttributes()[metering.AttributeRiskPolicyLinkStatus])
	require.Equal(t, "draft_rule_test", reading.GetAttributes()[metering.AttributeRiskPolicyLinkReason])
	require.NotContains(t, reading.GetAttributes(), metering.AttributeRiskPolicyID)
	require.NotContains(t, reading.GetAttributes(), metering.AttributeRiskPolicyVersion)
}

func TestRiskStripeExporterDropsRegisteredScansWithoutCustomerLookup(t *testing.T) {
	t.Parallel()
	conn, organizationID, projectID, _, _ := newAcceptedRiskReading(t)
	provenance := riskProvenance()
	provenance.OrganizationID = organizationID
	provenance.ProjectID = projectID
	client := &captureV2MeterEventClient{inputs: nil, err: nil}
	// A nil customer reader makes an accidental lookup fail immediately.
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, nil, client, tumStripeCatalog, true)
	for _, definition := range []metering.Definition{
		metering.RiskGitleaks(), metering.RiskPresidio(),
		metering.RiskPromptInjection(), metering.RiskPromptPolicy(),
		metering.RiskCustomRules(), metering.RiskCLIDestructive(),
	} {
		reading, err := metering.PrepareRiskReading(definition, provenance, 5, time.Now().UTC())
		require.NoError(t, err)
		require.NoError(t, exporter.Handle(t.Context(), reading, gcp.MessageMetadata{}))
	}
	require.Empty(t, client.inputs)
}

func TestRiskLedgerDeduplicatesDeliveryAndPreservesOrigin(t *testing.T) {
	t.Parallel()
	conn, organizationID, projectID, _, _ := newAcceptedRiskReading(t)
	provenance := riskProvenance()
	provenance.OrganizationID = organizationID
	provenance.ProjectID = projectID
	provenance.ContentPartID = uuid.New()
	occurredAt := time.Now().UTC()
	first, err := metering.PrepareRiskReading(metering.RiskPresidio(), provenance, 17, occurredAt)
	require.NoError(t, err)
	retry, err := metering.PrepareRiskReading(metering.RiskPresidio(), provenance, 17, occurredAt)
	require.NoError(t, err)
	capture := &captureReadingInserter{rows: nil, err: nil}
	writer := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), conn, capture)
	require.NoError(t, writer.HandleBatch(t.Context(), []*meteringv1.MeterReading{first, retry}, nil))
	require.Len(t, capture.rows, 1)
	row := capture.rows[0]
	require.Equal(t, int64(17), row.Value)
	require.Equal(t, provenance.RiskPolicyID.String(), row.Attributes[metering.AttributeRiskPolicyID])
	require.Equal(t, "3", row.Attributes[metering.AttributeRiskPolicyVersion])
	require.Equal(t, provenance.ChatMessageID.String(), row.Attributes[metering.AttributeChatMessageID])
	require.Equal(t, provenance.ContentPartID.String(), row.Attributes[metering.AttributeContentPartID])
	require.Equal(t, occurredAt, row.OccurredAt)
}
