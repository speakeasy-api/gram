//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

const (
	requestMCPNameMaxRunes   = 80
	requestMCPReasonMaxRunes = 280
	requestMCPRefMaxRunes    = 200
	requestMCPResourceMax    = 240
)

var (
	errRequestMCPInvalid     = errors.New("request_mcp input is not valid")
	errRequestMCPUnavailable = errors.New("request_mcp unavailable")
)

type RequestMCPInput struct {
	Name        string `json:"name" jsonschema:"human-readable name of the MCP server to request, at most 80 characters"`
	CatalogRef  string `json:"catalog_ref,omitempty" jsonschema:"optional canonical catalog reference returned by search_mcp_catalog; provide with provider_key, or provide remote_url instead"`
	ProviderKey string `json:"provider_key,omitempty" jsonschema:"optional reviewed provider key returned by search_mcp_catalog; provide with catalog_ref, or provide remote_url instead"`
	RemoteURL   string `json:"remote_url,omitempty" jsonschema:"optional user-supplied HTTPS Streamable HTTP MCP URL; mutually exclusive with provider_key and catalog_ref; credentials, credential-like query parameters, and fragments are not allowed"`
	Reason      string `json:"reason,omitempty" jsonschema:"optional short reason for the request, at most 280 characters; never include credentials"`
}

type RequestMCPOutput struct {
	SentToCount int    `json:"sent_to_count"`
	Message     string `json:"message"`
}

func registerRequestMCPTool(reg *Registrar, requester AccessRequester) {
	addTool(reg, &mcp.Tool{
		Name:        "request_mcp",
		Title:       "Request an MCP Server",
		Description: "Ask an organization administrator to add an MCP server this organization does not have yet. Confirm the server name with the user first. Searching or inspecting a candidate does not send a request.",
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input RequestMCPInput) (*mcp.CallToolResult, RequestMCPOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, RequestMCPOutput{}, err
		}
		normalized, err := normalizeRequestMCPInput(input)
		if err != nil {
			if result, ok := requestMCPToolResult(err); ok {
				return result, RequestMCPOutput{}, nil
			}
			return nil, RequestMCPOutput{}, err
		}
		if requester == nil {
			if result, ok := requestMCPToolResult(errRequestMCPUnavailable); ok {
				return result, RequestMCPOutput{}, nil
			}
			return nil, RequestMCPOutput{}, errRequestMCPUnavailable
		}

		result, err := requester.Notify(ctx, access.NotifyInput{
			OrganizationID: principal.OrganizationID,
			UserID:         principal.UserID,
			Scope:          string(authz.ScopeMCPWrite),
			ResourceID:     "",
			ResourceName:   requestMCPResourceName(normalized),
		})
		if err != nil {
			if result, ok := requestMCPToolResult(err); ok {
				return result, RequestMCPOutput{}, nil
			}
			return nil, RequestMCPOutput{}, err
		}
		return nil, RequestMCPOutput{
			SentToCount: result.SentToCount,
			Message:     "An administrator has been asked to add that MCP server. They will follow up when it is available.",
		}, nil
	})
}

type normalizedRequestMCP struct {
	Name        string
	CatalogRef  string
	ProviderKey string
	RemoteURL   string
	Reason      string
}

func normalizeRequestMCPInput(input RequestMCPInput) (normalizedRequestMCP, error) {
	name := strings.TrimSpace(input.Name)
	if !validRequestMCPName(name) {
		return normalizedRequestMCP{}, errRequestMCPInvalid
	}

	reason := strings.TrimSpace(input.Reason)
	if reason != "" && !validRequestMCPReason(reason) {
		return normalizedRequestMCP{}, errRequestMCPInvalid
	}

	remoteURL := strings.TrimSpace(input.RemoteURL)
	providerKey := normalizeCatalogProviderKey(input.ProviderKey)
	catalogRef := strings.TrimSpace(input.CatalogRef)
	catalogSelection := providerKey != "" || catalogRef != ""
	if remoteURL != "" && catalogSelection {
		return normalizedRequestMCP{}, errRequestMCPInvalid
	}
	if catalogSelection && (providerKey == "" || catalogRef == "") {
		return normalizedRequestMCP{}, errRequestMCPInvalid
	}
	if remoteURL != "" && !validDirectRemoteRegistrationURL(remoteURL) {
		return normalizedRequestMCP{}, errRequestMCPInvalid
	}
	if catalogRef != "" && !validRequestMCPRef(catalogRef) {
		return normalizedRequestMCP{}, errRequestMCPInvalid
	}
	if providerKey != "" && !validRequestMCPRef(providerKey) {
		return normalizedRequestMCP{}, errRequestMCPInvalid
	}

	return normalizedRequestMCP{
		Name:        name,
		CatalogRef:  catalogRef,
		ProviderKey: providerKey,
		RemoteURL:   remoteURL,
		Reason:      reason,
	}, nil
}

func validRequestMCPName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > requestMCPNameMaxRunes {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validRequestMCPReason(reason string) bool {
	if utf8.RuneCountInString(reason) > requestMCPReasonMaxRunes {
		return false
	}
	for _, r := range reason {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func validRequestMCPRef(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > requestMCPRefMaxRunes {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func requestMCPResourceName(in normalizedRequestMCP) string {
	name := in.Name
	switch {
	case in.RemoteURL != "":
		name = in.Name + " (" + in.RemoteURL + ")"
	case in.CatalogRef != "":
		name = in.Name + " (" + in.CatalogRef + ")"
	}
	if in.Reason != "" {
		name = name + " — " + in.Reason
	}
	if utf8.RuneCountInString(name) <= requestMCPResourceMax {
		return name
	}
	runes := []rune(name)
	return string(runes[:requestMCPResourceMax])
}

func requestMCPToolResult(err error) (*mcp.CallToolResult, bool) {
	var result operationBudgetResult
	switch {
	case errors.Is(err, errRequestMCPInvalid):
		result = operationBudgetResult{Code: "invalid_request", Message: "Name the MCP server to request. A remote URL must be HTTPS with no credentials, and catalogue identity must include both provider_key and catalog_ref."}
	case errors.Is(err, errRequestMCPUnavailable), errors.Is(err, access.ErrAccessRequestUnavailable):
		result = operationBudgetResult{Code: unavailableCode, Message: "That request could not be sent just now. Ask an administrator directly, or try again shortly."}
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
