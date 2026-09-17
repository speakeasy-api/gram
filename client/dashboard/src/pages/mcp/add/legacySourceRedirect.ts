import { attachmentToURNPrefix, mcpServerRouteParam } from "@/lib/sources";
import type { Deployment } from "@gram/client/models/components/deployment.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import type { UnproxiedMcpServer } from "@gram/client/models/components/unproxiedmcpserver.js";

// The `:sourceKind` segment of the retired /sources/:sourceKind/:sourceSlug
// route. "http" was accepted alongside "openapi" there, so it is here too.
export type LegacySourceKind =
  | "openapi"
  | "http"
  | "function"
  | "externalmcp"
  | "remotemcp"
  | "tunneledmcp"
  | "unproxiedmcp";

const LEGACY_SOURCE_KINDS: readonly string[] = [
  "openapi",
  "http",
  "function",
  "externalmcp",
  "remotemcp",
  "tunneledmcp",
  "unproxiedmcp",
];

export function parseLegacySourceKind(
  value: string | undefined,
): LegacySourceKind | undefined {
  return value && LEGACY_SOURCE_KINDS.includes(value)
    ? (value as LegacySourceKind)
    : undefined;
}

// Which lookups a kind needs, so the redirect only fetches what it resolves.
export function legacySourceKindLookups(kind: LegacySourceKind | undefined): {
  deployment: boolean;
  mcpServers: boolean;
  toolsets: boolean;
} {
  switch (kind) {
    case "openapi":
    case "http":
    case "function":
      return { deployment: true, mcpServers: false, toolsets: false };
    case "remotemcp":
    case "tunneledmcp":
    case "unproxiedmcp":
      return { deployment: false, mcpServers: true, toolsets: false };
    case "externalmcp":
      return { deployment: false, mcpServers: false, toolsets: true };
    case undefined:
      return { deployment: false, mcpServers: false, toolsets: false };
  }
}

export type LegacySourceLookup = {
  deployment: Deployment | undefined;
  mcpServers: McpServer[];
  remoteMcpServers: RemoteMcpServer[];
  tunneledMcpServers: TunneledMcpServer[];
  unproxiedMcpServers: UnproxiedMcpServer[];
  toolsets: ToolsetEntry[];
};

export type LegacySourceTarget =
  /** /mcp/sources/:sourceId, addressed by the deployment asset's id. */
  | { kind: "source"; assetId: string }
  /** /mcp/x/:mcpServerSlug, the server fronting a remote/tunneled/unproxied source. */
  | { kind: "mcp-server"; routeParam: string }
  /** /mcp/:toolsetSlug, the toolset carrying an external MCP's tools. */
  | { kind: "toolset"; slug: string }
  | { kind: "sources-list" }
  | { kind: "mcp-list" };

// The old route params preferred a slug and fell back to the id, so both
// spellings still have to resolve.
function matchesSlugOrId(
  entity: { id: string; slug?: string | undefined },
  value: string,
): boolean {
  return entity.slug === value || entity.id === value;
}

function mcpServerTarget(server: McpServer | undefined): LegacySourceTarget {
  if (!server) return { kind: "mcp-list" };
  return { kind: "mcp-server", routeParam: mcpServerRouteParam(server) };
}

export function resolveLegacySourceRedirect(
  kind: LegacySourceKind | undefined,
  sourceSlug: string,
  lookup: LegacySourceLookup,
): LegacySourceTarget {
  switch (kind) {
    case "openapi":
    case "http": {
      const asset = lookup.deployment?.openapiv3Assets.find(
        (candidate) => candidate.slug === sourceSlug,
      );
      return asset
        ? { kind: "source", assetId: asset.id }
        : { kind: "sources-list" };
    }
    case "function": {
      const asset = lookup.deployment?.functionsAssets?.find(
        (candidate) => candidate.slug === sourceSlug,
      );
      return asset
        ? { kind: "source", assetId: asset.id }
        : { kind: "sources-list" };
    }
    case "remotemcp": {
      const source = lookup.remoteMcpServers.find((candidate) =>
        matchesSlugOrId(candidate, sourceSlug),
      );
      return mcpServerTarget(
        source &&
          lookup.mcpServers.find(
            (server) => server.remoteMcpServerId === source.id,
          ),
      );
    }
    case "tunneledmcp": {
      // Tunneled sources never had a slug; the old route carried the id.
      const source = lookup.tunneledMcpServers.find(
        (candidate) => candidate.id === sourceSlug,
      );
      return mcpServerTarget(
        source &&
          lookup.mcpServers.find(
            (server) => server.tunneledMcpServerId === source.id,
          ),
      );
    }
    case "unproxiedmcp": {
      const source = lookup.unproxiedMcpServers.find((candidate) =>
        matchesSlugOrId(candidate, sourceSlug),
      );
      return mcpServerTarget(
        source &&
          lookup.mcpServers.find(
            (server) => server.unproxiedMcpServerId === source.id,
          ),
      );
    }
    case "externalmcp": {
      // A catalog-imported source contributes one URN per registry tool
      // (`tools:externalmcp:<slug>:<toolName>`); only the no-tools fallback
      // uses `:proxy`. The source-scoped prefix matches both shapes.
      const prefix = attachmentToURNPrefix("externalmcp", sourceSlug);
      const toolsets = lookup.toolsets.filter((candidate) =>
        candidate.toolUrns.some((urn) => urn.startsWith(prefix)),
      );
      // Several servers can carry the same external MCP; picking one would
      // land on an arbitrary server, so the list is the honest target.
      const [toolset] = toolsets;
      return toolset && toolsets.length === 1
        ? { kind: "toolset", slug: toolset.slug }
        : { kind: "mcp-list" };
    }
    case undefined:
      return { kind: "sources-list" };
  }
}
