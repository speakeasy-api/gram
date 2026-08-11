package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
)

var (
	ErrCatalogUnavailable = errors.New("platform mcp catalog unavailable")
	ErrCatalogRejected    = errors.New("platform mcp catalog candidate rejected")
)

// CatalogDescriptor binds a Platform catalogue source to a server-owned registry.
// CanonicalRef and AllowedRemoteURL are optional only for a registry-wide source:
// the exact entry and remote are then revalidated on every inspection. Existing
// reviewed descriptors keep pinning both values for a narrower provider contract.
type CatalogDescriptor struct {
	ProviderKey      string
	Registry         externalmcp.Registry
	CanonicalRef     string
	AllowedRemoteURL string
	SetupIntent      string
}

type CatalogCandidate struct {
	// ProviderKey is a server-issued opaque registry-source identity. It is not a
	// provider credential, remote URL, or client-supplied provider configuration.
	ProviderKey string `json:"provider_key"`
	CatalogRef  string `json:"catalog_ref"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	ToolCount   int    `json:"tool_count"`
	SetupIntent string `json:"setup_intent"`
}

type CatalogConfigurationField struct {
	Key         string   `json:"key"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Required    bool     `json:"required"`
	Secret      bool     `json:"secret"`
	Default     string   `json:"default,omitempty"`
	Choices     []string `json:"choices,omitempty"`
}

type CatalogDetails struct {
	CatalogCandidate
	Transport              string                      `json:"transport"`
	ToolNames              []string                    `json:"tool_names"`
	Configuration          []CatalogConfigurationField `json:"configuration"`
	RequiresDashboardSetup bool                        `json:"requires_dashboard_setup"`

	// remoteURLTemplate is derived only from an inspected server-owned registry
	// entry. It is never returned to Platform MCP callers and only becomes a
	// concrete Remote MCP URL after declared non-secret variables are validated.
	remoteURLTemplate string
	// remoteURL is retained for existing focused fixtures/tests that construct
	// an already-resolved reviewed candidate directly.
	remoteURL string
}

type Catalog interface {
	Search(ctx context.Context, query string) ([]CatalogCandidate, error)
	Inspect(ctx context.Context, providerKey, catalogRef string) (CatalogDetails, error)
}

type RegistryCatalogSource struct {
	// Client is constructed by server composition. The local fixture uses a
	// development-CA-aware client; normal browser-catalogue registries share the
	// standard client. It is never chosen from Platform MCP input.
	Client      *externalmcp.RegistryClient
	Descriptors []CatalogDescriptor
}

type registryCatalogDescriptor struct {
	CatalogDescriptor
	client *externalmcp.RegistryClient
}

type RegistryCatalog struct {
	descriptors map[string]registryCatalogDescriptor
}

func NewRegistryCatalog(client *externalmcp.RegistryClient, descriptors []CatalogDescriptor) *RegistryCatalog {
	return NewRegistryCatalogSources([]RegistryCatalogSource{{Client: client, Descriptors: descriptors}})
}

// BrowserCatalogDescriptor maps one configured browser-catalogue registry to a
// stable opaque Platform MCP source identity. The registry row and its endpoint
// remain server-owned; callers only receive this identity after a search result.
func BrowserCatalogDescriptor(registry externalmcp.Registry) CatalogDescriptor {
	return CatalogDescriptor{
		ProviderKey: "browser-catalog-registry-" + registry.ID.String(),
		Registry:    registry,
		SetupIntent: "dashboard_source_settings",
	}
}

func isBrowserCatalogProviderKey(providerKey string) bool {
	_, id, found := strings.Cut(providerKey, "browser-catalog-registry-")
	if !found || id == "" {
		return false
	}
	_, err := uuid.Parse(id)
	return err == nil
}

