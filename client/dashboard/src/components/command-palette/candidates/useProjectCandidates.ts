import { useSlugs } from "@/contexts/Sdk";
import { useSessionInfo } from "@gram/client/react-query/sessionInfo.js";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

/**
 * The organization's projects as candidates. Projects are the one resource
 * that is organization-scoped rather than project-scoped, so the palette can
 * offer them from either shell: at the org level picking a project is the
 * palette's main job, inside a project it is a switcher.
 */
export function useProjectCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const { orgSlug, projectSlug } = useSlugs();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  // Never throws: a failing source degrades to no candidates rather than
  // blanking the palette.
  const { data } = useSessionInfo(undefined, undefined, {
    enabled,
    refetchOnWindowFocus: false,
    throwOnError: false,
  });

  return useMemo(() => {
    const organizations = data?.result.organizations ?? [];
    const activeOrganizationId = data?.result.activeOrganizationId;
    // The palette opens from either shell, and only the project shell's path
    // carries a slug we could match on — so fall back to the session's active
    // organization, which is the one every other surface renders.
    const organization =
      organizations.find((org) => org.slug === orgSlug) ??
      organizations.find((org) => org.id === activeOrganizationId);
    if (!organization) return [];

    // Slug order, matching the switcher, so a reader scanning the idle list
    // finds a project where the switcher taught them to look.
    const projects = [...organization.projects].sort((a, b) =>
      a.slug.localeCompare(b.slug),
    );

    return projects.map((project): LauncherCandidate => {
      const label = project.name || project.slug;
      // Case-insensitively, as in the switcher: "Default" / "default" is the
      // same name, so the slug would only repeat the label.
      const slugRepeatsLabel =
        label.toLowerCase() === project.slug.toLowerCase();
      return {
        id: `project:${project.id}`,
        kind: "project",
        title: label,
        detail: slugRepeatsLabel ? "Project" : `Project · ${project.slug}`,
        keywords: ["project", project.name, project.slug, project.id],
        verbs: ["open"],
        icon: "folder",
        group: "Projects",
        run: () => {
          // Drop the cache on a switch, the way WorkspaceSwitcher does:
          // project-scoped queries that don't fold the slug into their key
          // would otherwise serve the previous project's data on the page
          // we land on.
          if (project.slug !== projectSlug) queryClient.clear();
          void navigate(`/${organization.slug}/projects/${project.slug}`);
        },
      };
    });
  }, [data, orgSlug, projectSlug, navigate, queryClient]);
}
