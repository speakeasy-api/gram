import { Type } from "@/components/ui/type";
import type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";
import { Building2, ChevronRight, FolderOpen, Plug } from "lucide-react";
import { Link } from "react-router";

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
      <Type variant="body" className="text-muted-foreground text-sm">
        —
      </Type>
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
    // Grants store the toolset id for toolset-backed servers and the
    // mcp_servers row id for remote/tunneled ones, so try both maps.
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
      <span className="truncate">{label}</span>
    </span>
  );
}
