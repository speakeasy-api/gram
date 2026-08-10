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

type CatalogDescriptor struct {
	ProviderKey      string
	Registry         externalmcp.Registry
	CanonicalRef     string
	AllowedRemoteURL string
	SetupIntent      string
}

type CatalogCandidate struct {
	ProviderKey string `json:"provider_key"`
	CatalogRef  string `json:"catalog_ref"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	ToolCount   int    `json:"tool_count"`
	SetupIntent string `json:"setup_intent"`
}

type CatalogDetails struct {
	CatalogCandidate
	Transport string   `json:"transport"`
	ToolNames []string `json:"tool_names"`

	// remoteURL is resolved only from an approved descriptor and is deliberately
	// not projected through MCP tool responses. M1 rejects entries that need
	// arbitrary remote headers.
	remoteURL string
}

type Catalog interface {
	Search(ctx context.Context, query string) ([]CatalogCandidate, error)
	Inspect(ctx context.Context, providerKey, catalogRef string) (CatalogDetails, error)
}

type RegistryCatalog struct {
	client      *externalmcp.RegistryClient
	descriptors map[string]CatalogDescriptor
}

func NewRegistryCatalog(client *externalmcp.RegistryClient, descriptors []CatalogDescriptor) *RegistryCatalog {
	byKey := make(map[string]CatalogDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.ProviderKey == "" || descriptor.Registry.ID == uuid.Nil || !isHTTPSURL(descriptor.Registry.URL) || descriptor.CanonicalRef == "" || !isHTTPSURL(descriptor.AllowedRemoteURL) || hasUnresolvedRemoteTemplate(descriptor.AllowedRemoteURL) || descriptor.SetupIntent == "" {
			continue
		}
		byKey[descriptor.ProviderKey] = descriptor
	}
	return &RegistryCatalog{client: client, descriptors: byKey}
}

func (c *RegistryCatalog) Search(ctx context.Context, query string) ([]CatalogCandidate, error) {
	if c == nil || c.client == nil {
		return nil, ErrCatalogUnavailable
	}

	candidates := make([]CatalogCandidate, 0, len(c.descriptors))
	for _, descriptor := range c.descriptors {
		result, err := c.client.ListServers(ctx, descriptor.Registry, externalmcp.ListServersParams{Search: &query})
		if err != nil {
			return nil, fmt.Errorf("list platform mcp catalog: %w", err)
		}
		for _, entry := range result.Servers {
			if entry.RegistrySpecifier != descriptor.CanonicalRef || !entryHasAllowedStreamableHTTPRemote(entry, descriptor.AllowedRemoteURL) {
				continue
			}
			candidates = append(candidates, catalogCandidateFromEntry(descriptor, entry))
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ProviderKey < candidates[j].ProviderKey
	})
	return candidates, nil
}

func (c *RegistryCatalog) Inspect(ctx context.Context, providerKey, catalogRef string) (CatalogDetails, error) {
	if c == nil || c.client == nil {
		return CatalogDetails{}, ErrCatalogUnavailable
	}
	descriptor, ok := c.descriptors[providerKey]
	if !ok || catalogRef != descriptor.CanonicalRef {
		return CatalogDetails{}, ErrCatalogRejected
	}

	details, err := c.client.GetServerDetails(ctx, descriptor.Registry, descriptor.CanonicalRef, []string{descriptor.AllowedRemoteURL})
	if err != nil {
		return CatalogDetails{}, fmt.Errorf("inspect platform mcp catalog candidate: %w", err)
	}
	if details.RemoteURL != descriptor.AllowedRemoteURL || hasUnresolvedRemoteTemplate(details.RemoteURL) || details.TransportType != externalmcptypes.TransportTypeStreamableHTTP || len(details.Headers) != 0 {
		return CatalogDetails{}, ErrCatalogRejected
	}

	toolNames := make([]string, 0, len(details.Tools))
	for _, tool := range details.Tools {
		if tool.Name != "" {
			toolNames = append(toolNames, tool.Name)
		}
	}
	sort.Strings(toolNames)
	return CatalogDetails{
		CatalogCandidate: CatalogCandidate{
			ProviderKey: providerKey,
			CatalogRef:  descriptor.CanonicalRef,
			Name:        details.Name,
			Description: details.Description,
			Version:     details.Version,
			ToolCount:   len(toolNames),
			SetupIntent: descriptor.SetupIntent,
		},
		Transport: string(details.TransportType),
		ToolNames: toolNames,
		remoteURL: details.RemoteURL,
	}, nil
}

func entryHasAllowedStreamableHTTPRemote(entry *types.ExternalMCPServerEntry, allowedRemoteURL string) bool {
	if !isHTTPSURL(allowedRemoteURL) || hasUnresolvedRemoteTemplate(allowedRemoteURL) {
		return false
	}
	for _, remote := range entry.Remotes {
		if remote != nil && remote.URL == allowedRemoteURL && isHTTPSURL(remote.URL) && !hasUnresolvedRemoteTemplate(remote.URL) && strings.EqualFold(remote.TransportType, "streamable-http") && len(remote.Headers) == 0 {
			return true
		}
	}
	return false
}

func isHTTPSURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.Fragment == ""
}

func hasUnresolvedRemoteTemplate(rawURL string) bool {
	return strings.ContainsAny(rawURL, "{}")
}

func catalogCandidateFromEntry(descriptor CatalogDescriptor, entry *types.ExternalMCPServerEntry) CatalogCandidate {
	name := entry.RegistrySpecifier
	if entry.Title != nil && *entry.Title != "" {
		name = *entry.Title
	}
	return CatalogCandidate{
		ProviderKey: descriptor.ProviderKey,
		CatalogRef:  descriptor.CanonicalRef,
		Name:        name,
		Description: entry.Description,
		Version:     entry.Version,
		ToolCount:   entry.ToolCount,
		SetupIntent: descriptor.SetupIntent,
	}
}
