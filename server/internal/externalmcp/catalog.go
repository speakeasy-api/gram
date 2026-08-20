package externalmcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
)

const (
	registrySourceTypePulseV01    = "pulse_v0_1"
	registrySourceTypeOfficialV01 = "official_v0_1"

	registryAuthProfilePulseServerCredentials = "pulse" + "_server_credentials"
	registryAuthProfileNone                   = "none"

	registryCertificationStateCertified = "certified"
)

var (
	ErrCatalogSourceNotFound = errors.New("catalog source not found")
	ErrCatalogSourceDisabled = errors.New("catalog source is not enabled and certified")
	ErrUnknownRegistrySource = errors.New("unknown registry source profile")
)

// CatalogSource is an operator-owned, reviewed registry configuration. It is
// loaded from mcp_registries; request callers never provide a source URL,
// adapter, or auth profile.
type CatalogSource struct {
	Registry             Registry
	SourceType           string
	AuthProfile          string
	CertificationVersion string
	Priority             int32
	SourceKey            string
	Legacy               bool
}

// CatalogService is the single source selection and aggregation boundary for
// dashboard and Platform MCP catalogue reads. It exposes only enabled and
// certified sources with a known, code-reviewed adapter/profile combination.
type CatalogService struct {
	repo     *repo.Queries
	adapters map[string]RegistryReader
}

func NewCatalogService(db *pgxpool.Pool, pulse RegistryReader, official RegistryReader) *CatalogService {
	adapters := make(map[string]RegistryReader, 2)
	if pulse != nil {
		adapters[registryAdapterKey(registrySourceTypePulseV01, registryAuthProfilePulseServerCredentials)] = pulse
	}
	if official != nil {
		adapters[registryAdapterKey(registrySourceTypeOfficialV01, registryAuthProfileNone)] = official
	}
	return &CatalogService{repo: repo.New(db), adapters: adapters}
}

func registryAdapterKey(sourceType, authProfile string) string {
	return sourceType + ":" + authProfile
}

func zeroRegistry() Registry {
	//nolint:exhaustruct // Registry projections intentionally omit unrelated row fields.
	return Registry{ID: uuid.Nil, URL: ""}
}

func zeroCatalogSource() CatalogSource {
	return CatalogSource{
		Registry:             zeroRegistry(),
		SourceType:           "",
		AuthProfile:          "",
		CertificationVersion: "",
		Priority:             0,
		SourceKey:            "",
		Legacy:               false,
	}
}

// List returns a deterministic merged catalogue. A same-specifier entry from
// two sources remains distinct because source identity is part of its
// provenance; only duplicates within a source are collapsed by its adapter.
func (s *CatalogService) List(ctx context.Context, search *string, registryID *uuid.UUID) ([]*types.ExternalMCPServerEntry, error) {
	sources, err := s.sources(ctx, registryID)
	if err != nil {
		return nil, err
	}

	servers := make([]*types.ExternalMCPServerEntry, 0)
	for _, source := range sources {
		adapter, err := s.adapterFor(source)
		if err != nil {
			return nil, err
		}
		result, err := adapter.ListServers(ctx, source.Registry, ListServersParams{Search: search})
		if err != nil {
			return nil, fmt.Errorf("list catalog source %q: %w", source.SourceKey, err)
		}
		servers = append(servers, result.Servers...)
	}

	sort.SliceStable(servers, func(i, j int) bool {
		leftSource, rightSource := sourceKeyForEntry(sources, servers[i]), sourceKeyForEntry(sources, servers[j])
		if leftSource.priority != rightSource.priority {
			return leftSource.priority < rightSource.priority
		}
		if leftSource.key != rightSource.key {
			return leftSource.key < rightSource.key
		}
		return servers[i].RegistrySpecifier < servers[j].RegistrySpecifier
	})
	return servers, nil
}

// Details always re-fetches the selected server through its source-specific
// adapter. Catalogue discovery is not readiness evidence and cached list data
// is never used to materialize a registration.
func (s *CatalogService) Details(ctx context.Context, registryID uuid.UUID, serverName string, allowedRemoteURLs []string) (*ServerDetails, error) {
	sources, err := s.sources(ctx, &registryID)
	if err != nil {
		return nil, err
	}
	if len(sources) != 1 {
		return nil, ErrCatalogSourceNotFound
	}
	adapter, err := s.adapterFor(sources[0])
	if err != nil {
		return nil, err
	}
	details, err := adapter.GetServerDetails(ctx, sources[0].Registry, serverName, allowedRemoteURLs)
	if err != nil {
		return nil, fmt.Errorf("get catalog source %q server details: %w", sources[0].SourceKey, err)
	}
	return details, nil
}

type sourceOrder struct {
	priority int32
	key      string
}

func sourceKeyForEntry(sources []CatalogSource, entry *types.ExternalMCPServerEntry) sourceOrder {
	if entry == nil || entry.RegistryID == nil {
		return sourceOrder{priority: 0, key: ""}
	}
	for _, source := range sources {
		if source.Registry.ID.String() == *entry.RegistryID {
			return sourceOrder{priority: source.Priority, key: source.SourceKey}
		}
	}
	return sourceOrder{priority: 0, key: ""}
}

// Sources returns only registry rows allowed to participate in the shared
// catalogue. Consumers that need their own projection can retain the source
// provenance while delegating fetches back through ReaderFor.
func (s *CatalogService) Sources(ctx context.Context) ([]CatalogSource, error) {
	return s.sources(ctx, nil)
}

