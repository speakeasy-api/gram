package gram

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/channels"
)

// The conversion report is meant to be copied to other people, so it carries
// identifiers and nothing else. Instance labels are account display names, and
// the superseded documents this command reads hold labels that were built from
// the account's email address before the authenticator stopped doing that.
func TestChannelsConversionMappingPrintsIdentifiersOnly(t *testing.T) {
	t.Parallel()
	newID := uuid.MustParse("0e3b6b1a-0000-4000-8000-000000000001")
	var out strings.Builder
	require.NoError(t, writeChannelsConversionMapping(&out, channels.ConversionResult{
		Organizations: 1, Instances: 1, Conversations: 1, Messages: 7, SkippedMessages: 1,
		Mappings: []channels.ConversionMapping{{
			OrganizationID: "org_example",
			Kind:           "instance",
			LegacyID:       "legacy-instance-1",
			ID:             newID,
			Label:          "person@example.test on dashboard",
		}},
	}))
	report := out.String()

	require.Contains(t, report, "org_example")
	require.Contains(t, report, "legacy-instance-1")
	require.Contains(t, report, newID.String())
	require.Contains(t, report, "skipped messages")

	require.NotContains(t, report, "@")
	require.NotContains(t, report, "person")
	require.NotContains(t, report, "on dashboard")
	require.NotContains(t, report, "LABEL")
}
