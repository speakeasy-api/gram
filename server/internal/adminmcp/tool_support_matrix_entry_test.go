package adminmcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

func TestProjectSupportMatrixEntryReturnsBoundedEditableFields(t *testing.T) {
	t.Parallel()
	matrix := supportMatrixFixture()
	mapping, err := projectSupportMatrixEntry(matrix, GetSupportMatrixEntryInput{Kind: "mapping", MethodID: "method-a", ProductID: "product-a"})
	require.NoError(t, err)
	require.Equal(t, "private operator condition", mapping.Conditions)
	require.Equal(t, []SupportMatrixEntryFact{{CapabilityID: "capability-a", Status: "supported", Note: "private coverage note", Verify: true}}, mapping.Facts)
	require.Equal(t, "private revision", mapping.Revision)

	reference, err := projectSupportMatrixEntry(matrix, GetSupportMatrixEntryInput{Kind: "reference", MethodID: "method-a"})
	require.NoError(t, err)
	require.Equal(t, []SupportMatrixEntryFact{{CapabilityID: "capability-a", Status: "partial", Note: "private reference note", Verify: true}}, reference.Facts)

	_, err = projectSupportMatrixEntry(matrix, GetSupportMatrixEntryInput{Kind: "mapping", MethodID: "method-a", ProductID: "missing"})
	require.ErrorContains(t, err, "not found")
	err = validateSupportMatrixEntryTarget(GetSupportMatrixEntryInput{Kind: "reference", MethodID: "method-a", ProductID: "product-a"})
	require.Error(t, err)
}

func TestSupportMatrixProjectionRejectsUnboundedNotes(t *testing.T) {
	t.Parallel()
	matrix := &gen.SupportMatrix{Revision: "revision", Draft: &gen.SupportDraft{Mappings: map[string]*gen.SupportMapping{
		"method-a/product-a": {Applicability: "applicable", Conditions: string(make([]byte, maxSupportMatrixText+1)), Facts: map[string]*gen.SupportFact{}},
	}, References: map[string]map[string]*gen.SupportFact{}}}
	_, err := projectSupportMatrixEntry(matrix, GetSupportMatrixEntryInput{Kind: "mapping", MethodID: "method-a", ProductID: "product-a"})
	require.ErrorIs(t, err, errSupportMatrixUnavailable)
}
