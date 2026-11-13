import { useOrganization, useProject } from "@/contexts/Auth";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { useRBAC } from "@/hooks/useRBAC";
import { useCallback, useEffect, useMemo } from "react";
import type { LauncherCandidate } from "./types";
import { useAccessRequestCandidates } from "./useAccessRequestCandidates";
import { useInferenceSetupCandidate } from "./useInferenceSetupCandidate";
import { useActionCandidates } from "./useActionCandidates";
import { useAgentIdentityCandidates } from "./useAgentIdentityCandidates";
import { useAssistantCandidates } from "./useAssistantCandidates";
import { useCatalogCandidates } from "./useCatalogCandidates";
import { useDeploymentCandidates } from "./useDeploymentCandidates";
import { useGatewayConnectionCandidates } from "./useGatewayConnectionCandidates";
import { useEnvironmentCandidates } from "./useEnvironmentCandidates";
import { useGatewayCandidates } from "./useGatewayCandidates";
import { useMarketplaceCandidate } from "./useMarketplaceCandidate";
import { useManagedDeviceCandidates } from "./useManagedDeviceCandidates";
import { useMcpServerCandidates } from "./useMcpServerCandidates";
import { usePersonCandidates } from "./usePersonCandidates";
import { usePlatformSetupCandidates } from "./usePlatformSetupCandidates";
import { usePluginCandidates } from "./usePluginCandidates";
import { usePolicyCandidates } from "./usePolicyCandidates";
import { useProjectCandidates } from "./useProjectCandidates";
import { useRecentCandidates } from "./useRecentCandidates";
import {
  useBuiltinRuleCandidates,
  useRuleCandidates,
} from "./useRuleCandidates";
import {
  createGrantFingerprint,
  type ProjectResourceIndexMetrics,
  useProjectResourceIndexCandidates,
} from "./useProjectResourceIndexCandidates";
import { useSkillCandidates } from "./useSkillCandidates";
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
 *
 * Session titles are intentionally excluded: transcripts are high-volume,
 * sensitive history, so opening the palette must not crawl their pages. The
 * Agent Sessions page action opens the scoped search; an agent's profile links
 * there with its agent and time-window filters. See docs/rbac.md.
 */
