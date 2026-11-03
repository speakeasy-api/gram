package externalmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
)

// nativeRegistrySource keeps the adapter on the in-process discovery and retained
// lookup paths. Discover exposes published records; GetByName also retains
// unpublished records needed by existing installations.
type nativeRegistrySource interface {
	Discover(context.Context, mcpregistry.DiscoveryOptions) (mcpregistry.DiscoveryPage, error)
	GetByName(context.Context, string) (mcpregistry.Entry, error)
}

type NativeRegistryReader struct{ source nativeRegistrySource }

var _ RegistryReader = (*NativeRegistryReader)(nil)
var _ nativeRegistrySource = (*mcpregistry.Service)(nil)

// NewNativeRegistryReader reads the local catalog without HTTP. The Registry
// supplied to each read owns the namespace; entry IDs are never registry IDs.
func NewNativeRegistryReader(source nativeRegistrySource) *NativeRegistryReader {
	return &NativeRegistryReader{source: source}
}

func (r *NativeRegistryReader) ListServers(ctx context.Context, registry Registry, params ListServersParams) (ListServersResult, error) {
	var result ListServersResult
	opts := mcpregistry.DiscoveryOptions{Search: "", Version: "", IncludeDeleted: false, UpdatedSince: nil, Cursor: "", Limit: 100}
	// Apply the same name/title/description search as the existing reader after
	// conversion; the discovery service's search is narrower.
	for {
		page, err := r.source.Discover(ctx, opts)
		if err != nil {
			return ListServersResult{}, fmt.Errorf("discover native registry: %w", err)
		}
		for _, raw := range page.Records {
			var entry serverEntry
			if err := json.Unmarshal(raw, &entry); err != nil {
				return ListServersResult{}, fmt.Errorf("decode native registry record: %w", err)
			}
			server, err := projectListServer(registry.ID, entry)
			if err != nil {
				return ListServersResult{}, err
			}
			// Preserve extension metadata that the legacy catalog projection cannot
			// represent, including official lifecycle information.
			var full struct {
				Meta map[string]any `json:"_meta"`
			}
			if err := json.Unmarshal(raw, &full); err != nil {
				return ListServersResult{}, fmt.Errorf("decode native registry metadata: %w", err)
			}
			// Keep arbitrary extensions and auth metadata, but not list tool payloads.
			if version, ok := full.Meta["com.pulsemcp/server-version"].(map[string]any); ok {
				for key, value := range version {
					if !strings.HasPrefix(key, "remotes[") || !strings.HasSuffix(key, "]") {
						continue
					}
					if remote, ok := value.(map[string]any); ok {
						delete(remote, "tools")
					}
				}
			}
			server.Meta = full.Meta
			result.Servers = append(result.Servers, server)
		}
		if page.NextCursor == "" {
			break
		}
		opts.Cursor = page.NextCursor
	}
	if params.Search != nil && *params.Search != "" {
		result.Servers = filterServers(result.Servers, *params.Search)
	}
	return result, nil
}

func (r *NativeRegistryReader) GetServerDetails(ctx context.Context, _ Registry, name string, allowedRemoteURLs []string) (*ServerDetails, error) {
	entry, err := r.source.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("lookup native registry server: %w", err)
	}
	var record serverDetailsEntry
	if err := json.Unmarshal(entry.Data, &record); err != nil {
		return nil, fmt.Errorf("decode native registry details: %w", err)
	}
	return projectServerDetails(record, allowedRemoteURLs), nil
}

// ListEvidenceServers is an internal approval-only read, never public discovery.
// The caller first resolves the research project's organization-selected source.
// Retained eligible records may inform URL evidence without being installable.
func (r *NativeRegistryReader) ListEvidenceServers(ctx context.Context, registry Registry) (ListServersResult, error) {
	source, ok := r.source.(interface {
		List(context.Context, mcpregistry.ListOptions) (mcpregistry.Page, error)
	})
	if !ok {
		return ListServersResult{}, fmt.Errorf("native retained evidence reader unavailable")
	}
	validator, err := mcpregistry.LoadValidator()
	if err != nil {
		return ListServersResult{}, fmt.Errorf("load native registry validator: %w", err)
	}
	var result ListServersResult
	opts := mcpregistry.ListOptions{Query: "", Published: nil, Cursor: "", Limit: 50}
	for {
		page, err := source.List(ctx, opts)
		if err != nil {
			return ListServersResult{}, fmt.Errorf("list retained evidence: %w", err)
		}
		for _, summary := range page.Entries {
			if len(summary.Issues) != 0 {
				continue
			}
			entry, err := r.source.GetByName(ctx, summary.Name)
			if err != nil {
				return ListServersResult{}, fmt.Errorf("read retained evidence: %w", err)
			}
			if len(validator.ValidateStored(entry.Data)) != 0 {
				continue
			}
			var record serverEntry
			if err := json.Unmarshal(entry.Data, &record); err != nil {
				return ListServersResult{}, fmt.Errorf("decode retained record: %w", err)
			}
			var meta struct {
				Meta map[string]any `json:"_meta"`
			}
			if err := json.Unmarshal(entry.Data, &meta); err != nil {
				return ListServersResult{}, fmt.Errorf("decode retained metadata: %w", err)
			}
			if official, ok := meta.Meta["io.modelcontextprotocol.registry/official"].(map[string]any); ok && official["status"] == "deleted" {
				continue
			}
			projected, err := projectListServer(registry.ID, record)
			if err != nil {
				return ListServersResult{}, err
			}
			projected.Meta = meta.Meta
			result.Servers = append(result.Servers, projected)
		}
		if page.NextCursor == "" {
			return result, nil
		}
		opts.Cursor = page.NextCursor
	}
}