// NewRegistryCatalogSources composes only server-owned registry sources. The
// opaque ProviderKey remains unique across every source, preventing a selected
// entry from being reinterpreted against a different registry.
func NewRegistryCatalogSources(sources []RegistryCatalogSource) *RegistryCatalog {
	byKey := make(map[string]registryCatalogDescriptor)
	for _, source := range sources {
		if source.Client == nil {
			continue
		}
		for _, descriptor := range source.Descriptors {
			if descriptor.ProviderKey == "" || descriptor.Registry.ID == uuid.Nil || !isHTTPSURL(descriptor.Registry.URL) || descriptor.SetupIntent == "" {
				continue
			}
			if (descriptor.CanonicalRef == "") != (descriptor.AllowedRemoteURL == "") {
				continue
			}
			if descriptor.AllowedRemoteURL != "" && !validCatalogRemoteTemplate(descriptor.AllowedRemoteURL) {
				continue
			}
			// Refuse ambiguous source identities. A duplicate registry/source must
			// be fixed in server composition rather than silently routing a
			// selection to a different catalogue source.
			if _, exists := byKey[descriptor.ProviderKey]; exists {
				continue
			}
			byKey[descriptor.ProviderKey] = registryCatalogDescriptor{CatalogDescriptor: descriptor, client: source.Client}
		}
	}
	return &RegistryCatalog{descriptors: byKey}
}

func (c *RegistryCatalog) Search(ctx context.Context, query string) ([]CatalogCandidate, error) {
	if c == nil {
		return nil, ErrCatalogUnavailable
	}

	candidates := make([]CatalogCandidate, 0, len(c.descriptors))
	var lastErr error
	for _, source := range c.descriptors {
		result, err := source.client.ListServers(ctx, source.Registry, externalmcp.ListServersParams{Search: &query})
		if err != nil {
			// Match the browser catalogue's degraded behavior: one unavailable
			// configured registry does not hide the remaining configured sources.
			lastErr = err
			continue
		}
		for _, entry := range result.Servers {
			if !descriptorAllowsEntry(source.CatalogDescriptor, entry) {
				continue
			}
			candidates = append(candidates, catalogCandidateFromEntry(source.CatalogDescriptor, entry))
		}
	}
	if len(candidates) == 0 && lastErr != nil {
		return nil, fmt.Errorf("list platform mcp catalog: %w", lastErr)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ProviderKey == candidates[j].ProviderKey {
			return candidates[i].CatalogRef < candidates[j].CatalogRef
		}
		return candidates[i].ProviderKey < candidates[j].ProviderKey
	})
	return candidates, nil
}

func (c *RegistryCatalog) Inspect(ctx context.Context, providerKey, catalogRef string) (CatalogDetails, error) {
	if c == nil {
		return CatalogDetails{}, ErrCatalogUnavailable
	}
	source, ok := c.descriptors[providerKey]
	if !ok || catalogRef == "" || (source.CanonicalRef != "" && catalogRef != source.CanonicalRef) {
		return CatalogDetails{}, ErrCatalogRejected
	}
	descriptor := source.CatalogDescriptor

	entries, err := source.client.ListServers(ctx, descriptor.Registry, externalmcp.ListServersParams{Search: &catalogRef})
	if err != nil {
		return CatalogDetails{}, fmt.Errorf("find platform mcp catalog candidate: %w", err)
	}
	var entry *types.ExternalMCPServerEntry
	for _, candidate := range entries.Servers {
		if candidate.RegistrySpecifier == catalogRef && descriptorAllowsEntry(descriptor, candidate) {
			entry = candidate
			break
		}
	}
	if entry == nil {
		return CatalogDetails{}, ErrCatalogRejected
	}

	allowedURLs := streamableHTTPRemoteURLs(entry)
	if descriptor.AllowedRemoteURL != "" {
		allowedURLs = []string{descriptor.AllowedRemoteURL}
	}
	details, err := source.client.GetServerDetails(ctx, descriptor.Registry, catalogRef, allowedURLs)
	if err != nil {
		return CatalogDetails{}, fmt.Errorf("inspect platform mcp catalog candidate: %w", err)
	}
	if details.RemoteURL == "" || !validCatalogRemoteTemplate(details.RemoteURL) || details.TransportType != externalmcptypes.TransportTypeStreamableHTTP || !containsString(allowedURLs, details.RemoteURL) {
		return CatalogDetails{}, ErrCatalogRejected
	}

	toolNames := make([]string, 0, len(details.Tools))
	for _, tool := range details.Tools {
		if tool.Name != "" {
			toolNames = append(toolNames, tool.Name)
		}
	}
	sort.Strings(toolNames)
	configuration := catalogConfiguration(details.Headers, details.Variables)
	return CatalogDetails{
		CatalogCandidate: CatalogCandidate{
			ProviderKey: providerKey,
			CatalogRef:  catalogRef,
			Name:        details.Name,
			Description: details.Description,
			Version:     details.Version,
			ToolCount:   len(toolNames),
			SetupIntent: descriptor.SetupIntent,
		},
		Transport:              string(details.TransportType),
		ToolNames:              toolNames,
		Configuration:          configuration,
		RequiresDashboardSetup: catalogRequiresDashboardSetup(configuration),
		remoteURLTemplate:      details.RemoteURL,
		remoteURL:              details.RemoteURL,
	}, nil
}

