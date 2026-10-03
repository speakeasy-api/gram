package adminmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	adminservice "github.com/speakeasy-api/gram/server/internal/admin"
)

func supportMatrixSnapshot(t *testing.T, db interface {
	Begin(context.Context) (pgx.Tx, error)
},
) *gen.SupportMatrix {
	t.Helper()
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	matrix, err := adminservice.ReadSupportMatrixTx(t.Context(), tx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	return matrix
}

func TestSupportMatrixWriterProposalPreservesUnchangedFieldsAndReplays(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_support_matrix_write")
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

	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationUpdateSupportMatrix: true}} //nolint:exhaustive // Only the reviewed global operation is enabled.
	writer := &supportMatrixWriter{store: f.store, writes: writes, baseURL: "https://staff.example.test" + Path}
	tools := newWriteTools(f.store, writes, writer.baseURL, map[WriteOperation]operationWriter{OperationUpdateSupportMatrix: writer}) //nolint:exhaustive // Only the reviewed operation is dispatched.
	ctx := writeContext(t, f)
	current := supportMatrixSnapshot(t, f.db)
	newNote := "updated note"
	input := PrepareSupportMatrixInput{
		Revision: current.Revision, RetryKey: "support-update-1",
		Changes: []SupportMatrixChange{{Kind: "mapping", MethodID: methodID, ProductID: productID, Facts: []SupportMatrixFactChange{{CapabilityID: capabilityID, Note: &newNote}}}},
	}
	prepared, err := writer.prepare(ctx, input)
	require.NoError(t, err)
	var preview supportMatrixPreview
	require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
	require.Len(t, preview.Changes, 1)
	require.Contains(t, string(preview.Changes[0].Before), "keep this note")
	require.Contains(t, string(preview.Changes[0].After), "updated note")
	for _, omitted := range []string{"keep these conditions", "keep neighbour", "status", "verify"} {
		require.NotContains(t, string(preview.Changes[0].Before), omitted)
		require.NotContains(t, string(preview.Changes[0].After), omitted)
	}

	proposalID, err := uuid.Parse(prepared.ProposalID)
	require.NoError(t, err)
	stored, err := f.store.GetForOwner(t.Context(), proposalID, f.owner)
	require.NoError(t, err)
	view, err := writer.view(stored)
	require.NoError(t, err)
	require.True(t, view.PlatformGlobal)
	require.Empty(t, view.OrganizationID, "global proposals must not invent a tenant target")
	require.Len(t, view.Changes, 1)
	require.Contains(t, view.Changes[0].Before, "keep this note")

	_, err = f.store.Approve(t.Context(), proposalID, f.owner.SubjectURN, stored.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
	result, err := tools.execute(ctx, ProposalIDInput{ProposalID: proposalID.String()})
	require.NoError(t, err)
	require.Equal(t, string(ProposalSucceeded), result.Status)
	var receipt supportMatrixReceipt
	require.NoError(t, json.Unmarshal(result.Result, &receipt))
	require.NotEmpty(t, receipt.Revision)

	saved := supportMatrixSnapshot(t, f.db)
	mapping := saved.Draft.Mappings[mappingKey]
	require.Equal(t, "keep these conditions", mapping.Conditions)
	require.Equal(t, "updated note", mapping.Facts[capabilityID].Note)
	require.Equal(t, "supported", mapping.Facts[capabilityID].Status)
	require.True(t, mapping.Facts[capabilityID].Verify)
	require.Equal(t, &gen.SupportFact{Status: "unknown", Note: "keep neighbour", Verify: false}, mapping.Facts[otherCapabilityID])
	require.Equal(t, current.Draft.References, saved.Draft.References)
	result, err = tools.execute(ctx, ProposalIDInput{ProposalID: proposalID.String()})
	require.NoError(t, err)
	require.True(t, result.Replay)
	require.Equal(t, 1, countWriteEvents(t, f.db, proposalID, "executed"))
}

