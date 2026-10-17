import { useOrganization } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { usePluginQueryScope } from "@/pages/plugins/usePluginQueryScope";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";

/**
 * Whether the dashboard shows skills as served over MCP. The server reads the
 * same org-targeted flag before it serves anything, so the hints only appear
 * where agents can really load skills.
 */
export function useSkillsOverMcpEnabled(): boolean {
  return useFeatureFlag(FEATURE_FLAGS.skillsOverMcp).status === "enabled";
}

/**
 * The project's plugins with the servers each includes, for working out which
 * hosted MCP servers serve a skill. Plugin listing needs org read or plugin
 * write, so the query stays off for readers who lack both rather than failing.
 */
export function useProjectPluginsWithServers(enabled = true): {
  /** False until the grants have loaded and allow listing plugins. */
  canReadPlugins: boolean;
  plugins: Plugin[] | undefined;
  /** Grants still loading, so canReadPlugins is not yet decided. */
  isPermissionLoading: boolean;
  /** Permission or plugin list still loading; never a settled denial. */
  isLoading: boolean;
  error: unknown;
  refetch: () => void;
} {
  const organization = useOrganization();
  const { hasAnyScope, isLoading: isLoadingGrants } = useRBAC();
  const canWritePlugins = usePluginWriteAccess();
  const canReadPlugins =
    !isLoadingGrants &&
    (canWritePlugins ||
      hasAnyScope(["org:read", "org:admin"], organization.id));
  const scope = usePluginQueryScope();
  const query = usePlugins(scope, undefined, {
    throwOnError: false,
    enabled: enabled && canReadPlugins,
  });
  // A disabled query can still expose a previous identity's cache; never hand
  // that to the page after authorization is lost.
  const isPermissionLoading = enabled && isLoadingGrants;
  return {
    canReadPlugins,
    plugins: canReadPlugins ? query.data?.plugins : undefined,
    isPermissionLoading,
    isLoading:
      isPermissionLoading || (enabled && canReadPlugins && query.isLoading),
    error: query.error,
    refetch: () => void query.refetch(),
  };
}
