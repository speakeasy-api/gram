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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
)

const (
	registrySourceTypeNative      = "native_v1"
	registrySourceTypePulseV01    = "pulse_v0_1"
	registrySourceTypeOfficialV01 = "official_v0_1"

	registryAuthProfilePulseServerCredentials = "pulse" + "_server_credentials"
	registryAuthProfileNone                   = "none"

	registryCertificationStateCertified = "certified"
)

var (
	errCatalogEmpty          = errors.New("catalog has no configured sources")
	ErrCatalogSourceNotFound = errors.New("catalog source not found")
	ErrCatalogSourceDisabled = errors.New("catalog source is not enabled and certified")
	ErrUnknownRegistrySource = errors.New("unknown registry source profile")
)

// CatalogSource is an operator-owned, reviewed registry configuration. It is
// loaded from mcp_registries; request callers never provide a source URL,
// adapter, or auth profile.
type CatalogSource struct {
	Name                 string
	Registry             Registry
	SourceType           string
	AuthProfile          string
	CertificationVersion string
	Priority             int32
	SourceKey            string
	Legacy               bool
}

// CatalogService is the single organization-selected source boundary for
// dashboard and Platform MCP catalogue reads. It exposes only enabled and
// certified sources with a known, code-reviewed adapter/profile combination.
type CatalogService struct {
	repo     catalogRepository
	features feature.Provider
	adapters map[string]RegistryReader
}

// NewCatalogService temporarily accepts both RegistryReader implementations for
// the organization-targeted catalog cutover. Remove the legacy reader after rollout.
func NewCatalogService(db *pgxpool.Pool, legacy RegistryReader, native RegistryReader, providers ...feature.Provider) *CatalogService {
	adapters := make(map[string]RegistryReader, 2)
	if legacy != nil {
		adapters[registryAdapterKey(registrySourceTypePulseV01, registryAuthProfilePulseServerCredentials)] = legacy
	}
	if native != nil {
		adapters[registryAdapterKey(registrySourceTypeNative, registryAuthProfileNone)] = native
	}
	var flags feature.Provider
	if len(providers) > 0 {
		flags = providers[0]
	}
	return &CatalogService{repo: repo.New(db), adapters: adapters, features: flags}
}

func registryAdapterKey(sourceType, authProfile string) string {
	return sourceType + ":" + authProfile
}

func zeroRegistry() Registry {
	return Registry{ID: uuid.Nil, URL: ""}
}

func zeroCatalogSource() CatalogSource {
	return CatalogSource{
		Registry:             zeroRegistry(),
		Name:                 "",
		SourceType:           "",
		AuthProfile:          "",
		CertificationVersion: "",
		Priority:             0,
		SourceKey:            "",
		Legacy:               false,
	}
}

// List returns the single selected catalog, never an aggregate.
func (s *CatalogService) List(ctx context.Context, search *string, registryID *uuid.UUID) ([]*types.ExternalMCPServerEntry, error) {
	sources, err := s.sources(ctx, registryID)
	if registryID == nil && errors.Is(err, errCatalogEmpty) {
		return []*types.ExternalMCPServerEntry{}, nil
	}
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
	if source.SourceType == registrySourceTypeNative && source.Registry.ID != NativeCatalogRegistryID {
		return nil, ErrCatalogSourceNotFound
	}
	adapter, ok := s.adapters[registryAdapterKey(source.SourceType, source.AuthProfile)]
	if !ok || adapter == nil {
		return nil, fmt.Errorf("%w: %s/%s", ErrUnknownRegistrySource, source.SourceType, source.AuthProfile)
	}
	return adapter, nil
}