func TestSupportMatrixWriterRejectsInvalidTargetsNoOpsAndBounds(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_support_matrix_validation")
	require.NoError(t, adminservice.SeedSupportMatrix(t.Context(), f.db))
	matrix := supportMatrixSnapshot(t, f.db)
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationUpdateSupportMatrix: true}} //nolint:exhaustive // Only the reviewed global operation is enabled.
	writer := &supportMatrixWriter{store: f.store, writes: writes}
	ctx := writeContext(t, f)

	_, err := writer.prepare(ctx, PrepareSupportMatrixInput{Revision: matrix.Revision, RetryKey: "bad-target", Changes: []SupportMatrixChange{{Kind: "mapping", MethodID: "unknown", ProductID: "unknown", Conditions: new("new")}}})
	require.ErrorContains(t, err, "does not exist")
	_, err = writer.prepare(ctx, PrepareSupportMatrixInput{Revision: matrix.Revision, RetryKey: "stale", Changes: []SupportMatrixChange{{Kind: "reference", MethodID: "device", Facts: []SupportMatrixFactChange{{CapabilityID: "org", Note: new("stale")}}}}})
	require.NoError(t, err)

	tooLong := make([]byte, maxSupportMatrixNote+1)
	longText := string(tooLong)
	_, err = normalizeSupportMatrixChanges([]SupportMatrixChange{{Kind: "mapping", MethodID: "device", ProductID: "claude-code-cli", Conditions: &longText}})
	require.ErrorContains(t, err, "512 bytes")
	tooMany := make([]SupportMatrixChange, maxSupportMatrixChanges+1)
	_, err = writer.prepare(ctx, PrepareSupportMatrixInput{Revision: matrix.Revision, RetryKey: "too-many", Changes: tooMany})
	require.ErrorContains(t, err, "between 1 and")
}

func TestSupportMatrixWriterStaleRevisionAndGlobalTargetChecks(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_support_matrix_stale")
	require.NoError(t, adminservice.SeedSupportMatrix(t.Context(), f.db))
	matrix := supportMatrixSnapshot(t, f.db)
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationUpdateSupportMatrix: true}} //nolint:exhaustive // Only the reviewed global operation is enabled.
	writer := &supportMatrixWriter{store: f.store, writes: writes}
	ctx := writeContext(t, f)
	newNote := "proposal note"
	prepared, err := writer.prepare(ctx, PrepareSupportMatrixInput{Revision: matrix.Revision, RetryKey: "stale-after-prepare", Changes: []SupportMatrixChange{{Kind: "reference", MethodID: "device", Facts: []SupportMatrixFactChange{{CapabilityID: "org", Note: &newNote}}}}})
	require.NoError(t, err)
	proposalID, err := uuid.Parse(prepared.ProposalID)
	require.NoError(t, err)
	proposal, err := f.store.GetForOwner(t.Context(), proposalID, f.owner)
	require.NoError(t, err)
	badTenant := proposal
	badTenant.Target.OrganizationID = f.orgA
	_, err = writer.view(badTenant)
	require.ErrorIs(t, err, ErrProposalInvalidated)
	badGlobal := proposal
	badGlobal.PlatformGlobal = false
	_, err = writer.view(badGlobal)
	require.ErrorIs(t, err, ErrProposalInvalidated)

	// An unrelated global matrix edit still changes the global revision and invalidates this proposal.
	changed := supportMatrixSnapshot(t, f.db)
	changed.Draft.References["device"]["org"].Note = "operator changed it"
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the domain matrix writer's SQLc queries
	require.NoError(t, err)
	_, err = adminservice.UpdateSupportMatrixTx(t.Context(), tx, &gen.UpdateSupportMatrixPayload{Revision: changed.Revision, Draft: changed.Draft})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	_, err = f.store.Approve(t.Context(), proposalID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.ErrorIs(t, err, ErrStaleState)
}

