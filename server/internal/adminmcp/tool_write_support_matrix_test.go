package adminmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	adminservice "github.com/speakeasy-api/gram/server/internal/admin"
)

func supportMatrixSnapshot(t *testing.T, db interface {
	Begin(context.Context) (pgx.Tx, error)
}) *gen.SupportMatrix {
	t.Helper()
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	matrix, err := adminservice.ReadSupportMatrixTx(t.Context(), tx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	return matrix
}

func TestSupportMatrixWriterPreservesUnchangedFields(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_support_matrix_write")
	require.NoError(t, adminservice.SeedSupportMatrix(t.Context(), f.db))
	initial := supportMatrixSnapshot(t, f.db)
	require.NotEmpty(t, initial.Methods)
	require.NotEmpty(t, initial.Products)
	require.GreaterOrEqual(t, len(initial.Capabilities), 2)
	methodID, productID := initial.Methods[0].ID, initial.Products[0].ID
	capabilityID, otherCapabilityID := initial.Capabilities[0].ID, initial.Capabilities[1].ID
	mappingKey := methodID + "/" + productID
	initial.Draft.Mappings[mappingKey] = &gen.SupportMapping{
		Applicability: "applicable", Conditions: "keep these conditions",
		Facts: map[string]*gen.SupportFact{
			capabilityID:      {Status: "supported", Note: "keep this note", Verify: true},
			otherCapabilityID: {Status: "unknown", Note: "keep neighbour", Verify: false},
		},
	}
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the domain matrix writer's SQLc queries
	require.NoError(t, err)
	_, err = adminservice.UpdateSupportMatrixTx(t.Context(), tx, &gen.UpdateSupportMatrixPayload{Revision: initial.Revision, Draft: initial.Draft})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))

	writes := WriteConfig{Enabled: true}
	writer := &supportMatrixWriter{runner: f.runner, writes: writes}
	ctx := writeContext(t, f)
	current := supportMatrixSnapshot(t, f.db)
	newNote := "updated note"
	input := SupportMatrixInput{
		Revision: current.Revision,
		Changes:  []SupportMatrixChange{{Kind: "mapping", MethodID: methodID, ProductID: productID, Facts: []SupportMatrixFactChange{{CapabilityID: capabilityID, Note: &newNote}}}},
	}
	receipt, err := writer.apply(ctx, input)
	require.NoError(t, err)
	require.Equal(t, current.Revision, receipt.RevisionBefore)
	require.NotEmpty(t, receipt.Revision)
	require.Len(t, receipt.Changes, 1)
	require.Contains(t, string(receipt.Changes[0].Before), "keep this note")
	require.Contains(t, string(receipt.Changes[0].After), "updated note")
	for _, omitted := range []string{"keep these conditions", "keep neighbour", "status", "verify"} {
		require.NotContains(t, string(receipt.Changes[0].Before), omitted)
		require.NotContains(t, string(receipt.Changes[0].After), omitted)
	}

	saved := supportMatrixSnapshot(t, f.db)
	mapping := saved.Draft.Mappings[mappingKey]
	require.Equal(t, "keep these conditions", mapping.Conditions)
	require.Equal(t, "updated note", mapping.Facts[capabilityID].Note)
	require.Equal(t, "supported", mapping.Facts[capabilityID].Status)
	require.True(t, mapping.Facts[capabilityID].Verify)
	require.Equal(t, &gen.SupportFact{Status: "unknown", Note: "keep neighbour", Verify: false}, mapping.Facts[otherCapabilityID])
	require.Equal(t, current.Draft.References, saved.Draft.References)
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationUpdateSupportMatrix, "", "executed"))

	_, err = writer.apply(ctx, input)
	require.ErrorIs(t, err, ErrStaleState, "repeating a write against the old revision changes nothing")
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationUpdateSupportMatrix, "", "failed"))
}

