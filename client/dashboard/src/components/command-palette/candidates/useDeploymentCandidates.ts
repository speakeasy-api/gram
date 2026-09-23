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
  const { data } = useListDeployments(undefined, undefined, { enabled });

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
