package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
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
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_retained_catalog")
	require.NoError(t, err)
	ctx := t.Context()
	_, err = conn.Exec(ctx, `DELETE FROM mcp_registries`)
	require.NoError(t, err)
	pulseID := uuid.New()
	_, err = conn.Exec(ctx, `INSERT INTO mcp_registries (id,name,url,source_type,auth_profile,enabled,certification_state,source_key) VALUES ($1,'Native','https://registry.speakeasy.com','native_v1','none',true,'certified','native'), ($2,'Pulse','https://api.pulsemcp.com','pulse_v0_1','pulse_server_credentials',true,'certified','pulse')`, externalmcp.NativeCatalogRegistryID, pulseID)
	require.NoError(t, err)
	native := &retainedCatalogRecord{published: true, data: json.RawMessage(`{"server":{"name":"reviewed/mcp","description":"Native retained record","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://native.test/{region}/mcp","headers":[{"name":"Authorization","isRequired":true,"isSecret":true}],"variables":{"region":{"default":"us","isRequired":true,"choices":["us","eu"]}}}]}}`)}
	pulse := &retainedCatalogRecord{published: true, data: json.RawMessage(`{"server":{"name":"reviewed/mcp","description":"Pulse retained record","version":"2.0.0","remotes":[{"type":"streamable-http","url":"https://pulse.test/mcp"}]}}`)}
	flags := &retainedCatalogFlags{}
	identity := externalmcp.NewCatalogService(conn, externalmcp.NewNativeRegistryReader(pulse), externalmcp.NewNativeRegistryReader(native), flags)
	catalog := NewDynamicRegistryCatalogSources(func(ctx context.Context) ([]RegistryCatalogSource, error) {
		source, err := identity.SelectedSource(ctx, "organization", "organization")
		if err != nil {
			return nil, err
		}
		reader, err := identity.ReaderFor(source)
		return []RegistryCatalogSource{{Client: reader, Descriptors: []CatalogDescriptor{BrowserCatalogDescriptor(source.Registry)}}}, err
	}).WithIdentityService(identity)
	for _, tc := range []struct {
		name            string
		id              uuid.UUID
		record          *retainedCatalogRecord
		initiallyNative bool
	}{
		{"native_to_pulse", externalmcp.NativeCatalogRegistryID, native, true},
		{"pulse_to_native", pulseID, pulse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags.enabled = tc.initiallyNative
			tc.record.published = true
			key := "browser-catalog-registry-" + tc.id.String()
			before, err := catalog.Inspect(ctx, key, "reviewed/mcp")
			require.NoError(t, err)
			tc.record.published = false
			_, err = catalog.Inspect(ctx, key, "reviewed/mcp")
			require.ErrorIs(t, err, ErrCatalogRejected)
			flags.enabled = !tc.initiallyNative
			_, err = catalog.Inspect(ctx, key, "reviewed/mcp")
			require.ErrorIs(t, err, ErrCatalogRejected)
			after, err := catalog.InspectIdentity(ctx, key, "reviewed/mcp")
			require.NoError(t, err)
			require.Equal(t, before, after, "retained details must preserve source, configuration, and metadata")
			_, err = catalog.InspectIdentity(ctx, key, "missing/mcp")
			require.Error(t, err)
			denied := &deniedCatalogReceiptStore{recordingRegistrationStore: recordingRegistrationStore{project: ResolvedProject{ID: uuid.New(), Slug: "project"}}, receiptErr: errors.New("receipt authorization denied")}
			deniedService := newRegistrationService(catalog, &testRegistrationGate{enabled: true}, denied)
			readsBeforeDenial := tc.record.reads
			_, err = deniedService.RegisterCatalogMCP(ctx, registrationServicePrincipal(), RegisterCatalogMCPInput{ProjectSlug: "project", ProviderKey: key, CatalogRef: "reviewed/mcp", IdempotencyKey: "denied"})
			require.ErrorIs(t, err, denied.receiptErr)
			require.Equal(t, readsBeforeDenial, tc.record.reads, "receipt authorization must precede retained lookup")
			require.Zero(t, denied.beginCalls)
			for _, status := range []string{receiptStatusPending, receiptStatusSucceeded} {
				t.Run(status, func(t *testing.T) {
					receipt := OperationReceipt{ID: uuid.New(), RegistrationID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, Status: status, Replayed: true}
					store := &retainedReceiptStore{recordingRegistrationStore: recordingRegistrationStore{project: ResolvedProject{ID: uuid.New(), Slug: "project"}, converged: receipt, completed: receipt}, receipt: receipt}
					service := newRegistrationService(catalog, &testRegistrationGate{enabled: true}, store)
					result, err := service.RegisterCatalogMCP(ctx, registrationServicePrincipal(), RegisterCatalogMCPInput{ProjectSlug: "project", ProviderKey: key, CatalogRef: "reviewed/mcp", IdempotencyKey: "accepted"})
					require.NoError(t, err)
					require.Equal(t, key, result.ProviderKey)
					require.Zero(t, store.beginCalls)
				})
			}
			store := &recordingRegistrationStore{project: ResolvedProject{ID: uuid.New(), Slug: "project"}, candidate: before.CatalogCandidate}
			service := newRegistrationService(catalog, &testRegistrationGate{enabled: true}, store).WithDashboardURL(&url.URL{Scheme: "https", Host: "dashboard.test"})
			input := IssueSetupHandoffInput{ProjectSlug: "project", RegistrationID: uuid.NewString(), ProviderKey: key, CatalogRef: "reviewed/mcp"}
			setup, err := service.DashboardSetupURL(ctx, registrationServicePrincipal(), input)
			require.NoError(t, err)
			require.Contains(t, setup, "/settings#authentication")
			reads := tc.record.reads
			store.candidate.CatalogRef = "different/mcp"
			_, err = service.DashboardSetupURL(ctx, registrationServicePrincipal(), input)
			require.ErrorIs(t, err, ErrCatalogRejected)
			require.Equal(t, reads, tc.record.reads, "persisted identity must match before retained lookup")
			_, err = service.RegisterCatalogMCP(ctx, registrationServicePrincipal(), RegisterCatalogMCPInput{ProjectSlug: "project", ProviderKey: key, CatalogRef: "reviewed/mcp", IdempotencyKey: "new-request"})
			require.ErrorIs(t, err, ErrCatalogRejected)
			require.Zero(t, store.beginCalls)
			require.Equal(t, reads, tc.record.reads, "new admission must not use retained lookup")
		})
	}
}
