import {
  catalogBackedMcpServers,
  deriveJourneyStatus,
  hasBlockingSecretsPolicy,
  hasDefaultPluginServer,
  hasMcpServerActivity,
  latestSecretsFinding,
} from "@/components/project-guide/journeyStatus";
import type {
  JourneyId,
  JourneyStatus,
} from "@/components/project-guide/journeys";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useListMCPCatalog } from "@/pages/catalog/hooks";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useGetMcpServerActivity } from "@gram/client/react-query/getMcpServerActivity.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
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

  const catalogQuery = useListMCPCatalog(undefined, undefined, true);

  const {
    data: serversData,
    isError: serversError,
    isPending: serversPending,
  } = useMcpServers({ gramProject }, undefined, { throwOnError: false });
  const {
    data: remoteServersData,
    isError: remoteServersError,
    isPending: remoteServersPending,
  } = useRemoteMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });
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
  const remoteServers = remoteServersError
    ? undefined
    : remoteServersData?.remoteMcpServers;
  const catalogServers = catalogQuery.isError
    ? undefined
    : catalogQuery.data?.servers;
  const plugins = pluginsError ? undefined : pluginsData?.plugins;
  const activity = activityError ? undefined : activityData?.activity;
  const policies = policiesError ? undefined : policiesData?.policies;
  const findings = findingsError ? undefined : secretsFindings?.results;
  const catalogMcpServers = catalogBackedMcpServers(
    servers,
    remoteServers,
    catalogServers,
  );

  const statusByJourney: Record<JourneyId, JourneyStatus> = {
    "third-party-mcp":
      servers && remoteServers && catalogServers && plugins && activity
        ? deriveJourneyStatus({
            startSignal: catalogMcpServers.length > 0,
            winSignal: catalogMcpServers.some(
              (server) =>
                hasDefaultPluginServer(plugins, server.id) &&
                hasMcpServerActivity(activity, server),
            ),
          })
        : "unreadable",
    "secret-block":
      policies && findings
        ? deriveJourneyStatus({
            startSignal: hasBlockingSecretsPolicy(policies),
            winSignal: Boolean(latestSecretsFinding(findings)),
          })
        : "unreadable",
  };

  return {
    statusByJourney,
    isPending:
      serversPending ||
      remoteServersPending ||
      catalogQuery.isPending ||
      pluginsPending ||
      activityPending ||
      policiesPending ||
      findingsPending,
  };
}
