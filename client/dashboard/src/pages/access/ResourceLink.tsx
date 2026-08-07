import { Text } from "@/components/ui/Text";
import type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";
import { Building2, ChevronRight, FolderOpen, Plug } from "lucide-react";
import { Link } from "react-router";

// Fallback for resource ids we cannot resolve to a display name (deleted
// rows, kinds with no org-level lookup): a truncated mono chip with the full
// id on hover, instead of a raw UUID in running text.
function IdChip({ id }: { id: string }): JSX.Element {
  return (
    <code
      className="bg-muted text-muted-foreground shrink-0 px-1.5 py-0.5 font-mono text-xs"
      title={id}
    >
      {id.length > 12 ? `${id.slice(0, 8)}…` : id}
    </code>
  );
}

export function ResourceLink({
  challenge,
  orgSlug,
  projectMap,
  toolsetMap,
  mcpServerMap,
}: {
  challenge: ChallengeBucket;
  orgSlug: string;
  projectMap: Map<string, { slug: string; name: string }>;
  toolsetMap: Map<string, { slug: string; name: string; projectId: string }>;
  mcpServerMap: Map<
    string,
    { slug?: string; name?: string; projectId: string }
  >;
}): JSX.Element {
  const { resourceKind, resourceId } = challenge;

  if (!resourceKind || !resourceId) {
    return (
      <Text variant="body" className="text-muted-foreground text-sm">
        —
      </Text>
    );
  }

  let to: string | null = null;
  let label = resourceId;
  let IconEl: typeof Building2 | null = null;

  if (resourceKind === "org") {
    label = "Organization";
    IconEl = Building2;
    to = `/${orgSlug}`;
  } else if (resourceKind === "project") {
    const proj = projectMap.get(resourceId);
    label = proj?.name ?? resourceId;
    IconEl = FolderOpen;
    to = proj ? `/${orgSlug}/projects/${proj.slug}` : null;
  } else if (resourceKind === "mcp") {
    IconEl = Plug;
    // Grants use resource_kind "mcp" for both server flavors: the resource id
    // is the toolset id for toolset-backed servers and the mcp_servers row id
    // for remote/tunneled ones, so try both maps.
    const toolset = toolsetMap.get(resourceId);
    const mcpServer = toolset ? undefined : mcpServerMap.get(resourceId);
    if (toolset) {
      label = toolset.name;
      const proj = projectMap.get(toolset.projectId);
      to = proj
        ? `/${orgSlug}/projects/${proj.slug}/mcp/${toolset.slug}`
        : null;
    } else if (mcpServer) {
      label = mcpServer.name ?? mcpServer.slug ?? resourceId;
      const proj = projectMap.get(mcpServer.projectId);
      to =
        proj && mcpServer.slug
          ? `/${orgSlug}/projects/${proj.slug}/mcp/x/${mcpServer.slug}`
          : null;
    } else {
      label = resourceId;
    }
  }

  if (to) {
    return (
      <Link
        to={to}
        className="text-primary hover:text-primary/80 inline-flex items-center gap-1.5 truncate text-sm underline underline-offset-4"
      >
        {IconEl && <IconEl className="h-3.5 w-3.5 shrink-0 opacity-60" />}
        <span className="truncate">{label}</span>
        <ChevronRight className="h-3 w-3 shrink-0" />
      </Link>
    );
  }

  return (
    <span className="text-muted-foreground inline-flex items-center gap-1.5 truncate text-sm">
      {IconEl && <IconEl className="h-3.5 w-3.5 shrink-0" />}
      {label === resourceId ? (
        <IdChip id={resourceId} />
      ) : (
        <span className="truncate">{label}</span>
      )}
    </span>
  );
}