func TestSupportMatrixWriterSmallEditOnLargeDraft(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_support_matrix_large")
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
	require.Greater(t, len(raw), maxProposalArgumentBytes)
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the domain matrix writer's SQLc queries
	require.NoError(t, err)
	_, err = adminservice.UpdateSupportMatrixTx(t.Context(), tx, &gen.UpdateSupportMatrixPayload{Revision: matrix.Revision, Draft: matrix.Draft})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	matrix = supportMatrixSnapshot(t, f.db)
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationUpdateSupportMatrix: true}} //nolint:exhaustive // Only the matrix operation is enabled.
	writer := &supportMatrixWriter{store: f.store, writes: writes}
	prepared, err := writer.prepare(writeContext(t, f), PrepareSupportMatrixInput{Revision: matrix.Revision, RetryKey: "large-draft", Changes: []SupportMatrixChange{{Kind: "reference", MethodID: "device", Facts: []SupportMatrixFactChange{{CapabilityID: "org", Note: new("small edit")}}}}})
	require.NoError(t, err)
	id := uuid.MustParse(prepared.ProposalID)
	proposal, err := f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), id, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
	authority, err := requireWriteAuthority(writeContext(t, f), writes, OperationUpdateSupportMatrix)
	require.NoError(t, err)
	_, _, err = f.store.Execute(t.Context(), f.owner, id, time.Now(), writer.execution(authority))
	require.NoError(t, err)
	updated := supportMatrixSnapshot(t, f.db)
	require.Equal(t, "small edit", updated.Draft.References["device"]["org"].Note)
	require.Equal(t, matrix.Draft.Mappings, updated.Draft.Mappings)
}

func TestSupportMatrixPreviewIncludesOnlySuppliedFields(t *testing.T) {
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
			preview, err := previewSupportMatrixChanges(matrix.Draft, after, changes)
			require.NoError(t, err)
			require.Len(t, preview, 1)
			require.JSONEq(t, `{"facts":[{"capability_id":"capability-0","status":"supported","verify":true}]}`, string(preview[0].Before))
			require.JSONEq(t, `{"facts":[{"capability_id":"capability-0","status":"unimplemented","verify":false}]}`, string(preview[0].After))
			encoded, err := json.Marshal(preview)
			require.NoError(t, err)
			require.Less(t, len(encoded), maxProposalPreviewBytes)
		})
	}
	t.Run("mapping fields", func(t *testing.T) {
		t.Parallel()
		matrix := supportMatrixFixture()
		changes := []SupportMatrixChange{{Kind: "mapping", MethodID: "method-a", ProductID: "product-a", Conditions: new(""), Applicability: new("na")}}
		after, err := applySupportMatrixChanges(matrix.Draft, changes)
		require.NoError(t, err)
		preview, err := previewSupportMatrixChanges(matrix.Draft, after, changes)
		require.NoError(t, err)
		require.JSONEq(t, `{"applicability":"applicable","conditions":"private operator condition"}`, string(preview[0].Before))
		require.JSONEq(t, `{"applicability":"na","conditions":""}`, string(preview[0].After))
	})
}

func TestSupportMatrixWriterRejectsNoOp(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_support_matrix_noop")
	require.NoError(t, adminservice.SeedSupportMatrix(t.Context(), f.db))
	matrix := supportMatrixSnapshot(t, f.db)
	fact := matrix.Draft.References["device"]["org"]
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationUpdateSupportMatrix: true}} //nolint:exhaustive // Only the reviewed global operation is enabled.
	writer := &supportMatrixWriter{store: f.store, writes: writes}
	_, err := writer.prepare(writeContext(t, f), PrepareSupportMatrixInput{Revision: matrix.Revision, RetryKey: "noop", Changes: []SupportMatrixChange{{Kind: "reference", MethodID: "device", Facts: []SupportMatrixFactChange{{CapabilityID: "org", Note: &fact.Note}}}}})
	require.ErrorContains(t, err, "nothing would change")
}
