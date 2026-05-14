package catalog

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/google/uuid"
	registriesgen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	remotemcpgen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const handlerInstall = "install_catalog_server"

type installInput struct {
	RegistryID        string            `json:"registry_id" jsonschema:"ID of the registry returned by platform_search_catalog."`
	RegistrySpecifier string            `json:"registry_specifier" jsonschema:"The server's registry_specifier returned by platform_search_catalog (e.g. 'io.github.user/server')."`
	Name              *string           `json:"name,omitempty" jsonschema:"Optional human-readable name for the resulting remote MCP server. Defaults to the catalog title or specifier tail."`
	RemoteURL         *string           `json:"remote_url,omitempty" jsonschema:"Optional exact URL of the remotes[] entry to install. Omit to let the tool pick: streamable-http first, then sse."`
	TransportType     *string           `json:"transport_type,omitempty" jsonschema:"Optional transport filter applied when remote_url is not set. One of 'streamable-http' or 'sse'."`
	Variables         map[string]string `json:"variables,omitempty" jsonschema:"Values for URL template variables declared by the selected remote. Required variables without a default must be supplied here."`
	Headers           map[string]string `json:"headers,omitempty" jsonschema:"Static values for the headers declared by the selected remote (keyed by header name). Required headers must be supplied here. Secret values are stored encrypted."`
}

type installResult struct {
	ServerID      string `json:"server_id"`
	ServerSlug    string `json:"server_slug,omitempty"`
	Name          string `json:"name,omitempty"`
	URL           string `json:"url"`
	TransportType string `json:"transport_type"`
}

// InstallTool registers a catalog server as a remote MCP server. It resolves
// the upstream URL, transport, and required headers/variables from the
// catalog entry and forwards a single remoteMcp.createServer call.
type InstallTool struct {
	descriptor core.ToolDescriptor
	catalog    Catalog
}

func NewInstallTool(catalog Catalog) *InstallTool {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := false

	return &InstallTool{
		catalog: catalog,
		descriptor: core.ToolDescriptor{
			SourceSlug:  SourceCatalog,
			HandlerName: handlerInstall,
			Name:        ToolNameInstallCatalogTool,
			Description: "Register an MCP catalog server as a remote MCP server in the caller's project. Resolves the upstream URL, transport, and required user inputs (URL variables, headers) from the catalog entry. Use platform_search_catalog first to discover the registry_id and registry_specifier.",
			InputSchema: core.BuildInputSchema[installInput](
				core.WithPropertyFormat("registry_id", "uuid"),
			),
			Variables:   nil,
			Annotations: catalogToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
	}
}

func (t *InstallTool) Descriptor() core.ToolDescriptor {
	return t.descriptor
}

func (t *InstallTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.catalog == nil {
		return oops.E(oops.CodeUnexpected, nil, "catalog tools are not configured")
	}

	if _, ok := contextvalues.GetAssistantPrincipal(ctx); !ok {
		return oops.E(oops.CodeUnauthorized, nil, "catalog tools require an assistant principal")
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "catalog tools require a project auth context")
	}

	var input installInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	registryID, err := uuid.Parse(strings.TrimSpace(input.RegistryID))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid registry_id")
	}
	specifier := strings.TrimSpace(input.RegistrySpecifier)
	if specifier == "" {
		return oops.E(oops.CodeBadRequest, nil, "registry_specifier is required")
	}

	details, err := t.catalog.GetServerDetails(ctx, &registriesgen.GetServerDetailsPayload{
		RegistryID:       registryID.String(),
		ServerSpecifier:  specifier,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return fmt.Errorf("fetch catalog server details: %w", err)
	}
	if details == nil {
		return oops.E(oops.CodeUnexpected, nil, "catalog server details returned no server")
	}

	remote, err := selectRemote(details.Remotes, input.RemoteURL, input.TransportType)
	if err != nil {
		return err
	}

	resolvedURL, err := resolveRemoteURL(remote.URL, remote.Variables, input.Variables)
	if err != nil {
		return err
	}

	headerInputs, err := buildHeaderInputs(remote.Headers, input.Headers)
	if err != nil {
		return err
	}

	displayName := strings.TrimSpace(conv.PtrValOrEmpty(input.Name, ""))
	if displayName == "" {
		displayName = defaultDisplayName(specifier, conv.PtrValOrEmpty(details.Title, ""))
	}

	created, err := t.catalog.CreateRemoteServer(ctx, &remotemcpgen.CreateServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             conv.PtrEmpty(displayName),
		URL:              resolvedURL,
		TransportType:    remote.TransportType,
		Headers:          headerInputs,
	})
	if err != nil {
		return fmt.Errorf("create remote mcp server: %w", err)
	}
	if created == nil {
		return oops.E(oops.CodeUnexpected, nil, "create remote mcp server returned no server")
	}

	return writeJSON(wr, installResult{
		ServerID:      created.ID,
		ServerSlug:    conv.PtrValOrEmpty(created.Slug, ""),
		Name:          conv.PtrValOrEmpty(created.Name, ""),
		URL:           created.URL,
		TransportType: created.TransportType,
	})
}

