package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type initializeResult struct {
	ProtocolVersion string                     `json:"protocolVersion"`
	Capabilities    map[string]json.RawMessage `json:"capabilities"`
	ServerInfo      serverInfo                 `json:"serverInfo"`
	Instructions    string                     `json:"instructions,omitempty"`
}
type serverInfo struct {
	Name       string `json:"name"`
	Title      string `json:"title,omitempty"`
	Version    string `json:"version"`
	WebsiteURL string `json:"websiteUrl,omitempty"`
	Icons      []icon `json:"icons,omitempty"`
}

// icon is an SEP-973 server icon advertised in serverInfo. Src must be an
// absolute URL reachable by the MCP client without auth.
type icon struct {
	Src      string   `json:"src"`
	MimeType string   `json:"mimeType,omitempty"`
	Sizes    []string `json:"sizes,omitempty"`
}

// initializeParams captures the subset of the MCP initialize request params we
// record: the requested protocol version, the client identity, and the
// capabilities the client advertised.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
	Capabilities map[string]json.RawMessage `json:"capabilities"`
}

// parseInitializeParams decodes the initialize request params we record. It
// returns the parsed params, the sorted set of advertised capability keys, and
// whether the params were well-formed. Malformed params must not fail the RPC,
// so callers log and continue rather than surfacing the error.
func parseInitializeParams(raw json.RawMessage) (initializeParams, []string, error) {
	var params initializeParams
	if len(raw) == 0 {
		return params, nil, nil
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return initializeParams{}, nil, fmt.Errorf("unmarshal initialize params: %w", err)
	}
	return params, slices.Sorted(maps.Keys(params.Capabilities)), nil
}

func handleInitialize(ctx context.Context, logger *slog.Logger, req *rawRequest, payload *mcpInputs, productMetrics *posthog.Posthog, toolsetsRepoParam *toolsets_repo.Queries, metadataRepoParam *metadata_repo.Queries, serverURL *url.URL) (json.RawMessage, error) {
	params, capabilities, err := parseInitializeParams(req.Params)
	validParams := err == nil
	if err != nil {
		// Malformed params should not fail the RPC; we just lose the
		// recorded client info for this request.
		logger.WarnContext(ctx, "failed to parse mcp initialize params", attr.SlogError(err))
	}

	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		if err := productMetrics.CaptureEvent(ctx, "mcp_initialized", payload.sessionID, map[string]any{
			"project_id":           payload.projectID.String(),
			"authenticated":        payload.authenticated,
			"mcp_domain":           requestContext.Host,
			"mcp_url":              requestContext.Host + requestContext.ReqURL,
			"disable_notification": true,
			"mcp_session_id":       payload.sessionID,
			"protocol_version":     params.ProtocolVersion,
			"client_name":          conv.PtrEmpty(conv.TruncateString(params.ClientInfo.Name, 100)),
			"client_version":       conv.PtrEmpty(conv.TruncateString(params.ClientInfo.Version, 100)),
			"capabilities":         conv.Ternary(validParams, capabilities, nil),
		}); err != nil {
			logger.ErrorContext(ctx, "failed to capture mcp_initialized event", attr.SlogError(err))
		}
	}

	identity := fetchServerIdentity(ctx, logger, toolsetsRepoParam, metadataRepoParam, payload.toolset, payload.projectID, serverURL)

	result := &result[initializeResult]{
		ID: req.ID,
		Result: initializeResult{
			ProtocolVersion: "2025-03-26",
			Capabilities: map[string]json.RawMessage{
				"tools":     json.RawMessage("{}"),
				"prompts":   json.RawMessage("{}"),
				"resources": json.RawMessage("{}"),
			},
			ServerInfo: serverInfo{
				Name:       identity.name,
				Title:      identity.title,
				Version:    "0.0.0",
				WebsiteURL: identity.websiteURL,
				Icons:      identity.icons,
			},
			Instructions: identity.instructions,
		},
	}

	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize initialize response").LogError(ctx, logger)
	}

	return bs, nil
}

// serverIdentity is what an MCP server reports about itself in the
// initialize response. Fields are zero-valued (and omitted from the wire
// format) when the toolset or its metadata cannot be resolved.
type serverIdentity struct {
	name         string
	title        string
	websiteURL   string
	icons        []icon
	instructions string
}

// fetchServerIdentity resolves the toolset-flavored serverInfo fields:
// name/title from the toolset, and instructions, website URL, and logo icon
// from its MCP metadata. Lookups are best-effort — a bare "Gram" identity is
// returned rather than failing the initialize RPC.
func fetchServerIdentity(ctx context.Context, logger *slog.Logger, toolsetsRepo *toolsets_repo.Queries, metadataRepo *metadata_repo.Queries, toolsetSlug string, projectID uuid.UUID, serverURL *url.URL) serverIdentity {
	identity := serverIdentity{
		name:         "Gram",
		title:        "",
		websiteURL:   "",
		icons:        nil,
		instructions: "",
	}

	toolset, err := toolsetsRepo.GetToolset(ctx, toolsets_repo.GetToolsetParams{
		Slug:      toolsetSlug,
		ProjectID: projectID,
	})
	if err != nil {
		// not finding a toolset is OK - any other errors are unexpected and should be logged
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.WarnContext(ctx, "failed to fetch toolset for server identity", attr.SlogError(err))
		}
		return identity
	}

	identity.name = toolset.Slug
	identity.title = toolset.Name

	metadata, err := metadataRepo.GetMetadataForToolset(ctx, uuid.NullUUID{UUID: toolset.ID, Valid: true})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.WarnContext(ctx, "failed to fetch MCP metadata for server identity", attr.SlogError(err))
		}
		return identity
	}

	if metadata.Instructions.Valid {
		identity.instructions = metadata.Instructions.String
	}
	if metadata.ExternalDocumentationUrl.Valid {
		identity.websiteURL = strings.TrimSpace(metadata.ExternalDocumentationUrl.String)
	}
	if metadata.LogoID.Valid && serverURL != nil {
		identity.icons = []icon{{Src: assets.ServeImageURL(serverURL, metadata.LogoID.UUID), MimeType: "", Sizes: nil}}
	}

	return identity
}