// ReaderFor resolves the reviewed adapter/profile for a source returned by
// Sources. It deliberately accepts no caller-supplied URL or profile.
func (s *CatalogService) ReaderFor(source CatalogSource) (RegistryReader, error) {
	return s.adapterFor(source)
}

// Source resolves one enabled and certified source by its opaque database ID.
// It is used by detail paths that must preserve their surface-specific response
// projection while sharing source admission with the aggregate catalogue.
func (s *CatalogService) Source(ctx context.Context, registryID uuid.UUID) (CatalogSource, error) {
	sources, err := s.sources(ctx, &registryID)
	if err != nil {
		return zeroCatalogSource(), err
	}
	if len(sources) != 1 {
		return zeroCatalogSource(), ErrCatalogSourceNotFound
	}
	return sources[0], nil
}

func (s *CatalogService) adapterFor(source CatalogSource) (RegistryReader, error) {
	adapter, ok := s.adapters[registryAdapterKey(source.SourceType, source.AuthProfile)]
	if !ok || adapter == nil {
		return nil, fmt.Errorf("%w: %s/%s", ErrUnknownRegistrySource, source.SourceType, source.AuthProfile)
	}
	return adapter, nil
}

func (s *CatalogService) sources(ctx context.Context, registryID *uuid.UUID) ([]CatalogSource, error) {
	if s == nil || s.repo == nil {
		return nil, ErrCatalogSourceNotFound
	}
	if registryID != nil {
		row, err := s.repo.GetMCPRegistryByID(ctx, *registryID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrCatalogSourceNotFound
			}
			return nil, fmt.Errorf("get catalog source: %w", err)
		}
		source, ok := catalogSourceFromDetailRow(row)
		if !ok {
			return nil, ErrCatalogSourceDisabled
		}
		if _, err := s.adapterFor(source); err != nil {
			return nil, ErrCatalogSourceNotFound
		}
		return []CatalogSource{source}, nil
	}

	rows, err := s.repo.ListMCPRegistries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list catalog sources: %w", err)
	}
	sources := make([]CatalogSource, 0, len(rows))
	for _, row := range rows {
		source, ok := catalogSourceFromRow(row)
		if !ok {
			continue
		}
		// Metadata is necessary but not sufficient: a source participates only
		// when its reviewed adapter/profile is wired into this binary.
		if _, err := s.adapterFor(source); err == nil {
			sources = append(sources, source)
		}
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Priority != sources[j].Priority {
			return sources[i].Priority < sources[j].Priority
		}
		return sources[i].SourceKey < sources[j].SourceKey
	})
	return sources, nil
}

func catalogSourceFromRow(row repo.ListMCPRegistriesRow) (CatalogSource, bool) {
	// Existing Pulse rows predate source metadata. They remain eligible only for
	// the exact historic Pulse URL while this reader compatibility release rolls
	// out; arbitrary legacy URLs cannot enter the shared catalogue.
	if legacyPulseSourceMetadataAbsent(row) && strings.TrimRight(row.Url, "/") == "https://api.pulsemcp.com" {
		return CatalogSource{
			Registry:             Registry{ID: row.ID, URL: row.Url}, //nolint:exhaustruct // Registry projections intentionally omit unrelated row fields.
			SourceType:           registrySourceTypePulseV01,
			AuthProfile:          registryAuthProfilePulseServerCredentials,
			CertificationVersion: "",
			Priority:             0,
			SourceKey:            "legacy-pulse-" + row.ID.String(),
			Legacy:               true,
		}, true
	}
	if !row.Enabled.Valid || !row.Enabled.Bool || !row.CertificationState.Valid || row.CertificationState.String != registryCertificationStateCertified || !row.SourceType.Valid || !row.AuthProfile.Valid || !row.SourceKey.Valid || strings.TrimSpace(row.SourceKey.String) == "" {
		return zeroCatalogSource(), false
	}
	priority := int32(0)
	if row.Priority.Valid {
		priority = row.Priority.Int32
	}
	return CatalogSource{
		Registry:             Registry{ID: row.ID, URL: row.Url}, //nolint:exhaustruct // Registry projections intentionally omit unrelated row fields.
		SourceType:           row.SourceType.String,
		AuthProfile:          row.AuthProfile.String,
		CertificationVersion: row.CertificationVersion.String,
		Priority:             priority,
		SourceKey:            row.SourceKey.String,
		Legacy:               false,
	}, true
}

// legacyPulseSourceMetadataAbsent admits only rows that have not started the
// metadata migration. Partially populated rows must satisfy the full reviewed
// source contract rather than bypassing enabled/certified admission.
func legacyPulseSourceMetadataAbsent(row repo.ListMCPRegistriesRow) bool {
	return !row.SourceType.Valid &&
		!row.AuthProfile.Valid &&
		!row.Enabled.Valid &&
		!row.CertificationState.Valid &&
		!row.CertificationVersion.Valid &&
		!row.Priority.Valid &&
		!row.SourceKey.Valid
}

func catalogSourceFromDetailRow(row repo.GetMCPRegistryByIDRow) (CatalogSource, bool) {
	//nolint:exhaustruct // Detail rows intentionally omit list-only timestamps.
	return catalogSourceFromRow(repo.ListMCPRegistriesRow{
		ID:                   row.ID,
		Name:                 "",
		Url:                  row.Url,
		SourceType:           row.SourceType,
		AuthProfile:          row.AuthProfile,
		Enabled:              row.Enabled,
		CertificationState:   row.CertificationState,
		CertificationVersion: row.CertificationVersion,
		Priority:             row.Priority,
		SourceKey:            row.SourceKey,
	})
}
