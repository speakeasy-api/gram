package adminmcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
)

func registryRecord(name string) string {
	return `{"server":{"name":"` + name + `","description":"Demo server","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.test/mcp"}]}}`
}

func newRegistryEntryWriter(t *testing.T, f proposalFixture) (*registryEntryWriter, *writeTools, *mcpregistry.Service) {
	t.Helper()
	validator, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	registry := mcpregistry.New(f.db, validator)
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationCreateRegistryEntry: true}} //nolint:exhaustive // Only the reviewed global operation is enabled.
	writer := &registryEntryWriter{store: f.store, registry: registry, writes: writes, baseURL: "https://staff.example.test" + Path}
	tools := newWriteTools(f.store, writes, writer.baseURL, map[WriteOperation]operationWriter{OperationCreateRegistryEntry: writer}) //nolint:exhaustive // Only the reviewed operation is dispatched.
	return writer, tools, registry
}

func approveRegistryProposal(t *testing.T, f proposalFixture, writer *registryEntryWriter, prepared ProposalOutput) uuid.UUID {
	t.Helper()
	proposalID, err := uuid.Parse(prepared.ProposalID)
	require.NoError(t, err)
	stored, err := f.store.GetForOwner(t.Context(), proposalID, f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), proposalID, f.owner.SubjectURN, stored.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
	return proposalID
}

func TestRegistryEntryWriterCreatesPublishedEntryAndReplays(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_registry_create")
	writer, tools, registry := newRegistryEntryWriter(t, f)
	ctx := writeContext(t, f)
	name := "example.test/" + uuid.NewString()

	prepared, err := writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: registryRecord(name), RetryKey: "registry-create-1"})
	require.NoError(t, err)
	require.Equal(t, string(ProposalPendingApproval), prepared.Status)
	var preview registryCreatePreview
	require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
	require.Equal(t, name, preview.Name)
	require.Equal(t, []RegistryTransportSummary{{Type: "streamable-http", Count: 1}}, preview.Transports)

	proposalID, err := uuid.Parse(prepared.ProposalID)
	require.NoError(t, err)
	stored, err := f.store.GetForOwner(t.Context(), proposalID, f.owner)
	require.NoError(t, err)
	view, err := writer.view(stored)
	require.NoError(t, err)
	require.True(t, view.PlatformGlobal)
	require.Empty(t, view.OrganizationID, "global proposals must not invent a tenant target")
	require.Contains(t, view.Summary, name)
	require.Contains(t, view.Changes[len(view.Changes)-1].After, "https://example.test/mcp", "approval shows the exact record")

	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrProposalNotApproved)
	approveRegistryProposal(t, f, writer, prepared)
	result, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.Equal(t, string(ProposalSucceeded), result.Status)
	var receipt registryCreateReceipt
	require.NoError(t, json.Unmarshal(result.Result, &receipt))
	require.Equal(t, name, receipt.Name)

	created, err := registry.GetByName(t.Context(), name)
	require.NoError(t, err)
	require.Equal(t, receipt.ID, created.ID.String())
	require.True(t, created.Published)

	result, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.True(t, result.Replay)
	require.Equal(t, 1, countWriteEvents(t, f.db, proposalID, "executed"))
}

func TestRegistryEntryWriterRejectsInvalidAndDuplicateRecords(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_registry_create_validation")
	writer, _, registry := newRegistryEntryWriter(t, f)
	ctx := writeContext(t, f)

	_, err := writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: `{"server":{"name":"example.test/broken"}}`, RetryKey: "invalid"})
	require.ErrorContains(t, err, "not a valid registry record")
	_, err = writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: registryRecord("example.test/no-key")})
	require.ErrorContains(t, err, "retry key")
	_, err = writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: strings.Repeat(" ", maxRegistryRecordInput+1), RetryKey: "too-large"})
	require.ErrorContains(t, err, "at most")

	owned := `{"server":{"name":"example.test/owned","description":"Demo server","version":"1.0.0","remotes":[]},"_meta":{"com.speakeasy.ai/registry":{"publishedAt":"2026-01-01T00:00:00Z"}}}`
	_, err = writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: owned, RetryKey: "owned-metadata"})
	require.ErrorContains(t, err, "backend-owned", "records creation would always refuse are rejected before approval")

	existing := "example.test/" + uuid.NewString()
	_, err = registry.Create(t.Context(), json.RawMessage(registryRecord(existing)))
	require.NoError(t, err)
	_, err = writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: registryRecord(existing), RetryKey: "duplicate"})
	require.ErrorContains(t, err, "already exists")
}