func TestSupportMatrixWriterRejectsInvalidTargetsNoOpsAndBounds(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_support_matrix_validation")
	require.NoError(t, adminservice.SeedSupportMatrix(t.Context(), f.db))
	matrix := supportMatrixSnapshot(t, f.db)
	writes := WriteConfig{Enabled: true}
	writer := &supportMatrixWriter{runner: f.runner, writes: writes}
	ctx := writeContext(t, f)

	_, err := writer.apply(ctx, SupportMatrixInput{Revision: matrix.Revision, Changes: []SupportMatrixChange{{Kind: "mapping", MethodID: "unknown", ProductID: "unknown", Conditions: new("new")}}})
	require.ErrorContains(t, err, "does not exist")

	tooLong := make([]byte, maxSupportMatrixNote+1)
	longText := string(tooLong)
	_, err = normalizeSupportMatrixChanges([]SupportMatrixChange{{Kind: "mapping", MethodID: "device", ProductID: "claude-code-cli", Conditions: &longText}})
	require.ErrorContains(t, err, "512 bytes")
	tooMany := make([]SupportMatrixChange, maxSupportMatrixChanges+1)
	_, err = writer.apply(ctx, SupportMatrixInput{Revision: matrix.Revision, Changes: tooMany})
	require.ErrorContains(t, err, "between 1 and")
	failuresBefore := countWriteEvents(t, f.db, OperationUpdateSupportMatrix, "", writeEventFailed)
	for _, revision := range []string{"", "not-a-revision", strings.ToUpper(matrix.Revision), matrix.Revision + "0"} {
		_, err = writer.apply(ctx, SupportMatrixInput{Revision: revision, Changes: []SupportMatrixChange{{Kind: "mapping", MethodID: "device", ProductID: "claude-code-cli"}}})
		require.ErrorContains(t, err, "provide the current matrix revision")
	}
	require.Equal(t, failuresBefore, countWriteEvents(t, f.db, OperationUpdateSupportMatrix, "", writeEventFailed), "a malformed revision never reaches the trail")
}

func TestSupportMatrixWriterRefusesStaleRevision(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_support_matrix_stale")
	require.NoError(t, adminservice.SeedSupportMatrix(t.Context(), f.db))
	matrix := supportMatrixSnapshot(t, f.db)
	writes := WriteConfig{Enabled: true}
	writer := &supportMatrixWriter{runner: f.runner, writes: writes}
	ctx := writeContext(t, f)

	// An unrelated global matrix edit still changes the global revision and refuses this write.
	changed := supportMatrixSnapshot(t, f.db)
	changed.Draft.References["device"]["org"].Note = "operator changed it"
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the domain matrix writer's SQLc queries
	require.NoError(t, err)
	_, err = adminservice.UpdateSupportMatrixTx(t.Context(), tx, &gen.UpdateSupportMatrixPayload{Revision: changed.Revision, Draft: changed.Draft})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))

	newNote := "stale note"
	_, err = writer.apply(ctx, SupportMatrixInput{Revision: matrix.Revision, Changes: []SupportMatrixChange{{Kind: "reference", MethodID: "device", Facts: []SupportMatrixFactChange{{CapabilityID: "org", Note: &newNote}}}}})
	require.ErrorIs(t, err, ErrStaleState)
	require.Equal(t, "operator changed it", supportMatrixSnapshot(t, f.db).Draft.References["device"]["org"].Note)
}

