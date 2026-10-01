package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type retainedCatalogFlags struct {
	feature.Provider
	enabled bool
}

func (f *retainedCatalogFlags) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return f.enabled, nil
}

type retainedCatalogRecord struct {
	data      json.RawMessage
	published bool
	reads     int
}

func (r *retainedCatalogRecord) Discover(context.Context, mcpregistry.DiscoveryOptions) (mcpregistry.DiscoveryPage, error) {
	page := mcpregistry.DiscoveryPage{}
	if r.published {
		page.Records = []json.RawMessage{r.data}
	}
	return page, nil
}
func (r *retainedCatalogRecord) GetByName(_ context.Context, name string) (mcpregistry.Entry, error) {
	r.reads++
	if name != "reviewed/mcp" {
		return mcpregistry.Entry{}, errors.New("unknown record")
	}
	return mcpregistry.Entry{Data: r.data, Published: r.published}, nil
}

type deniedCatalogReceiptStore struct {
	recordingRegistrationStore
	receiptErr error
}

func (s *deniedCatalogReceiptStore) FindReceipt(context.Context, Principal, ResolvedProject, CatalogRegistrationRequest, time.Time) (OperationReceipt, bool, error) {
	return OperationReceipt{}, false, s.receiptErr
}

func TestCatalogRetainedUnpublishedIdentity(t *testing.T) {
	t.Parallel()
	for _, initiallyNative := range []bool{true, false} {
		t.Run(fmt.Sprintf("initially_native_%t", initiallyNative), func(t *testing.T) {
			t.Parallel()
			conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_retained_catalog")
			require.NoError(t, err)
			ctx := t.Context()
			err = testrepo.New(conn).DeleteRetainedCatalogSourcesFixture(ctx)
			require.NoError(t, err)
			pulseID := uuid.New()
			err = testrepo.New(conn).InsertRetainedCatalogSourcesFixture(ctx, pulseID)
			require.NoError(t, err)
			native := &retainedCatalogRecord{published: true, data: json.RawMessage(`{"server":{"name":"reviewed/mcp","description":"Native retained record","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://native.test/{region}/mcp","headers":[{"name":"Authorization","isRequired":true,"isSecret":true}],"variables":{"region":{"default":"us","isRequired":true,"choices":["us","eu"]}}}]}}`)}
			pulse := &retainedCatalogRecord{published: true, data: json.RawMessage(`{"server":{"name":"reviewed/mcp","description":"Pulse retained record","version":"2.0.0","remotes":[{"type":"streamable-http","url":"https://pulse.test/mcp"}]}}`)}
			flags := &retainedCatalogFlags{}
			identity := externalmcp.NewCatalogService(conn, externalmcp.NewNativeRegistryReader(pulse), externalmcp.NewNativeRegistryReader(native), flags)
			catalog := NewDynamicRegistryCatalogSources(func(ctx context.Context) ([]RegistryCatalogSource, error) {
				source, err := identity.SelectedSource(ctx, "organization", "organization")
				if err != nil {
					return nil, fmt.Errorf("select catalog source: %w", err)
				}
				reader, err := identity.ReaderFor(source)
				if err != nil {
					return nil, fmt.Errorf("resolve catalog reader: %w", err)
				}
				return []RegistryCatalogSource{{Client: reader, Descriptors: []CatalogDescriptor{BrowserCatalogDescriptor(source.Registry)}}}, nil
			}).WithIdentityService(identity)
			id, record := pulseID, pulse
			if initiallyNative {
				id, record = externalmcp.NativeCatalogRegistryID, native
			}
			flags.enabled = initiallyNative
			record.published = true
			key := "browser-catalog-registry-" + id.String()
			before, err := catalog.Inspect(ctx, key, "reviewed/mcp")
			require.NoError(t, err)
			record.published = false
			_, err = catalog.Inspect(ctx, key, "reviewed/mcp")
			require.ErrorIs(t, err, ErrCatalogRejected)
			flags.enabled = !initiallyNative
			_, err = catalog.Inspect(ctx, key, "reviewed/mcp")
			require.ErrorIs(t, err, ErrCatalogRejected)
			after, err := catalog.InspectIdentity(ctx, key, "reviewed/mcp")
			require.NoError(t, err)
			require.Equal(t, before, after, "retained details must preserve source, configuration, and metadata")
			_, err = catalog.InspectIdentity(ctx, key, "missing/mcp")
			require.Error(t, err)
			denied := &deniedCatalogReceiptStore{recordingRegistrationStore: recordingRegistrationStore{project: ResolvedProject{ID: uuid.New(), Slug: "project"}}, receiptErr: errors.New("receipt authorization denied")}
			deniedService := newRegistrationService(catalog, &testRegistrationGate{enabled: true}, denied)
			readsBeforeDenial := record.reads
			_, err = deniedService.RegisterCatalogMCP(ctx, registrationServicePrincipal(), RegisterCatalogMCPInput{ProjectSlug: "project", ProviderKey: key, CatalogRef: "reviewed/mcp", IdempotencyKey: "denied"})
			require.ErrorIs(t, err, denied.receiptErr)
			require.Equal(t, readsBeforeDenial, record.reads, "receipt authorization must precede retained lookup")
			require.Zero(t, denied.beginCalls)
			for _, status := range []string{receiptStatusPending, receiptStatusSucceeded} {
				receipt := OperationReceipt{ID: uuid.New(), RegistrationID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, Status: status, Replayed: true}
				store := &retainedReceiptStore{recordingRegistrationStore: recordingRegistrationStore{project: ResolvedProject{ID: uuid.New(), Slug: "project"}, converged: receipt, completed: receipt}, receipt: receipt}
				service := newRegistrationService(catalog, &testRegistrationGate{enabled: true}, store)
				result, err := service.RegisterCatalogMCP(ctx, registrationServicePrincipal(), RegisterCatalogMCPInput{ProjectSlug: "project", ProviderKey: key, CatalogRef: "reviewed/mcp", IdempotencyKey: "accepted"})
				require.NoError(t, err)
				require.Equal(t, key, result.ProviderKey)
				require.Zero(t, store.beginCalls)
			}
			store := &recordingRegistrationStore{project: ResolvedProject{ID: uuid.New(), Slug: "project"}, candidate: before.CatalogCandidate}
			service := newRegistrationService(catalog, &testRegistrationGate{enabled: true}, store).WithDashboardURL(&url.URL{Scheme: "https", Host: "dashboard.test"})
			input := IssueSetupHandoffInput{ProjectSlug: "project", RegistrationID: uuid.NewString(), ProviderKey: key, CatalogRef: "reviewed/mcp"}
			setup, err := service.DashboardSetupURL(ctx, registrationServicePrincipal(), input)
			require.NoError(t, err)
			require.Contains(t, setup, "/settings#authentication")
			reads := record.reads
			store.candidate.CatalogRef = "different/mcp"
			_, err = service.DashboardSetupURL(ctx, registrationServicePrincipal(), input)
			require.ErrorIs(t, err, ErrCatalogRejected)
			require.Equal(t, reads, record.reads, "persisted identity must match before retained lookup")
			_, err = service.RegisterCatalogMCP(ctx, registrationServicePrincipal(), RegisterCatalogMCPInput{ProjectSlug: "project", ProviderKey: key, CatalogRef: "reviewed/mcp", IdempotencyKey: "new-request"})
			require.ErrorIs(t, err, ErrCatalogRejected)
			require.Zero(t, store.beginCalls)
			require.Equal(t, reads, record.reads, "new admission must not use retained lookup")
		})
	}
}
