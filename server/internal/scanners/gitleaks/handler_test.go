package gitleaks_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"log/slog"
	"testing"
	"time"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// capturingPub records every Finding handed to Publish so tests can assert on
// the published payloads.
func capturingPub(t *testing.T) (*gcp.MockPublisher[*riskv1.Finding], *[]*riskv1.Finding) {
	t.Helper()
	pub := gcp.NewMockPublisher[*riskv1.Finding]()
	var published []*riskv1.Finding
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			f, ok := args.Get(1).(*riskv1.Finding)
			require.True(t, ok)
			published = append(published, f)
		})
	return pub, &published
}

func capturingMeterPub(t *testing.T) (*gcp.MockPublisher[*meteringv1.MeterReading], *[]*meteringv1.MeterReading) {
	t.Helper()
	pub := gcp.NewMockPublisher[*meteringv1.MeterReading]()
	var published []*meteringv1.MeterReading
	pub.On("Publish", mock.Anything, mock.Anything).
		Return(gcp.NewSuccessPublishResult()).
		Run(func(args mock.Arguments) {
			reading, ok := args.Get(1).(*meteringv1.MeterReading)
			require.True(t, ok)
			published = append(published, reading)
		})
	return pub, &published
}

func newRequest(content string) *riskv1.GitleaksAnalysis {
	return riskv1.GitleaksAnalysis_builder{
		RequestId:               new("req-1"),
		ChatMessageId:           new("018ffad2-1c32-7f73-8a54-85306c37a314"),
		ProjectId:               new("018ffad2-1c32-7f73-8a54-85306c37a313"),
		OrganizationId:          new("org-1"),
		RiskPolicyId:            new("018ffad2-1c32-7f73-8a54-85306c37a315"),
		RiskPolicyVersion:       new(int64(3)),
		CreatedAt:               new("2026-06-20T00:00:00Z"),
		Content:                 &content,
		OriginRiskPolicyId:      new("018ffad2-1c32-7f73-8a54-85306c37a315"),
		OriginRiskPolicyVersion: new(int64(3)),
		ExecutionPath:           new("async"),
	}.Build()
}

func TestHandle_PublishesGitleaksFinding(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), pub, metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))

	// The access key id anchors detection but is not itself reported (it is an
	// identifier, not a secret); the secret access key is the reported finding.
	content := `AccessKeyId: ` + fakeAccessKeyID + `, SecretAccessKey: ` + fakeSecret
	require.NoError(t, h.Handle(t.Context(), newRequest(content), gcp.MessageMetadata{}))

	require.NotEmpty(t, *published, "expected at least one finding published")

	var awsFinding *riskv1.Finding
	for _, f := range *published {
		// Request context propagates onto every finding.
		require.Equal(t, "gitleaks", f.GetSource())
		require.Equal(t, "req-1", f.GetRequestId())
		require.Equal(t, "018ffad2-1c32-7f73-8a54-85306c37a314", f.GetChatMessageId())
		require.Equal(t, int64(3), f.GetRiskPolicyVersion())
		require.NotEmpty(t, f.GetId())
		require.InDelta(t, 1.0, f.GetConfidence(), 0.0001)

		// Byte offsets must slice the matched secret out of the content.
		start, end := int(f.GetStartPos()), int(f.GetEndPos())
		require.GreaterOrEqual(t, start, 0)
		require.LessOrEqual(t, end, len(content))
		require.Equal(t, f.GetMatch(), content[start:end])

		if f.GetRuleId() == "secret.aws_secret_access_key" {
			awsFinding = f
		}
		require.NotEqual(t, "secret.aws_access_token", f.GetRuleId(),
			"the access key id must not be reported as a finding")
	}
	require.NotNil(t, awsFinding, "expected an aws secret access key finding")
}

