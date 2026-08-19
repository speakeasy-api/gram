import { useAllTimeProjectOverview } from "@/components/project-guide/allTimeOverviewQuery";
import {
  deriveJourneyStatus,
  hasBlockingSecretsPolicy,
  hasCatalogBackedServer,
  hasDefaultPluginServer,
  hasMcpServerActivity,
  latestSecretsFinding,
} from "@/components/project-guide/journeyStatus";
import type {
  JourneyId,
  JourneyStatus,
} from "@/components/project-guide/journeys";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useGetMcpServerActivity } from "@gram/client/react-query/getMcpServerActivity.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRiskListResults } from "@gram/client/react-query/riskListResults.js";

/**
 * Per-journey card status. Keyed by project (`gramProject`) so project switches
 * cannot reuse another project's servers or policies; the secrets-findings
 * query is this hook's own.
 */
export function useProjectGuideProgress(): {
  statusByJourney: Record<JourneyId, JourneyStatus>;
  isPending: boolean;
} {
  const gramProject = useProjectSlugForRequests();

  const {
    data: serversData,
    isError: serversError,
    isPending: serversPending,
  } = useMcpServers({ gramProject }, undefined, { throwOnError: false });
  const {
    data: pluginsData,
    isError: pluginsError,
    isPending: pluginsPending,
  } = usePlugins({ gramProject }, undefined, { throwOnError: false });
  const {
    data: activityData,
    isError: activityError,
    isPending: activityPending,
  } = useGetMcpServerActivity(
    { gramProject, getMcpServerActivityPayload: {} },
    undefined,
    { throwOnError: false },
  );
  const {
    data: policiesData,
    isError: policiesError,
    isPending: policiesPending,
  } = useRiskListPolicies({ gramProject }, undefined, {
    throwOnError: false,
  });
  const { isPending: overviewPending } = useAllTimeProjectOverview({
    enabled: true,
  });
  // One row is enough: the card only asks whether a secrets finding exists.
  const {
    data: secretsFindings,
    isError: findingsError,
    isPending: findingsPending,
  } = useRiskListResults(
    { gramProject, category: "secrets", limit: 1 },
    undefined,
    { throwOnError: false },
  );

  const servers = serversError ? undefined : serversData?.mcpServers;
  const plugins = pluginsError ? undefined : pluginsData?.plugins;
  const activity = activityError ? undefined : activityData?.activity;
  const policies = policiesError ? undefined : policiesData?.policies;
  const findings = findingsError ? undefined : secretsFindings?.results;

  const statusByJourney: Record<JourneyId, JourneyStatus> = {
    "third-party-mcp":
      servers && plugins && activity
        ? deriveJourneyStatus({
            startSignal: hasCatalogBackedServer(servers),
            winSignal: servers.some(
              (server) =>
                Boolean(server.remoteMcpServerId) &&
                hasDefaultPluginServer(plugins, server.id) &&
                hasMcpServerActivity(activity, server),
            ),
          })
        : "in-progress",
    "secret-block":
      policies && findings
        ? deriveJourneyStatus({
            startSignal: hasBlockingSecretsPolicy(policies),
            winSignal: Boolean(latestSecretsFinding(findings)),
          })
        : "in-progress",
  };

  return {
    statusByJourney,
    isPending:
      serversPending ||
      pluginsPending ||
      activityPending ||
      policiesPending ||
      overviewPending ||
      findingsPending,
  };
}
