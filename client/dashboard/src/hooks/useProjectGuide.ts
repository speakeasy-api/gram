import {
  isOverviewEmpty,
  useAllTimeProjectOverview,
} from "@/components/project-guide/allTimeOverviewQuery";
import {
  decideProjectGuideStatus,
  type ProjectGuideStatus,
} from "@/components/project-guide/projectGuideGate";
import { useProjectGuideStarted } from "@/components/project-guide/projectGuideStores";
import { useProjectSlugForRequests, useSlugs } from "@/contexts/Sdk";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";

export function useProjectGuide(): { status: ProjectGuideStatus } {
  const { projectSlug } = useSlugs();
  const gramProject = useProjectSlugForRequests();
  const started = useProjectGuideStarted(projectSlug);
  const canCheck = Boolean(projectSlug) && !started;

  const {
    data: serversData,
    isPending: serversPending,
    isError: serversError,
  } = useMcpServers({ gramProject }, undefined, {
    enabled: canCheck,
    throwOnError: false,
  });
  const hasServers = (serversData?.mcpServers.length ?? 0) > 0;

  const {
    data: policiesData,
    isPending: policiesPending,
    isError: policiesError,
  } = useRiskListPolicies({ gramProject }, undefined, {
    enabled: canCheck,
    throwOnError: false,
  });
  const hasPolicies = (policiesData?.policies.length ?? 0) > 0;

  const {
    data: overview,
    isPending: overviewPending,
    isError: overviewError,
  } = useAllTimeProjectOverview({
    enabled:
      canCheck &&
      !serversPending &&
      !serversError &&
      !hasServers &&
      !policiesPending &&
      !policiesError &&
      !hasPolicies,
  });

  return {
    status: decideProjectGuideStatus({
      hasProjectSlug: Boolean(projectSlug),
      started,
      serversPending,
      serversError,
      hasServers,
      policiesPending,
      policiesError,
      hasPolicies,
      overviewPending,
      overviewError,
      hasData: !isOverviewEmpty(overview),
    }),
  };
}
