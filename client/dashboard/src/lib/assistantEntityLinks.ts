import type { LinkResolver } from "@/elements";
import { useCallback } from "react";

import { useProjectSlugForRequests, useSlugs } from "@/contexts/Sdk";
import { resolveEntityLink } from "./assistantEntityLinkResolve";

/**
 * Returns a {@link LinkResolver} that maps the Project Assistant's `gram:`
 * entity references to real dashboard routes, scoped to the current org/project.
 */
export function useAssistantLinkResolver(): LinkResolver {
  const { orgSlug, projectSlug } = useSlugs();
  const requestProjectSlug = useProjectSlugForRequests();
  const effectiveProject = projectSlug ?? requestProjectSlug;

  return useCallback(
    (href: string) => resolveEntityLink(href, orgSlug, effectiveProject),
    [orgSlug, effectiveProject],
  );
}
