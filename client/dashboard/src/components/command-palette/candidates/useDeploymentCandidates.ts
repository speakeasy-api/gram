import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useListDeployments } from "@gram/client/react-query/listDeployments.js";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

export function useDeploymentCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const gramProject = useProjectSlugForRequests();
  // Keyed by project: the SDK folds gramProject into the query key, so
  // omitting it would share one cache entry across projects. Never throws:
  // a failing source degrades to no candidates rather than blanking the
  // palette.
  const { data } = useListDeployments({ gramProject }, undefined, {
    enabled,
    throwOnError: false,
  });

  return useMemo(
    () =>
      (data?.items ?? []).map((deployment): LauncherCandidate => ({
        id: `deployment:${deployment.id}`,
        kind: "deployment",
        title: `Deployment ${deployment.id.slice(0, 8)}`,
        detail: `Deployment · ${deployment.status}`,
        keywords: ["deployment", deployment.id, deployment.status],
        verbs: ["open"],
        icon: "history",
        group: "Deployments",
        run: () => routes.deployments.deployment.goTo(deployment.id),
      })),
    [data, routes],
  );
}