// CatalogOrganization is explicit at background/agent call sites; dashboard wrappers
// below use the authenticated organization, never a project or user identity.
func (s *CatalogService) sources(ctx context.Context, registryID *uuid.UUID) ([]CatalogSource, error) {
	var orgID, slug string
	if auth, ok := contextvalues.GetAuthContext(ctx); ok && auth != nil {
		orgID, slug = auth.ActiveOrganizationID, auth.OrganizationSlug
	}
	source, err := s.SelectedSource(ctx, orgID, slug)
	if err != nil {
		return nil, err
	}
	if registryID != nil && source.Registry.ID != *registryID {
		return nil, ErrCatalogSourceNotFound
	}
	return []CatalogSource{source}, nil
}

// SelectedSource selects exactly one catalog. Flag errors/off/unknown select the legacy catalog;
// missing or failing selected sources never fall back to the other catalog.
func (s *CatalogService) SelectedSource(ctx context.Context, organizationID, organizationSlug string) (CatalogSource, error) {
	if s == nil || s.repo == nil {
		return zeroCatalogSource(), ErrCatalogSourceNotFound
	}
	native, _ := feature.GramMCPCatalogEnabled(ctx, s.features, organizationID, organizationSlug)
	rows, err := s.repo.ListMCPRegistries(ctx)
	if err != nil {
		return zeroCatalogSource(), fmt.Errorf("list catalog sources: %w", err)
	}
	if len(rows) == 0 {
		return zeroCatalogSource(), fmt.Errorf("%w: %w", ErrCatalogSourceNotFound, errCatalogEmpty)
	}
	var selected *CatalogSource
	for _, row := range rows {
		source, ok := catalogSourceFromRow(row)
		if !ok {
			continue
		}
		matches := source.SourceType == registrySourceTypePulseV01
		if native {
			matches = source.SourceType == registrySourceTypeNative && source.Registry.ID == NativeCatalogRegistryID
		}
		if !matches {
			continue
		}
		if selected != nil {
			return zeroCatalogSource(), fmt.Errorf("ambiguous selected catalog source")
		}
		selected = &source
	}
	if selected == nil {
		return zeroCatalogSource(), ErrCatalogSourceNotFound
	}
	if _, err := s.adapterFor(*selected); err != nil {
		return zeroCatalogSource(), err
	}
	return *selected, nil
}

// SelectSource admits an explicit registry ID for a NEW selection.
func (s *CatalogService) SelectSource(ctx context.Context, organizationID, organizationSlug string, id uuid.UUID) (CatalogSource, error) {
	source, err := s.SelectedSource(ctx, organizationID, organizationSlug)
	if err != nil {
		return zeroCatalogSource(), err
	}
	if source.Registry.ID != id {
		return zeroCatalogSource(), ErrCatalogSourceNotFound
	}
	return source, nil
}

// IdentitySource resolves persisted/accepted work independently of rollout state.
// It still requires a known enabled/certified source; it never switches identities.
func (s *CatalogService) IdentitySource(ctx context.Context, id uuid.UUID) (CatalogSource, error) {
	if s == nil || s.repo == nil {
		return zeroCatalogSource(), ErrCatalogSourceNotFound
	}
	row, err := s.repo.GetMCPRegistryByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return zeroCatalogSource(), ErrCatalogSourceNotFound
	}
	if err != nil {
		return zeroCatalogSource(), fmt.Errorf("get catalog identity: %w", err)
	}
	source, ok := catalogSourceFromDetailRow(row)
	if !ok {
		return zeroCatalogSource(), ErrCatalogSourceDisabled
	}
	if _, err := s.adapterFor(source); err != nil {
		return zeroCatalogSource(), err
	}
	return source, nil
}

// IdentityDetails reads an installed or accepted registry identity without new-selection admission.
func (s *CatalogService) IdentityDetails(ctx context.Context, id uuid.UUID, name string, allowed []string) (*ServerDetails, error) {
	source, err := s.IdentitySource(ctx, id)
	if err != nil {
		return nil, err
	}
	reader, err := s.ReaderFor(source)
	if err != nil {
		return nil, err
	}
	details, err := reader.GetServerDetails(ctx, source.Registry, name, allowed)
	if err != nil {
		return nil, fmt.Errorf("read catalog identity details: %w", err)
	}
	return details, nil
}

