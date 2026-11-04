package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	metadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
)

// serverDescription is shared by both protocol eras without any handshake
// recording or session establishment. Cache hints apply only to discovery.
type serverDescription struct {
	// capabilities lists the operations this surface offers.
	capabilities map[string]json.RawMessage

	// identity labels this server in both protocol eras.
	identity serverInfo

	// instructions carries the surface-specific operator guidance.
	instructions string

	// cacheHints protects the complete discovery payload.
	cacheHints *cacheHints
}

func (d serverDescription) initializeResult(version string) initializeResult {
	return initializeResult{
		ProtocolVersion: version,
		Capabilities:    d.capabilities,
		ServerInfo:      d.identity,
		Instructions:    d.instructions,
	}
}

// describePlatformServer keeps discovery caller-varying: the surface answers
// only assistant-token callers, so a shared cache must not serve its
// description to anyone else, and the capabilities depend on the caller's
// organization.
//
// skillsEnabled adds the Skills extension together with the resources
// capability it depends on: the platform toolset has no resources of its own,
// so resources exist on this surface only to carry skill reads.
func describePlatformServer(skillsEnabled bool) serverDescription {
	description := serverDescription{
		capabilities: map[string]json.RawMessage{"tools": json.RawMessage("{}")},
		identity:     serverInfoPlatformToolset,
		instructions: "",
		cacheHints:   cacheHintsCallerVarying,
	}
	if skillsEnabled {
		description.capabilities["resources"] = json.RawMessage("{}")
		description.capabilities[capabilityExtensions] = skillsExtensionCapability
	}
	return description
}

// describeMetaServer describes a gateway. skillsEnabled adds the Skills
// extension; the resources capability it depends on is declared
// unconditionally. The flag is the gateway organization's, so it does not
// change who a description may be shared with.
func describeMetaServer(meta *metamcprepo.MetaMcpServer, mode metamcp.DiscoveryMode, skillsEnabled bool) serverDescription {
	hints := cacheHintsCallerUniform
	if meta.UserSessionIssuerID.Valid {
		hints = cacheHintsCallerVarying
	}
	description := serverDescription{
		capabilities: map[string]json.RawMessage{"tools": json.RawMessage("{}"), "resources": json.RawMessage("{}")},
		identity:     serverInfoMetaServer,
		instructions: metamcp.ResolveDiscoveryInstructions(conv.FromPGText[string](meta.Instructions), mode),
		cacheHints:   hints,
	}
	if skillsEnabled {
		description.capabilities[capabilityExtensions] = skillsExtensionCapability
	}
	return description
}

// capabilityExtensions is the capabilities member under which a server
// declares the extensions it implements, keyed by their labels.
const capabilityExtensions = "extensions"

// skillsExtensionCapability declares the Skills extension (SEP-2640) with no
// optional features: Speakeasy serves single-file skills, so it never declares
// directoryRead. The label is spliced from the method vocabulary so the
// declaration and dispatch cannot disagree on it.
var skillsExtensionCapability = json.RawMessage(`{"` + mcpversions.ExtensionSkills + `":{}}`)

// describeHostedServer tolerates missing metadata exactly as the
// initialize handshake does. It reads the toolset the request builder already loaded; a
// payload without one describes no instructions and keeps hints private.
//
// skillsEnabled adds the Skills extension to the declared capabilities. The
// resources capability it depends on is declared unconditionally, so the
// declaration is one member either way.
func describeHostedServer(ctx context.Context, logger *slog.Logger, metadataRepo *metadata_repo.Queries, payload *mcpInputs, skillsEnabled bool) serverDescription {
	description := serverDescription{
		capabilities: map[string]json.RawMessage{
			"tools":     json.RawMessage("{}"),
			"prompts":   json.RawMessage("{}"),
			"resources": json.RawMessage("{}"),
		},
		identity:     serverInfoHostedToolset,
		instructions: "",
		cacheHints:   cacheHintsCallerVarying,
	}
	if skillsEnabled {
		description.capabilities[capabilityExtensions] = skillsExtensionCapability
	}
	if !payload.toolsetID.Valid {
		return description
	}

	description.cacheHints = hostedListCacheHints(!payload.effectiveMCPPrivate(payload.toolsetIsPublic), payload.authenticated)

	metadata, err := metadataRepo.GetMetadataByBackendID(ctx, metadata_repo.GetMetadataByBackendIDParams{ProjectID: payload.projectID, BackendID: payload.toolsetID.UUID, ToolsetBackend: true})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.WarnContext(ctx, "failed to fetch MCP metadata for instructions", attr.SlogError(err))
		}
		return description
	}

	if !metadata.Instructions.Valid {
		return description
	}

	description.instructions = metadata.Instructions.String
	return description
}
