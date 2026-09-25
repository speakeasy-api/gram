import { useOrganization, useProject } from "@/contexts/Auth";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { useRBAC } from "@/hooks/useRBAC";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";
import { useAccessRequestCandidates } from "./useAccessRequestCandidates";
import { useActionCandidates } from "./useActionCandidates";
import { useAssistantCandidates } from "./useAssistantCandidates";
import { useCatalogCandidates } from "./useCatalogCandidates";
import { useDeploymentCandidates } from "./useDeploymentCandidates";
import { useEnvironmentCandidates } from "./useEnvironmentCandidates";
import { useMarketplaceCandidate } from "./useMarketplaceCandidate";
import { useMcpServerCandidates } from "./useMcpServerCandidates";
import { usePersonCandidates } from "./usePersonCandidates";
import { usePluginCandidates } from "./usePluginCandidates";
import { usePolicyCandidates } from "./usePolicyCandidates";
import { useProjectCandidates } from "./useProjectCandidates";
import { useRecentCandidates } from "./useRecentCandidates";
import { useRuleCandidates } from "./useRuleCandidates";
import { useSourceCandidates } from "./useSourceCandidates";

/**
 * Every launcher candidate, in display order. Each source hook is called
 * unconditionally (Rules of Hooks) and never suspends or throws: it uses the
 * non-suspense SDK hook and yields `[]` while loading or on error, so one
 * failing endpoint cannot blank the palette.
 *
 * `enabled` is the palette's open state and is threaded into every SDK
 * query's `enabled` option, so nothing fetches while the palette is closed.
 * Project-scoped sources are further gated on `inProject`, and admin-only
 * sources on RBAC, mirroring the gates the resource groups applied before.
 */
export function useLauncherCandidates({
  enabled,
  inProject,
  recentsUserId,
  orgSlug,
  projectSlug,
}: {
  enabled: boolean;
  inProject: boolean;
  recentsUserId: string | null;
  orgSlug: string | undefined;
  projectSlug: string | undefined;
}): LauncherCandidate[] {
  const organization = useOrganization();
  const project = useProject();
  const { hasAnyScope, hasScope } = useRBAC();
  // Every org-level check names the organization: without a resource id
  // hasScope is existential (a grant on any org passes), so a member who is
  // an admin elsewhere would otherwise fire this tenant's forbidden calls.
  //
  // Risk resources are org:admin-gated on their own pages; mirror that here so
  // non-admins never fire the (forbidden) list calls.
  const isAdmin = hasAnyScope(["org:admin"], organization.id);
  // Approval requests are an org-admin surface, matching the queue page's own
  // gate.
  const canReadApprovals = hasScope("org:admin", organization.id);
  const canReadPeople = hasAnyScope(["org:read", "org:admin"], organization.id);
  // The MCP page's own gate (pages/mcp/MCP.tsx), so a member who cannot open
  // the listing never fires its (forbidden) list calls from the palette.
  // Sources are a tab of that section and their detail page gates on
  // mcp:read, so they share it.
  const canListMcp = hasAnyScope(["mcp:read", "mcp:write"]);
  // Plugins list for whoever can edit them (org admins and plugin:write on
  // this project, per usePluginWriteAccess) or read the organization; the
  // same gate the Plugins page renders behind.
  const canWritePlugins = usePluginWriteAccess();
  const canListPlugins =
    canWritePlugins || hasAnyScope(["org:read", "org:admin"], organization.id);
  // What listCatalog itself requires (server/internal/externalmcp/impl.go),
  // rather than the looser any-of gate the catalog page renders behind: an
  // mcp:write-only reader would pass that one and then have the request
  // refused. Named on the project, as the server checks it.
  const canBrowseCatalog = hasScope("project:read", project.id);

  const projectEnabled = enabled && inProject;

  const actions = useActionCandidates();
  const recents = useRecentCandidates({
    enabled,
    userId: recentsUserId,
    orgSlug,
    projectSlug,
  });
  const marketplace = useMarketplaceCandidate({ enabled, inProject });
  const mcpServers = useMcpServerCandidates({
    enabled: projectEnabled && canListMcp,
  });
  const catalog = useCatalogCandidates({
    enabled: projectEnabled && canBrowseCatalog,
  });
  const plugins = usePluginCandidates({
    enabled: projectEnabled && canListPlugins,
  });
  const assistants = useAssistantCandidates({ enabled: projectEnabled });
  const environments = useEnvironmentCandidates({ enabled: projectEnabled });
  const sources = useSourceCandidates({
    enabled: projectEnabled && canListMcp,
  });
  const deployments = useDeploymentCandidates({ enabled: projectEnabled });
  const policies = usePolicyCandidates({
    enabled: projectEnabled && isAdmin,
  });
  const rules = useRuleCandidates({ enabled: projectEnabled && isAdmin });
  const accessRequests = useAccessRequestCandidates({
    enabled: projectEnabled && canReadApprovals,
  });
  const people = usePersonCandidates({ enabled: enabled && canReadPeople });
  // Org-scoped, so offered from both shells: at the org level picking a
  // project is the palette's main job, inside a project it is a switcher.
  const projects = useProjectCandidates({ enabled });

  return useMemo(() => {
    const projectOnly = (list: LauncherCandidate[]) => (inProject ? list : []);
    const adminOnly = (list: LauncherCandidate[]) =>
      inProject && isAdmin ? list : [];
    return [
      ...actions,
      ...recents,
      ...marketplace,
      ...(inProject && canListMcp ? mcpServers : []),
      // Directly below the project's own servers: when a name matches both,
      // what you already run should read first and the catalog offer second.
      ...(inProject && canBrowseCatalog ? catalog : []),
      ...(inProject && canListPlugins ? plugins : []),
      ...projectOnly(assistants),
      ...projectOnly(environments),
      ...(inProject && canListMcp ? sources : []),
      ...projectOnly(deployments),
      ...adminOnly(policies),
      ...adminOnly(rules),
      ...(inProject && canReadApprovals ? accessRequests : []),
      ...(canReadPeople ? people : []),
      ...projects,
    ];
  }, [
    inProject,
    isAdmin,
    canReadApprovals,
    canReadPeople,
    canListMcp,
    canListPlugins,
    canBrowseCatalog,
    actions,
    recents,
    marketplace,
    mcpServers,
    catalog,
    plugins,
    assistants,
    environments,
    sources,
    deployments,
    policies,
    rules,
    accessRequests,
    people,
    projects,
  ]);
}