func descriptorAllowsEntry(descriptor CatalogDescriptor, entry *types.ExternalMCPServerEntry) bool {
	if entry == nil || (descriptor.CanonicalRef != "" && entry.RegistrySpecifier != descriptor.CanonicalRef) {
		return false
	}
	if descriptor.AllowedRemoteURL != "" {
		return entryHasAllowedStreamableHTTPRemote(entry, descriptor.AllowedRemoteURL)
	}
	return len(streamableHTTPRemoteURLs(entry)) > 0
}

func entryHasAllowedStreamableHTTPRemote(entry *types.ExternalMCPServerEntry, allowedRemoteURL string) bool {
	for _, remote := range entry.Remotes {
		if remote != nil && remote.URL == allowedRemoteURL && validCatalogRemoteTemplate(remote.URL) && strings.EqualFold(remote.TransportType, "streamable-http") {
			return true
		}
	}
	return false
}

func isHTTPSURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.Fragment == ""
}

func streamableHTTPRemoteURLs(entry *types.ExternalMCPServerEntry) []string {
	urls := make([]string, 0, len(entry.Remotes))
	for _, remote := range entry.Remotes {
		if remote != nil && strings.EqualFold(remote.TransportType, "streamable-http") && validCatalogRemoteTemplate(remote.URL) {
			urls = append(urls, remote.URL)
		}
	}
	return urls
}

func validCatalogRemoteTemplate(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.Fragment == ""
}

func hasUnresolvedRemoteTemplate(rawURL string) bool {
	return strings.ContainsAny(rawURL, "{}")
}

func catalogConfiguration(headers []externalmcp.RemoteHeader, variables map[string]externalmcp.RemoteVariable) []CatalogConfigurationField {
	fields := make([]CatalogConfigurationField, 0, len(headers)+len(variables))
	for _, header := range headers {
		if header.Name == "" {
			continue
		}
		fields = append(fields, CatalogConfigurationField{
			Key:         "header:" + strings.ToLower(header.Name),
			Kind:        "header",
			Name:        header.Name,
			Description: stringValue(header.Description),
			Required:    header.IsRequired,
			Secret:      header.IsSecret || sensitiveHeaderName(header.Name),
			Default:     stringValue(header.Placeholder),
		})
	}
	variableNames := make([]string, 0, len(variables))
	for name := range variables {
		variableNames = append(variableNames, name)
	}
	sort.Strings(variableNames)
	for _, name := range variableNames {
		variable := variables[name]
		fields = append(fields, CatalogConfigurationField{
			Key:         "url_variable:" + name,
			Kind:        "url_variable",
			Name:        name,
			Description: stringValue(variable.Description),
			Required:    variable.IsRequired,
			Secret:      variable.IsSecret,
			Default:     stringValue(variable.Default),
			Choices:     append([]string(nil), variable.Choices...),
		})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	return fields
}

func catalogRequiresDashboardSetup(fields []CatalogConfigurationField) bool {
	for _, field := range fields {
		if field.Secret && field.Required {
			return true
		}
	}
	return false
}

func sensitiveHeaderName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key":
		return true
	default:
		return false
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func catalogCandidateFromEntry(descriptor CatalogDescriptor, entry *types.ExternalMCPServerEntry) CatalogCandidate {
	name := entry.RegistrySpecifier
	if entry.Title != nil && *entry.Title != "" {
		name = *entry.Title
	}
	return CatalogCandidate{
		ProviderKey: descriptor.ProviderKey,
		CatalogRef:  entry.RegistrySpecifier,
		Name:        name,
		Description: entry.Description,
		Version:     entry.Version,
		ToolCount:   entry.ToolCount,
		SetupIntent: descriptor.SetupIntent,
	}
}