func TestHandle_PublishesGitleaksFindingForContentPart(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), pub, metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))

	content := `AccessKeyId: ` + fakeAccessKeyID + `, SecretAccessKey: ` + fakeSecret
	req := newRequest(content)
	req.ClearChatMessageId()
	req.SetContentPartId("018ffad2-1c32-7f73-8a54-85306c37a316")
	req.SetMessageLinkReason("content_part_test_unlinked")
	require.NoError(t, h.Handle(t.Context(), req, gcp.MessageMetadata{}))

	require.NotEmpty(t, *published, "expected at least one finding published")
	for _, f := range *published {
		require.Empty(t, f.GetChatMessageId())
		require.Equal(t, "018ffad2-1c32-7f73-8a54-85306c37a316", f.GetContentPartId())
	}
}

func TestHandle_CleanContentPublishesNothing(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), pub, metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))

	require.NoError(t, h.Handle(t.Context(), newRequest("hello world, this is a normal message"), gcp.MessageMetadata{}))
	require.Empty(t, *published)
}

// The stream handler scans the verbatim request content, so every published
// finding carries surface "content" and no span attribution.
func TestHandle_StampsContentSurface(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), pub, metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))

	content := `SecretAccessKey: ` + fakeSecret
	require.NoError(t, h.Handle(t.Context(), newRequest(content), gcp.MessageMetadata{}))

	require.NotEmpty(t, *published)
	for _, f := range *published {
		require.Equal(t, "content", f.GetSurface())
		require.Empty(t, f.GetField())
		require.Empty(t, f.GetPath())
		require.Empty(t, f.GetToolCallId())
	}
}

// Redelivering the same scan request republishes every finding under the same
// deterministic id, so ClickHouse's id-level dedup collapses the duplicates
// instead of counting them twice (the old uuid.NewV7-per-publish minted a new
// row per redelivery).
func TestHandle_RedeliveryKeepsDeterministicIDs(t *testing.T) {
	t.Parallel()

	firstPub, firstPublished := capturingPub(t)
	secondPub, secondPublished := capturingPub(t)

	content := `AccessKeyId: ` + fakeAccessKeyID + `, SecretAccessKey: ` + fakeSecret
	require.NoError(t, gitleaks.NewHandler(testenv.NewLogger(t), firstPub, metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]())).Handle(t.Context(), newRequest(content), gcp.MessageMetadata{}))
	require.NoError(t, gitleaks.NewHandler(testenv.NewLogger(t), secondPub, metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]())).Handle(t.Context(), newRequest(content), gcp.MessageMetadata{}))

	require.NotEmpty(t, *firstPublished)
	require.Len(t, *secondPublished, len(*firstPublished))
	for i, f := range *firstPublished {
		require.NotEmpty(t, f.GetId())
		require.Equal(t, f.GetId(), (*secondPublished)[i].GetId(), "ids must be stable across redeliveries")
	}
}

func TestHandle_PublishesUsageForFindingAndCleanScans(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		`SecretAccessKey: ` + fakeSecret,
		"hello world, this is a normal message",
	} {
		findingsPub, _ := capturingPub(t)
		meterPub, readings := capturingMeterPub(t)
		h := gitleaks.NewHandler(testenv.NewLogger(t), findingsPub, metering.NewRiskRecorder(meterPub))

		require.NoError(t, h.Handle(t.Context(), newRequest(content), gcp.MessageMetadata{}))
		require.Len(t, *readings, 1)
		reading := (*readings)[0]
		require.Positive(t, reading.GetValue())
		require.Equal(t, "risk_scanner", reading.GetSource())
		require.Equal(t, "async:018ffad2-1c32-7f73-8a54-85306c37a315:3:input_message:018ffad2-1c32-7f73-8a54-85306c37a314", reading.GetOperationId())
		require.Equal(t, "linked", reading.GetAttributes()[metering.AttributeRiskPolicyLinkStatus])
		require.Equal(t, "linked", reading.GetAttributes()[metering.AttributeMessageLinkStatus])
	}
}

func TestHandle_MeterFailureDoesNotSuppressFinding(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	findingsPub, findings := capturingPub(t)
	meterPub := gcp.NewMockPublisher[*meteringv1.MeterReading]()
	meterPub.On("Publish", mock.Anything, mock.Anything).Return(errors.New("meter unavailable"))

	h := gitleaks.NewHandler(slog.New(slog.NewTextHandler(&logs, nil)), findingsPub, metering.NewRiskRecorder(meterPub))

	require.NoError(t, h.Handle(t.Context(), newRequest(`SecretAccessKey: `+fakeSecret), gcp.MessageMetadata{}))
	require.NotEmpty(t, *findings)
	require.Contains(t, logs.String(), "meter unavailable")
}

