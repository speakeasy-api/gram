package enforcement

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func TestFindingsTruncatesDescriptionAtContractLimit(t *testing.T) {
	t.Parallel()

	description := strings.Repeat("é", enforcereply.MaxFindingDescriptionRunes+1)
	findings := Findings([]scanners.Finding{{RuleID: "custom.long-description", Description: description}})

	require.Len(t, findings, 1)
	require.Len(t, []rune(findings[0].GetDescription()), enforcereply.MaxFindingDescriptionRunes)
	require.Equal(t, strings.Repeat("é", enforcereply.MaxFindingDescriptionRunes), findings[0].GetDescription())
}
