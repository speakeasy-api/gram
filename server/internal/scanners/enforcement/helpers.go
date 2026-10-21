package enforcement

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// DefaultMaxRequestAge is the maximum useful age of a realtime request.
	DefaultMaxRequestAge = 30 * time.Second

	// MaxReplyReasonRunes matches the EnforcementReply reason contract.
	MaxReplyReasonRunes = 256
)

// NewTruncationCounter records engine-local input cuts separately from transport cuts.
func NewTruncationCounter(meterProvider metric.MeterProvider) metric.Int64Counter {
	counter, _ := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/scanners/enforcement").Int64Counter(
		"risk.enforcement.engine_truncations",
		metric.WithDescription("Number of enforcement scans truncated to an engine input limit"),
		metric.WithUnit("{scan}"),
	)
	return counter
}

// RecordTruncation records one engine-local input cut.
func RecordTruncation(ctx context.Context, counter metric.Int64Counter, scanner string) {
	if counter != nil {
		counter.Add(ctx, 1, metric.WithAttributes(attribute.String("gram.risk.enforcement.scanner", scanner)))
	}
}

// Request identifies the common structural fields of an enforcement request.
type Request struct {
	CreatedAt      string
	OrganizationID string
	ProjectID      string
	ReplyURN       string
	MaxRequestAge  time.Duration
}

// ValidateRequest validates common fields and returns reply correlation data.
func ValidateRequest(in Request) (correlationID string, stale bool, err error) {
	createdAt, err := time.Parse(time.RFC3339Nano, in.CreatedAt)
	if err != nil {
		return "", false, fmt.Errorf("parse enforcement created_at: %w", err)
	}
	if in.OrganizationID == "" {
		return "", false, errors.New("enforcement organization id is required")
	}
	if in.ProjectID == "" {
		return "", false, errors.New("enforcement project id is required")
	}
	if in.ReplyURN == "" {
		return "", false, errors.New("enforcement reply urn attribute is required")
	}
	_, correlationID, err = enforcereply.ParseReplyURN(in.ReplyURN)
	if err != nil {
		return "", false, fmt.Errorf("parse enforcement reply urn: %w", err)
	}
	maxAge := in.MaxRequestAge
	if maxAge <= 0 {
		maxAge = DefaultMaxRequestAge
	}
	age := time.Since(createdAt)
	return correlationID, age > maxAge || age < -maxAge, nil
}

// TruncateFields fills one UTF-8-safe byte budget in argument order.
func TruncateFields(maxBytes int, fields ...*string) bool {
	remaining := max(maxBytes, 0)
	truncated := false
	for _, field := range fields {
		if field == nil {
			continue
		}
		if len(*field) <= remaining {
			remaining -= len(*field)
			continue
		}
		cut := remaining
		for cut > 0 && !utf8.RuneStart((*field)[cut]) {
			cut--
		}
		*field = (*field)[:cut]
		remaining -= cut
		truncated = true
	}
	return truncated
}

// Findings maps scanner findings onto the raw-content-free reply contract.
func Findings(in []scanners.Finding) []*riskv1.EnforcementFinding {
	out := make([]*riskv1.EnforcementFinding, 0, len(in))
	for _, finding := range in {
		out = append(out, riskv1.EnforcementFinding_builder{
			RuleId:        new(finding.RuleID),
			Category:      new(string(categories.Classify(finding.Source, finding.RuleID))),
			Score:         new(finding.Confidence),
			StartPos:      new(conv.SafeInt32(finding.StartPos)),
			EndPos:        new(conv.SafeInt32(finding.EndPos)),
			Surface:       new(scanners.FindingSurface(finding.Source, finding.Field, finding.Path)),
			Field:         new(finding.Field),
			Path:          new(finding.Path),
			ToolCallId:    new(finding.McpLookupToolCallID),
			MaskedPreview: new(""),
			Fingerprint:   new(""),
			Description:   new(""),
		}.Build())
	}
	return out
}

// ReplyURN returns the request-reply transport attribute.
func ReplyURN(meta map[string]string) string {
	return meta[requestreply.ReplyURNAttribute]
}
