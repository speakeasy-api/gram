import {
  isOverviewEmpty,
  useAllTimeProjectOverview,
} from "@/components/project-guide/allTimeOverviewQuery";
import {
  decideProjectGuideStatus,
  type ProjectGuideStatus,
} from "@/components/project-guide/projectGuideGate";
import {
  useProjectGuideDismissed,
  useProjectGuideStarted,
} from "@/components/project-guide/projectGuideStores";
import { useProjectSlugForRequests, useSlugs } from "@/contexts/Sdk";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";

/**
 * Whether project home shows the project guide, the existing dashboard, or
 * neither yet. See `decideProjectGuideStatus` for the ordering and why it is
 * evaluated on load rather than continuously.
 *
 * The rollout flag comes off with the journey bodies. Until then the shell has
 * placeholder steps, so it must not reach a real new admin — and local dev
 * enables every flag, so review is unaffected.
 */
export function useProjectGuide(): { status: ProjectGuideStatus } {
  const { orgSlug, projectSlug } = useSlugs();
  const gramProject = useProjectSlugForRequests();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const flag = useFeatureFlag(FEATURE_FLAGS.enterpriseTrials);

  const { data: features, isPending: featuresPending } = useProductFeatures();

  const dismissed = useProjectGuideDismissed(projectSlug);
  const started = useProjectGuideStarted(projectSlug);

  const isAdmin = hasScope("org:admin");
  const logsEnabled = features?.logsEnabled === true;

  // A candidate is an admin with logs who has not dismissed the guide. Only
  // candidates pay for the emptiness queries. `orgSlug` mirrors the guard
  // `useAllTimeProjectOverview` applies internally, so the two never disagree
  // about whether there is enough routing context to query.
  const candidate =
    Boolean(orgSlug) &&
    Boolean(projectSlug) &&
    flag.status === "enabled" &&
    !rbacLoading &&
    !featuresPending &&
    isAdmin &&
    logsEnabled &&
    !dismissed &&
    !started;

  // Keyed by project (`gramProject`) so a project switch — which leaves the
  // previous project's cache entry in place under react-query's
  // invalidate-but-keep-data behavior — doesn't serve another project's
  // servers/policies while this project's query is still "pending".
  const {
    data: serversData,
    isPending: serversPending,
    isError: serversError,
  } = useMcpServers({ gramProject }, undefined, {
    enabled: candidate,
    throwOnError: false,
  });
  const hasServers = (serversData?.mcpServers.length ?? 0) > 0;

  const {
    data: policiesData,
    isPending: policiesPending,
    isError: policiesError,
  } = useRiskListPolicies({ gramProject }, undefined, {
    enabled: candidate,
    throwOnError: false,
  });
  const hasPolicies = (policiesData?.policies.length ?? 0) > 0;

  // The expensive one: only asked once both cheap checks came back empty.
  const {
    data: overview,
    isPending: overviewPending,
    isError: overviewError,
  } = useAllTimeProjectOverview({
    enabled:
      candidate &&
      !serversPending &&
      !serversError &&
      !hasServers &&
      !policiesPending &&
      !policiesError &&
      !hasPolicies,
  });

  const status = decideProjectGuideStatus({
    hasProjectSlug: Boolean(projectSlug),
    rbacLoading,
    isAdmin: isAdmin && flag.status === "enabled",
    featuresPending: featuresPending || flag.status === "loading",
    logsEnabled,
    dismissed,
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
  });

  return { status };
}
