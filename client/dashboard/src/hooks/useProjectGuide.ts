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

export function useProjectGuide(args: { enabled?: boolean } = {}): {
  status: ProjectGuideStatus;
} {
  const { projectSlug } = useSlugs();
  const gramProject = useProjectSlugForRequests();
  const started = useProjectGuideStarted(projectSlug);
  const canCheck = args.enabled !== false && Boolean(projectSlug) && !started;

  const {
    data: serversData,
    isPending: serversPending,
    isError: serversError,
  } = useMcpServers({ gramProject }, undefined, {
    enabled: canCheck,
    throwOnError: false,
  });
  const serversUnreadable =
    serversError || (!serversPending && serversData === undefined);
  const hasServers = (serversData?.mcpServers.length ?? 0) > 0;

  const {
    data: policiesData,
    isPending: policiesPending,
    isError: policiesError,
  } = useRiskListPolicies({ gramProject }, undefined, {
    enabled: canCheck,
    throwOnError: false,
  });
  const policiesUnreadable =
    policiesError || (!policiesPending && policiesData === undefined);
  const hasPolicies = (policiesData?.policies.length ?? 0) > 0;

  const {
    data: overview,
    isPending: overviewPending,
    isError: overviewError,
  } = useAllTimeProjectOverview({
    enabled:
      canCheck &&
      !serversPending &&
      !serversUnreadable &&
      !hasServers &&
      !policiesPending &&
      !policiesUnreadable &&
      !hasPolicies,
  });
  const overviewUnreadable =
    overviewError || (!overviewPending && overview === undefined);

  return {
    status: decideProjectGuideStatus({
      hasProjectSlug: Boolean(projectSlug),
      started,
      serversPending,
      serversError: serversUnreadable,
      hasServers,
      policiesPending,
      policiesError: policiesUnreadable,
      hasPolicies,
      overviewPending,
      overviewError: overviewUnreadable,
      hasData: !isOverviewEmpty(overview),
    }),
  };
}
