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
  const { hasAnyScope, hasScope } = useRBAC();
  // Risk resources are org:admin-gated on their own pages; mirror that here so
  // non-admins never fire the (forbidden) list calls.
  const isAdmin = hasAnyScope(["org:admin"]);
  // Approval requests are an org-admin surface, matching the queue page.
  const canReadApprovals = hasScope("org:admin");
  const canReadPeople = hasAnyScope(["org:read", "org:admin"]);
  // The MCP page's own gate (pages/mcp/MCP.tsx), so a member who cannot open
  // the listing never fires its (forbidden) list calls from the palette.
  // Sources are a tab of that section and their detail page gates on
  // mcp:read, so they share it.
  const canListMcp = hasAnyScope(["mcp:read", "mcp:write"]);
  // The scopes that reach the Plugins page from the nav
  // (hooks/useProjectNavRoutes.ts), so the list is only fetched for members
  // who can open it.
  const canListPlugins = hasAnyScope(["project:read", "project:write"]);
  // What listCatalog itself requires (server/internal/externalmcp/impl.go),
  // rather than the looser any-of gate the catalog page renders behind: an
  // mcp:write-only reader would pass that one and then have the request
  // refused.
  const canBrowseCatalog = hasScope("project:read");

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
  ]);
}