// Channels is hidden: it contributes no page action and no named
// conversations, and is reachable only by direct URL. Private titles must not
// escape their project cache. Discovery pages the live message archive (including DM previews and endpoint
// attribution), with no metadata search/public-only endpoint. Crawling every
// page on global launcher opens would duplicate the archive and its access-
// revocation cache outside Channels. Keep this sensitive, high-volume archive
// in the Channels list; do not register a partial first-page resource index.
export function useLauncherCandidates({
  enabled,
  inProject,
  recentsUserId,
  orgSlug,
  projectSlug,
  query: _query,
  onIndexMetrics,
}: {
  enabled: boolean;
  inProject: boolean;
  recentsUserId: string | null;
  orgSlug: string | undefined;
  projectSlug: string | undefined;
  query: string;
  onIndexMetrics?: (metrics: ProjectResourceIndexMetrics) => void;
}): LauncherCandidate[] {
  const organization = useOrganization();
  const project = useProject();
  const { grants, hasAnyScope, hasScope } = useRBAC();
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
  // The Device Agent pages' own gate.
  const canReadDevices = canReadPeople;
  // The MCP page's own gate (pages/mcp/MCP.tsx), so a member who cannot open
  // the listing never fires its (forbidden) list calls from the palette.
  // Sources are a tab of that section and their detail page gates on
  // mcp:read, so they share it.
  const canListMcp = hasAnyScope(["mcp:read", "mcp:write"]);
  // Listing gateways requires mcp:read on the project itself
  // (server/internal/metamcp/impl.go), so a grant in another project must not
  // fire the forbidden list call here or keep its cached names visible.
  const canListGateways = hasAnyScope(["mcp:read", "mcp:write"], project.id);
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
  const canReadAssistants = hasScope("assistant:read", undefined, project.id);

  const canReadSkills = hasScope("skill:read", project.id);

  const projectEnabled = enabled && inProject;
  // Row-level assistant and MCP grants can change without changing any coarse
  // page gate. Keep the canonical grant fingerprint local to the query cache; it
  // is never included in palette telemetry.
  const accessKey = useMemo(() => createGrantFingerprint(grants), [grants]);
  const resourceIndex = useProjectResourceIndexCandidates({
    enabled: projectEnabled,
    accessKey,
  });
  useEffect(() => {
    onIndexMetrics?.(resourceIndex.metrics);
  }, [onIndexMetrics, resourceIndex.metrics]);
  // A partial response identifies exactly which inventories need legacy
  // coverage. Successful remote kinds never re-enable their old list calls.
  const fallback = useCallback(
    (kind: string) =>
      projectEnabled && resourceIndex.fallbackKinds.has(kind as never),
    [projectEnabled, resourceIndex.fallbackKinds],
  );

  const gatewayConnections = useGatewayConnectionCandidates({
    enabled,
    inProject,
  });
  const actions = useActionCandidates();
  const recents = useRecentCandidates({
    enabled,
    userId: recentsUserId,
    orgSlug,
    projectSlug,
  });
  const marketplace = useMarketplaceCandidate({ enabled, inProject });
  // Keep the MCP inventory live even after its open/detail rows migrate: the
  // local candidates carry visibility state and per-server mutation verbs that
  // the compact remote index intentionally does not model. Overlapping remote
  // open rows are deduplicated below, while remote-only rows remain available.
  const mcpServers = useMcpServerCandidates({
    enabled: projectEnabled && canListMcp,
  });
  const gateways = useGatewayCandidates({
    enabled: fallback("gateway") && canListGateways,
  });
  const catalog = useCatalogCandidates({
    enabled: projectEnabled && canBrowseCatalog,
  });
  const plugins = usePluginCandidates({
    enabled: fallback("plugin") && canListPlugins,
  });
  const assistants = useAssistantCandidates({
    enabled: fallback("assistant") && canReadAssistants,
  });
  const skills = useSkillCandidates({
    enabled: fallback("skill") && canReadSkills,
  });
  const inferenceSetup = useInferenceSetupCandidate({ enabled, inProject });
  const platformSetups = usePlatformSetupCandidates({ enabled, inProject });
  const agentIdentities = useAgentIdentityCandidates({ enabled });
  const environments = useEnvironmentCandidates({
    enabled: fallback("environment"),
  });
  const sources = useSourceCandidates({
    enabled: projectEnabled && canListMcp,
  });
  // Deployment rows are synthetic history entries, not durable named
  // resources, so they remain on their existing independent source.
  const deployments = useDeploymentCandidates({
    enabled: projectEnabled,
  });
  const policies = usePolicyCandidates({
    enabled: projectEnabled && isAdmin,
  });
  const rules = useRuleCandidates({
    enabled: fallback("rule") && isAdmin,
    includeBuiltins: false,
  });
  const builtinRules = useBuiltinRuleCandidates({
    enabled: projectEnabled && isAdmin,
  });
  const accessRequests = useAccessRequestCandidates({
    enabled: projectEnabled && canReadApprovals,
  });
  const people = usePersonCandidates({ enabled: enabled && canReadPeople });
  const devices = useManagedDeviceCandidates({
    enabled: enabled && canReadDevices,
  });
  // Org-scoped, so offered from both shells: at the org level picking a
  // project is the palette's main job, inside a project it is a switcher.
  const projects = useProjectCandidates({ enabled });

  return useMemo(() => {
    const projectOnly = (list: LauncherCandidate[]) => (inProject ? list : []);
    const adminOnly = (list: LauncherCandidate[]) =>
      inProject && isAdmin ? list : [];
    const localMcpRouteIDs = new Set(
      mcpServers.map((candidate) => candidate.id.replace(/^mcp:/, "")),
    );
    const indexedCandidates = resourceIndex.candidates.filter((candidate) => {
      if (candidate.kind !== "mcp_server") return true;
      const identity = candidate.resourceIdentity;
      return (
        !identity ||
        (!localMcpRouteIDs.has(identity.id) &&
          !localMcpRouteIDs.has(identity.routeSelector))
      );
    });
    return [
      ...actions,
      ...gatewayConnections,
      ...recents,
      ...marketplace,
      ...(inProject && canListMcp ? mcpServers : []),
      ...(inProject ? indexedCandidates : []),
      ...(fallback("gateway") && canListGateways ? gateways : []),
      // Directly below the project's own servers: when a name matches both,
      // what you already run should read first and the catalog offer second.
      ...(inProject && canBrowseCatalog ? catalog : []),
      ...(fallback("plugin") && canListPlugins ? plugins : []),
      ...(fallback("assistant") && canReadAssistants ? assistants : []),
      ...(fallback("skill") && canReadSkills ? skills : []),
      ...agentIdentities,
      ...inferenceSetup,
      ...platformSetups,
      ...(fallback("environment") ? projectOnly(environments) : []),
      ...(inProject && canListMcp ? sources : []),
      ...projectOnly(deployments),
      ...adminOnly(policies),
      ...adminOnly(builtinRules),
      ...(fallback("rule") ? adminOnly(rules) : []),
      ...(inProject && canReadApprovals ? accessRequests : []),
      ...(canReadPeople ? people : []),
      ...(canReadDevices ? devices : []),
      ...projects,
    ];
  }, [
    inProject,
    isAdmin,
    canReadApprovals,
    canReadPeople,
    canReadDevices,
    canListMcp,
    canListGateways,
    canListPlugins,
    canBrowseCatalog,
    canReadAssistants,
    canReadSkills,
    resourceIndex.candidates,
    fallback,
    skills,
    agentIdentities,
    inferenceSetup,
    platformSetups,
    actions,
    gatewayConnections,
    recents,
    marketplace,
    mcpServers,
    gateways,
    catalog,
    plugins,
    assistants,
    environments,
    sources,
    deployments,
    policies,
    rules,
    builtinRules,
    accessRequests,
    people,
    devices,
    projects,
  ]);
}
