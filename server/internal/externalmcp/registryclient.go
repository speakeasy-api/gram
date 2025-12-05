package externalmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
)

// RegistryClient handles communication with external MCP registries.
type RegistryClient struct {
	httpClient *http.Client
}

// NewRegistryClient creates a new registry client.
func NewRegistryClient() *RegistryClient {
	return &RegistryClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Registry represents an MCP registry endpoint.
type Registry struct {
	ID  uuid.UUID
	URL string
}

// ListServersParams contains optional parameters for listing servers.
type ListServersParams struct {
	Search *string
	Cursor *string
}

// listResponse represents the response from the MCP registry API.
type listResponse struct {
	Servers  []serverEntry `json:"servers"`
	Metadata struct {
		Count      int     `json:"count"`
		NextCursor *string `json:"nextCursor"`
	} `json:"metadata"`
}

type serverEntry struct {
	Server serverJSON   `json:"server"`
	Meta   responseMeta `json:"_meta"`
}

type serverJSON struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Version     string  `json:"version"`
	Title       *string `json:"title"`
	WebsiteURL  *string `json:"websiteUrl"`
	Icons       []struct {
		URL string `json:"url"`
	} `json:"icons"`
}

type responseMeta struct {
	ID string `json:"id"`
}

// ListServers fetches servers from the given registry.
func (c *RegistryClient) ListServers(ctx context.Context, registry Registry, params ListServersParams) ([]*types.ExternalMCPServer, error) {
	reqURL := fmt.Sprintf("%s/v0.1/servers?version=latest&limit=50", registry.URL)
	if params.Search != nil && *params.Search != "" {
		reqURL += fmt.Sprintf("&search=%s", *params.Search)
	}
	if params.Cursor != nil && *params.Cursor != "" {
		reqURL += fmt.Sprintf("&cursor=%s", *params.Cursor)
	}

	slog.InfoContext(ctx, "fetching servers from registry",
		slog.String("url", reqURL),
		slog.String("registry_id", registry.ID.String()),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if tenantID := os.Getenv("PULSE_REGISTRY_TENANT"); tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
		slog.InfoContext(ctx, "set X-Tenant-ID header", slog.String("tenant_id", tenantID))
	}
	if apiKey := os.Getenv("PULSE_REGISTRY_KEY"); apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
		slog.InfoContext(ctx, "set X-Api-Key header", slog.String("api_key_len", fmt.Sprintf("%d", len(apiKey))))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.ErrorContext(ctx, "registry request failed", slog.String("error", err.Error()))
		return nil, fmt.Errorf("failed to fetch from registry: %w", err)
	}
	defer resp.Body.Close()

	slog.InfoContext(ctx, "registry response received",
		slog.Int("status_code", resp.StatusCode),
		slog.String("content_type", resp.Header.Get("Content-Type")),
	)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.ErrorContext(ctx, "registry returned non-OK status",
			slog.Int("status_code", resp.StatusCode),
			slog.String("body", string(body)),
		)
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	slog.InfoContext(ctx, "registry response body",
		slog.Int("body_len", len(body)),
		slog.String("body_preview", string(body[:min(500, len(body))]),
	))

	var listResp listResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		slog.ErrorContext(ctx, "failed to decode registry response",
			slog.String("error", err.Error()),
			slog.String("body", string(body)),
		)
		return nil, fmt.Errorf("failed to decode registry response: %w", err)
	}

	slog.InfoContext(ctx, "parsed registry response",
		slog.Int("server_count", len(listResp.Servers)),
	)

	registryID := registry.ID.String()
	servers := make([]*types.ExternalMCPServer, 0, len(listResp.Servers))
	for _, s := range listResp.Servers {
		server := &types.ExternalMCPServer{
			Name:        s.Server.Name,
			Version:     s.Server.Version,
			Description: s.Server.Description,
			RegistryID:  registryID,
			Title:       s.Server.Title,
		}

		if len(s.Server.Icons) > 0 {
			server.IconURL = &s.Server.Icons[0].URL
		}

		servers = append(servers, server)
	}

	return servers, nil
}
