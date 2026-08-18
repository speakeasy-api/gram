import { useAllTimeProjectOverview } from "@/components/project-guide/allTimeOverviewQuery";
import {
  deriveJourneyStatus,
  hasBlockingSecretsPolicy,
  hasCatalogBackedServer,
} from "@/components/project-guide/journeyStatus";
import type {
  JourneyId,
  JourneyStatus,
} from "@/components/project-guide/journeys";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
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

  const { data: serversData, isPending: serversPending } = useMcpServers(
    { gramProject },
    undefined,
    { throwOnError: false },
  );
  const { data: policiesData, isPending: policiesPending } =
    useRiskListPolicies({ gramProject }, undefined, {
      throwOnError: false,
    });
  const { data: overview, isPending: overviewPending } =
    useAllTimeProjectOverview({ enabled: true });
  // One row is enough: the card only asks whether a secrets finding exists.
  const { data: secretsFindings, isPending: findingsPending } =
    useRiskListResults(
      { gramProject, category: "secrets", limit: 1 },
      undefined,
      { throwOnError: false },
    );

  const statusByJourney: Record<JourneyId, JourneyStatus> = {
    "third-party-mcp": deriveJourneyStatus({
      startSignal: hasCatalogBackedServer(serversData?.mcpServers),
      winSignal: (overview?.summary.totalToolCalls ?? 0) > 0,
    }),
    "secret-block": deriveJourneyStatus({
      startSignal: hasBlockingSecretsPolicy(policiesData?.policies),
      winSignal: (secretsFindings?.results.length ?? 0) > 0,
    }),
  };

  return {
    statusByJourney,
    isPending:
      serversPending || policiesPending || overviewPending || findingsPending,
  };
}