func TestHandle_FindingFailureDoesNotSuppressUsage(t *testing.T) {
	t.Parallel()

	findingErr := errors.New("finding unavailable")
	findingsPub := gcp.NewMockPublisher[*riskv1.Finding]()
	findingsPub.On("Publish", mock.Anything, mock.Anything).Return(findingErr)
	meterPub, readings := capturingMeterPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), findingsPub, metering.NewRiskRecorder(meterPub))

	err := h.Handle(t.Context(), newRequest(`SecretAccessKey: `+fakeSecret), gcp.MessageMetadata{})
	require.ErrorIs(t, err, findingErr)
	require.Len(t, *readings, 1)
	require.Positive(t, (*readings)[0].GetValue())
}

func TestHandle_FailedScanPublishesNoUsage(t *testing.T) {
	t.Parallel()

	findingsPub, findings := capturingPub(t)
	meterPub, readings := capturingMeterPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), findingsPub, metering.NewRiskRecorder(meterPub))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := h.Handle(ctx, newRequest(`SecretAccessKey: `+fakeSecret), gcp.MessageMetadata{})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, *findings)
	require.Empty(t, *readings)
}

func TestHandle_UsageTimestampsDescribeExecutionAndEmission(t *testing.T) {
	t.Parallel()

	findingsPub, _ := capturingPub(t)
	meterPub, readings := capturingMeterPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), findingsPub, metering.NewRiskRecorder(meterPub))
	before := time.Now().UTC()

	require.NoError(t, h.Handle(t.Context(), newRequest("hello world"), gcp.MessageMetadata{}))
	after := time.Now().UTC()

	require.Len(t, *readings, 1)
	occurredAt, err := time.Parse(time.RFC3339Nano, (*readings)[0].GetOccurredAt())
	require.NoError(t, err)
	producedAt, err := time.Parse(time.RFC3339Nano, (*readings)[0].GetProducedAt())
	require.NoError(t, err)
	require.False(t, occurredAt.Before(before))
	require.False(t, occurredAt.After(after))
	require.False(t, producedAt.Before(occurredAt))
	require.False(t, producedAt.After(after))
}

func TestHandle_RedeliveryPublishesStableUsageIdentity(t *testing.T) {
	t.Parallel()

	findingsPub, _ := capturingPub(t)
	meterPub, readings := capturingMeterPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), findingsPub, metering.NewRiskRecorder(meterPub))
	request := newRequest("hello world, this is a normal message")

	require.NoError(t, h.Handle(t.Context(), request, gcp.MessageMetadata{}))
	require.NoError(t, h.Handle(t.Context(), request, gcp.MessageMetadata{}))

	require.Len(t, *readings, 2)
	require.Equal(t, (*readings)[0].GetOperationId(), (*readings)[1].GetOperationId())
	require.Equal(t, (*readings)[0].GetId(), (*readings)[1].GetId())
}

// A batch request scanned over the composed scan surface says so, and the
// published findings carry that surface so reveal slices the same text.
func TestHandle_HonoursRequestFindingSurface(t *testing.T) {
	t.Parallel()

	pub, published := capturingPub(t)
	h := gitleaks.NewHandler(testenv.NewLogger(t), pub, metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))

	req := newRequest("\n{\"command\":\"export AWS_SECRET_ACCESS_KEY=" + fakeSecret + " AWS_ACCESS_KEY_ID=" + fakeAccessKeyID + "\"}")
	req.SetFindingSurface("scan_surface")
	require.NoError(t, h.Handle(t.Context(), req, gcp.MessageMetadata{}))

	require.NotEmpty(t, *published)
	for _, f := range *published {
		require.Equal(t, "scan_surface", f.GetSurface())
		require.Equal(t, f.GetMatch(), req.GetContent()[f.GetStartPos():f.GetEndPos()])
	}
}