func TestRegistryEntryWriterStaleWhenNameTakenAfterPrepare(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_registry_create_stale")
	writer, tools, registry := newRegistryEntryWriter(t, f)
	ctx := writeContext(t, f)
	name := "example.test/" + uuid.NewString()

	prepared, err := writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: registryRecord(name), RetryKey: "stale-create"})
	require.NoError(t, err)
	approveRegistryProposal(t, f, writer, prepared)

	_, err = registry.Create(t.Context(), json.RawMessage(registryRecord(name)))
	require.NoError(t, err)
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrStaleState)

	page, err := registry.List(context.WithoutCancel(t.Context()), mcpregistry.ListOptions{Query: name, Limit: 10})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1, "a stale proposal must not create a second entry")
}

func TestRegistryEntryWriterViewRejectsTenantTarget(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_registry_create_target")
	writer, _, _ := newRegistryEntryWriter(t, f)
	ctx := writeContext(t, f)

	prepared, err := writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: registryRecord("example.test/" + uuid.NewString()), RetryKey: "target"})
	require.NoError(t, err)
	proposalID, err := uuid.Parse(prepared.ProposalID)
	require.NoError(t, err)
	proposal, err := f.store.GetForOwner(t.Context(), proposalID, f.owner)
	require.NoError(t, err)
	proposal.Target.OrganizationID = f.orgA
	_, err = writer.view(proposal)
	require.ErrorIs(t, err, ErrProposalInvalidated)
}

func TestRegistryEntryWriterAcceptsMoreRemotesThanAReadPage(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_registry_create_many_remotes")
	writer, _, _ := newRegistryEntryWriter(t, f)
	ctx := writeContext(t, f)

	remotes := make([]string, 0, maxRegistryPage+1)
	for i := range maxRegistryPage + 1 {
		remotes = append(remotes, `{"type":"streamable-http","url":"https://example.test/mcp/`+strconv.Itoa(i)+`"}`)
	}
	record := `{"server":{"name":"example.test/` + uuid.NewString() + `","description":"Demo server","version":"1.0.0","remotes":[` + strings.Join(remotes, ",") + `]}}`
	_, readable := projectRegistryRecord([]byte(record), maxRegistryPage)
	require.False(t, readable, "reads keep their page bound")

	prepared, err := writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: record, RetryKey: "many-remotes"})
	require.NoError(t, err)
	var preview registryCreatePreview
	require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
	require.Equal(t, []RegistryTransportSummary{{Type: "streamable-http", Count: maxRegistryPage + 1}}, preview.Transports)
}

func TestRegistryEntryWriterReplaysRetryAfterExecution(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_registry_create_retry")
	writer, tools, _ := newRegistryEntryWriter(t, f)
	ctx := writeContext(t, f)
	record := registryRecord("example.test/" + uuid.NewString())

	prepared, err := writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: record, RetryKey: "retry-after-execute"})
	require.NoError(t, err)
	approveRegistryProposal(t, f, writer, prepared)
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)

	retried, err := writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: record, RetryKey: "retry-after-execute"})
	require.NoError(t, err, "a retry replays the stored proposal instead of refusing the now-taken name")
	require.True(t, retried.Replay)
	require.Equal(t, prepared.ProposalID, retried.ProposalID)
	require.Equal(t, string(ProposalSucceeded), retried.Status)

	_, err = writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: registryRecord("example.test/" + uuid.NewString()), RetryKey: "retry-after-execute"})
	require.ErrorIs(t, err, ErrProposalConflict, "a reused retry key for a different change is refused")
	_, err = writer.prepare(ctx, PrepareCreateRegistryEntryInput{RecordJSON: record, RetryKey: "fresh-key"})
	require.ErrorContains(t, err, "already exists", "a new retry key still gets the name check")
}

func TestRegistryCreateErrorMapsNameConflictToStaleState(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, registryCreateError(mcpregistry.ErrConflict), ErrStaleState)
	require.ErrorIs(t, registryCreateError(&mcpregistry.InvalidError{}), ErrStaleState, "a state-dependent rejection after validation is stale")
	err := registryCreateError(mcpregistry.ErrNotFound)
	require.NotErrorIs(t, err, ErrStaleState)
	require.ErrorIs(t, err, mcpregistry.ErrNotFound)
}