func TestSupportMatrixWriterSmallEditOnLargeDraft(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_support_matrix_large")
	require.NoError(t, adminservice.SeedSupportMatrix(t.Context(), f.db))
	matrix := supportMatrixSnapshot(t, f.db)
	populated := 0
	for _, method := range matrix.Methods {
		for _, product := range matrix.Products {
			if populated >= 10 {
				break
			}
			mapping := &gen.SupportMapping{Applicability: "applicable", Facts: map[string]*gen.SupportFact{}}
			for _, capability := range matrix.Capabilities {
				mapping.Facts[capability.ID] = &gen.SupportFact{Status: "supported", Note: strings.Repeat("n", 8000)}
				populated++
			}
			matrix.Draft.Mappings[method.ID+"/"+product.ID] = mapping
		}
	}
	raw, err := json.Marshal(matrix.Draft)
	require.NoError(t, err)
	require.Greater(t, len(raw), MaxBodyBytes)
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the domain matrix writer's SQLc queries
	require.NoError(t, err)
	_, err = adminservice.UpdateSupportMatrixTx(t.Context(), tx, &gen.UpdateSupportMatrixPayload{Revision: matrix.Revision, Draft: matrix.Draft})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	matrix = supportMatrixSnapshot(t, f.db)
	writes := WriteConfig{Enabled: true}
	writer := &supportMatrixWriter{runner: f.runner, writes: writes}
	_, err = writer.apply(writeContext(t, f), SupportMatrixInput{Revision: matrix.Revision, Changes: []SupportMatrixChange{{Kind: "reference", MethodID: "device", Facts: []SupportMatrixFactChange{{CapabilityID: "org", Note: new("small edit")}}}}})
	require.NoError(t, err)
	updated := supportMatrixSnapshot(t, f.db)
	require.Equal(t, "small edit", updated.Draft.References["device"]["org"].Note)
	require.Equal(t, matrix.Draft.Mappings, updated.Draft.Mappings)
}

func TestSupportMatrixDiffIncludesOnlySuppliedFields(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"mapping", "reference"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			matrix := supportMatrixFixture()
			facts := supportMatrixFacts(10)
			for _, fact := range facts {
				fact.Note = strings.Repeat("n", 8000)
				fact.Verify = true
			}
			matrix.Draft.Mappings["method-a/product-a"].Conditions = strings.Repeat("c", 8000)
			matrix.Draft.Mappings["method-a/product-a"].Facts = facts
			matrix.Draft.References["method-a"] = facts
			change := SupportMatrixChange{Kind: kind, MethodID: "method-a", Facts: []SupportMatrixFactChange{{CapabilityID: "capability-0", Status: new("unimplemented"), Verify: new(false)}}}
			if kind == "mapping" {
				change.ProductID = "product-a"
			}
			changes := []SupportMatrixChange{change}
			after, err := applySupportMatrixChanges(matrix.Draft, changes)
			require.NoError(t, err)
			diffs, err := diffSupportMatrixChanges(matrix.Draft, after, changes)
			require.NoError(t, err)
			require.Len(t, diffs, 1)
			require.JSONEq(t, `{"facts":[{"capability_id":"capability-0","status":"supported","verify":true}]}`, string(diffs[0].Before))
			require.JSONEq(t, `{"facts":[{"capability_id":"capability-0","status":"unimplemented","verify":false}]}`, string(diffs[0].After))
			encoded, err := json.Marshal(diffs)
			require.NoError(t, err)
			require.Less(t, len(encoded), MaxBodyBytes)
		})
	}
	t.Run("mapping fields", func(t *testing.T) {
		t.Parallel()
		matrix := supportMatrixFixture()
		changes := []SupportMatrixChange{{Kind: "mapping", MethodID: "method-a", ProductID: "product-a", Conditions: new(""), Applicability: new("na")}}
		after, err := applySupportMatrixChanges(matrix.Draft, changes)
		require.NoError(t, err)
		diffs, err := diffSupportMatrixChanges(matrix.Draft, after, changes)
		require.NoError(t, err)
		require.JSONEq(t, `{"applicability":"applicable","conditions":"private operator condition"}`, string(diffs[0].Before))
		require.JSONEq(t, `{"applicability":"na","conditions":""}`, string(diffs[0].After))
	})
}

func TestSupportMatrixWriterRejectsNoOp(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_support_matrix_noop")
	require.NoError(t, adminservice.SeedSupportMatrix(t.Context(), f.db))
	matrix := supportMatrixSnapshot(t, f.db)
	fact := matrix.Draft.References["device"]["org"]
	writes := WriteConfig{Enabled: true}
	writer := &supportMatrixWriter{runner: f.runner, writes: writes}
	_, err := writer.apply(writeContext(t, f), SupportMatrixInput{Revision: matrix.Revision, Changes: []SupportMatrixChange{{Kind: "reference", MethodID: "device", Facts: []SupportMatrixFactChange{{CapabilityID: "org", Note: &fact.Note}}}}})
	require.ErrorContains(t, err, "nothing would change")
}