type catalogRepository interface {
	ListMCPRegistries(context.Context) ([]repo.ListMCPRegistriesRow, error)
	GetMCPRegistryByID(context.Context, uuid.UUID) (repo.GetMCPRegistryByIDRow, error)
}

// NativeCatalogRegistryID is a catalog namespace, never an individual entry UUID.
var NativeCatalogRegistryID = uuid.MustParse("7de663c2-4975-4a3d-a7d4-707866aaf1be")

const NativeCatalogRegistryURL = "https://registry.speakeasy.com"

// EnsureNativeCatalogSource retains a stable namespace using existing metadata.
// It never changes legacy catalog rows or overwrites an operator-disabled native source.
func EnsureNativeCatalogSource(ctx context.Context, db *pgxpool.Pool) error {
	queries := repo.New(db)
	err := queries.EnsureNativeCatalogSource(ctx, repo.EnsureNativeCatalogSourceParams{ID: NativeCatalogRegistryID, Url: NativeCatalogRegistryURL})
	if err != nil {
		return fmt.Errorf("ensure native catalog namespace: %w", err)
	}
	valid, err := queries.ValidateNativeCatalogSource(ctx, repo.ValidateNativeCatalogSourceParams{ID: NativeCatalogRegistryID, Url: NativeCatalogRegistryURL})
	if err != nil {
		return fmt.Errorf("verify native catalog namespace: %w", err)
	}
	if !valid {
		return fmt.Errorf("native catalog namespace conflicts with existing source")
	}
	return nil
}

func catalogSourceFromRow(row repo.ListMCPRegistriesRow) (CatalogSource, bool) {
	// Existing Pulse rows predate source metadata. They remain eligible only for
	// the exact historic Pulse URL while this reader compatibility release rolls
	// out; arbitrary legacy URLs cannot enter the shared catalogue.
	if legacyPulseSourceMetadataAbsent(row) && strings.TrimRight(row.Url, "/") == "https://api.pulsemcp.com" {
		return CatalogSource{
			Name:                 row.Name,
			Registry:             Registry{ID: row.ID, URL: row.Url},
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
		Name:                 row.Name,
		Registry:             Registry{ID: row.ID, URL: row.Url},
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

// AdmitNewEntry checks the organization-selected source and current native entry
// eligibility. Persisted identities and accepted continuations use IdentityDetails.
func (s *CatalogService) AdmitNewEntry(ctx context.Context, organizationID, organizationSlug string, id uuid.UUID, name string) error {
	source, err := s.SelectSource(ctx, organizationID, organizationSlug, id)
	if err != nil {
		return err
	}
	if source.SourceType != registrySourceTypeNative {
		return nil
	}
	reader, err := s.ReaderFor(source)
	if err != nil {
		return err
	}
	native, ok := reader.(*NativeRegistryReader)
	if !ok {
		return ErrUnknownRegistrySource
	}
	_, err = native.discoveryEntry(ctx, name)
	return err
}

// discoveryEntry deliberately does not use the retained GetByName lookup.
// The indexed discovery lookup enforces publication, lifecycle and validity.
func (r *NativeRegistryReader) discoveryEntry(ctx context.Context, name string) (mcpregistry.Entry, error) {
	source, ok := r.source.(interface {
		LookupDiscoveryVersion(context.Context, string, string, bool) (mcpregistry.Entry, error)
	})
	if !ok {
		return mcpregistry.Entry{}, ErrUnknownRegistrySource
	}
	entry, err := source.LookupDiscoveryVersion(ctx, name, "latest", false)
	if err != nil {
		return mcpregistry.Entry{}, fmt.Errorf("lookup catalog discovery version: %w", err)
	}
	return entry, nil
}
