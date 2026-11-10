package adminmcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
)

func registryRecord(name string) string {
	return `{"server":{"name":"` + name + `","description":"Demo server","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.test/mcp"}]}}`
}

func newRegistryEntryWriter(t *testing.T, f writeFixture) (*registryEntryWriter, *mcpregistry.Service) {
	t.Helper()
	validator, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	registry := mcpregistry.New(f.db, validator)
	writes := WriteConfig{Enabled: true}
	return &registryEntryWriter{runner: f.runner, registry: registry, writes: writes}, registry
}

func TestRegistryEntryWriterCreatesPublishedEntry(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_registry_create")
	writer, registry := newRegistryEntryWriter(t, f)
	ctx := writeContext(t, f)
	name := "example.test/" + uuid.NewString()

	receipt, err := writer.apply(ctx, CreateRegistryEntryInput{RecordJSON: registryRecord(name)})
	require.NoError(t, err)
	require.Equal(t, name, receipt.Name)
	require.Equal(t, []RegistryTransportSummary{{Type: "streamable-http", Count: 1}}, receipt.Transports)
	require.Contains(t, string(receipt.Record), "https://example.test/mcp", "the result shows the exact record")

	created, err := registry.GetByName(t.Context(), name)
	require.NoError(t, err)
	require.Equal(t, receipt.ID, created.ID.String())
	require.True(t, created.Published)
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationCreateRegistryEntry, "", "executed"))
}

func TestRegistryEntryWriterRejectsInvalidAndDuplicateRecords(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_registry_create_validation")
	writer, registry := newRegistryEntryWriter(t, f)
	ctx := writeContext(t, f)

	_, err := writer.apply(ctx, CreateRegistryEntryInput{RecordJSON: `{"server":{"name":"example.test/broken"}}`})
	require.ErrorContains(t, err, "not a valid registry record")
	_, err = writer.apply(ctx, CreateRegistryEntryInput{RecordJSON: strings.Repeat(" ", maxRegistryRecordInput+1)})
	require.ErrorContains(t, err, "at most")

	owned := `{"server":{"name":"example.test/owned","description":"Demo server","version":"1.0.0","remotes":[]},"_meta":{"com.speakeasy.ai/registry":{"publishedAt":"2026-01-01T00:00:00Z"}}}`
	_, err = writer.apply(ctx, CreateRegistryEntryInput{RecordJSON: owned})
	require.ErrorContains(t, err, "backend-owned", "records creation would always refuse are rejected")

	existing := "example.test/" + uuid.NewString()
	_, err = registry.Create(t.Context(), json.RawMessage(registryRecord(existing)))
	require.NoError(t, err)
	_, err = writer.apply(ctx, CreateRegistryEntryInput{RecordJSON: registryRecord(existing)})
	require.ErrorContains(t, err, "already exists")

	page, err := registry.List(context.WithoutCancel(t.Context()), mcpregistry.ListOptions{Query: existing, Limit: 10})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1, "a duplicate must not create a second entry")
}

func TestRegistryEntryWriterAcceptsMoreRemotesThanAReadPage(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_registry_create_many_remotes")
	writer, _ := newRegistryEntryWriter(t, f)
	ctx := writeContext(t, f)

	remotes := make([]string, 0, maxRegistryPage+1)
	for i := range maxRegistryPage + 1 {
		remotes = append(remotes, `{"type":"streamable-http","url":"https://example.test/mcp/`+strconv.Itoa(i)+`"}`)
	}
	record := `{"server":{"name":"example.test/` + uuid.NewString() + `","description":"Demo server","version":"1.0.0","remotes":[` + strings.Join(remotes, ",") + `]}}`
	_, readable := projectRegistryRecord([]byte(record), maxRegistryPage)
	require.False(t, readable, "reads keep their page bound")

	receipt, err := writer.apply(ctx, CreateRegistryEntryInput{RecordJSON: record})
	require.NoError(t, err)
	require.Equal(t, []RegistryTransportSummary{{Type: "streamable-http", Count: maxRegistryPage + 1}}, receipt.Transports)
}

func TestRegistryCreateErrorMapsNameConflictToStaleState(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, registryCreateError(mcpregistry.ErrConflict), ErrStaleState)
	invalid := registryCreateError(&mcpregistry.InvalidError{Issues: []mcpregistry.Issue{{Path: "/name", Message: "already mapped"}}})
	require.NotErrorIs(t, invalid, ErrStaleState, "a rejected record is not stale state")
	require.ErrorContains(t, invalid, "/name: already mapped")
	err := registryCreateError(mcpregistry.ErrNotFound)
	require.NotErrorIs(t, err, ErrStaleState)
	require.ErrorIs(t, err, mcpregistry.ErrNotFound)
}
