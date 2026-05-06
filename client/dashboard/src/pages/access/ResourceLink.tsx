import { Type } from "@/components/ui/type";
import type { AuthzChallenge } from "@gram/client/models/components/authzchallenge.js";
import { Building2, ChevronRight, FolderOpen, Plug } from "lucide-react";
import { Link } from "react-router";

export function ResourceLink({
  challenge,
  orgSlug,
  projectMap,
}: {
  challenge: AuthzChallenge;
  orgSlug: string;
  projectMap: Map<string, { slug: string; name: string }>;
}) {
  const { resourceKind, resourceId, projectId } = challenge;

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
    label = resourceId;
    IconEl = Plug;
    const proj = projectId ? projectMap.get(projectId) : undefined;
    to = proj ? `/${orgSlug}/projects/${proj.slug}/mcp/${resourceId}` : null;
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
