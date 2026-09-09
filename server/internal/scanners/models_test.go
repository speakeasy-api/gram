package scanners_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func TestParseRiskProvenancePreservesExplicitUnlinkedReasons(t *testing.T) {
	t.Parallel()

	message := riskv1.GitleaksAnalysis_builder{
		RequestId:         new("request-1"),
		ProjectId:         new("018ffad2-1c32-7f73-8a54-85306c37a313"),
		OrganizationId:    new("org-1"),
		ContentPartId:     new("018ffad2-1c32-7f73-8a54-85306c37a316"),
		PolicyLinkReason:  new("draft_rule_test"),
		MessageLinkReason: new("content_part_unlinked"),
	}.Build()

	provenance, err := scanners.ParseRiskProvenance(message, "user_message", "async")
	require.NoError(t, err)
	require.Equal(t, "draft_rule_test", provenance.PolicyLinkReason)
	require.Equal(t, "content_part_unlinked", provenance.MessageLinkReason)
	require.Equal(t, "async", provenance.ExecutionPath)
	require.Equal(t, "user_message", provenance.MessageType)
	require.Equal(t, "async::0:input_part:018ffad2-1c32-7f73-8a54-85306c37a316", provenance.OperationID)
}

func TestParseRiskProvenanceRejectsMalformedOptionalAnchor(t *testing.T) {
	t.Parallel()

	message := riskv1.GitleaksAnalysis_builder{
		RequestId:               new("request-1"),
		ProjectId:               new("018ffad2-1c32-7f73-8a54-85306c37a313"),
		OrganizationId:          new("org-1"),
		RiskPolicyId:            new("018ffad2-1c32-7f73-8a54-85306c37a315"),
		RiskPolicyVersion:       new(int64(3)),
		OriginRiskPolicyId:      new("018ffad2-1c32-7f73-8a54-85306c37a315"),
		OriginRiskPolicyVersion: new(int64(3)),
		ChatMessageId:           new("not-a-uuid"),
	}.Build()

	_, err := scanners.ParseRiskProvenance(message, "user_message", "async")
	require.ErrorContains(t, err, "parse chat message id")
}