// selectRemote picks one entry from remotes following: explicit remote_url
// match, then explicit transport_type filter (first match), then implicit
// streamable-http preference, then sse fallback.
func selectRemote(remotes []*types.ExternalMCPRemote, remoteURL *string, transportType *string) (*types.ExternalMCPRemote, error) {
	if len(remotes) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "catalog server exposes no remotes")
	}

	if remoteURL != nil && strings.TrimSpace(*remoteURL) != "" {
		want := strings.TrimSpace(*remoteURL)
		for _, r := range remotes {
			if r != nil && r.URL == want {
				return r, nil
			}
		}
		return nil, oops.E(oops.CodeBadRequest, nil, "no remote with url %q on this catalog server", want)
	}

	if transportType != nil && strings.TrimSpace(*transportType) != "" {
		want := strings.TrimSpace(*transportType)
		for _, r := range remotes {
			if r != nil && r.TransportType == want && r.URL != "" {
				return r, nil
			}
		}
		return nil, oops.E(oops.CodeBadRequest, nil, "no %s remote on this catalog server", want)
	}

	for _, transport := range []string{"streamable-http", "sse"} {
		for _, r := range remotes {
			if r != nil && r.TransportType == transport && r.URL != "" {
				return r, nil
			}
		}
	}
	return nil, oops.E(oops.CodeBadRequest, nil, "catalog server has no streamable-http or sse remote")
}

// resolveRemoteURL substitutes `{name}` placeholders in rawURL using the
// supplied variable values (falling back to declared defaults). Returns an
// error when a required variable has no value, when a supplied value is not
// in the variable's choices, or when the URL still contains an unresolved
// placeholder after substitution.
func resolveRemoteURL(rawURL string, declared map[string]*types.ExternalMCPRemoteVariable, supplied map[string]string) (string, error) {
	resolved := rawURL
	for name, variable := range declared {
		// Secret URL variables can't be safely installed via this tool: the
		// substituted value would be persisted in the plaintext url column
		// and surfaced in the install response. Catalog authors should
		// declare secrets as headers, not URL placeholders.
		if variable != nil && variable.IsSecret != nil && *variable.IsSecret {
			return "", oops.E(oops.CodeBadRequest, nil, "url variable %q is declared as secret; secrets in the URL path are not supported — ask the catalog author to declare it as a header instead", name)
		}
		value, ok := supplied[name]
		if !ok || value == "" {
			if variable != nil && variable.Default != nil {
				value = *variable.Default
			}
		}
		if value == "" {
			required := variable != nil && variable.IsRequired != nil && *variable.IsRequired
			if required {
				return "", oops.E(oops.CodeBadRequest, nil, "missing required url variable %q", name)
			}
			continue
		}
		if variable != nil && len(variable.Choices) > 0 {
			allowed := slices.Contains(variable.Choices, value)
			if !allowed {
				return "", oops.E(oops.CodeBadRequest, nil, "url variable %q value %q is not one of the declared choices", name, value)
			}
		}
		resolved = strings.ReplaceAll(resolved, "{"+name+"}", value)
	}

	if strings.Contains(resolved, "{") && strings.Contains(resolved, "}") {
		return "", oops.E(oops.CodeBadRequest, nil, "remote url still contains unresolved placeholders after variable substitution: %s", resolved)
	}

	return resolved, nil
}

// buildHeaderInputs maps the catalog-declared headers to remoteMcp header
// inputs using the supplied static values. Required headers without a value
// fail the call. is_secret/is_required are preserved from the catalog.
func buildHeaderInputs(declared []*types.ExternalMCPRemoteHeader, supplied map[string]string) ([]*remotemcpgen.HeaderInput, error) {
	if len(declared) == 0 {
		return nil, nil
	}

	out := make([]*remotemcpgen.HeaderInput, 0, len(declared))
	for _, header := range declared {
		if header == nil || header.Name == "" {
			continue
		}
		value := supplied[header.Name]
		required := header.IsRequired != nil && *header.IsRequired
		if value == "" {
			if required {
				return nil, oops.E(oops.CodeBadRequest, nil, "missing required header %q", header.Name)
			}
			continue
		}
		isSecret := false
		if header.IsSecret != nil {
			isSecret = *header.IsSecret
		}
		out = append(out, &remotemcpgen.HeaderInput{
			Name:                   header.Name,
			Description:            header.Description,
			IsRequired:             new(required),
			IsSecret:               new(isSecret),
			Value:                  new(value),
			ValueFromRequestHeader: nil,
		})
	}
	return out, nil
}

func defaultDisplayName(specifier string, fallback string) string {
	specifier = strings.TrimSpace(specifier)
	if specifier == "" {
		return strings.TrimSpace(fallback)
	}
	if idx := strings.LastIndex(specifier, "/"); idx >= 0 && idx < len(specifier)-1 {
		return specifier[idx+1:]
	}
	return specifier
}
