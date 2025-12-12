package externalmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// RegistryClient handles communication with external MCP registries.
type RegistryClient struct {
	httpClient *http.Client
	logger     *slog.Logger
}

// NewRegistryClient creates a new registry client.
func NewRegistryClient(logger *slog.Logger) *RegistryClient {
	return &RegistryClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
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

// checkForPulseCredentials returns headers for Pulse MCP registry authentication.
// TODO: Add registry_organizations and registry_environments tables to have a better
// system for managing registry credentials.
func checkForPulseCredentials(registryURL string) http.Header {
	headers := make(http.Header)

	parsed, err := url.Parse(registryURL)
	if err != nil {
		return headers
	}

	if parsed.Scheme != "https" || parsed.Host != "api.pulsemcp.com" {
		return headers
	}

	if tenantID := os.Getenv("PULSE_REGISTRY_TENANT"); tenantID != "" {
		headers.Set("X-Tenant-ID", tenantID)
	}
	if apiKey := os.Getenv("PULSE_REGISTRY_KEY"); apiKey != "" {
		headers.Set("X-Api-Key", apiKey)
	}

	return headers
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

	c.logger.InfoContext(ctx, "fetching servers from registry", attr.SlogMCPRegistryURL(reqURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for key, values := range checkForPulseCredentials(registry.URL) {
		for _, value := range values {
			req.Header.Set(key, value)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from registry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var listResp listResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("failed to decode registry response: %w", err)
	}

	registryID := registry.ID.String()
	servers := make([]*types.ExternalMCPServer, 0, len(listResp.Servers))
	for _, s := range listResp.Servers {
		var iconURL *string
		if len(s.Server.Icons) > 0 {
			iconURL = &s.Server.Icons[0].URL
		}

		server := &types.ExternalMCPServer{
			Name:        s.Server.Name,
			Version:     s.Server.Version,
			Description: s.Server.Description,
			RegistryID:  registryID,
			Title:       s.Server.Title,
			IconURL:     iconURL,
		}

		servers = append(servers, server)
	}

	return servers, nil
}
