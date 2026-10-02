import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

export function useEnvironmentCandidates({
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
  const { data } = useListEnvironments({ gramProject }, undefined, {
    enabled,
    refetchOnWindowFocus: false,
    throwOnError: false,
  });

  return useMemo(
    () =>
      (data?.environments ?? []).map((environment): LauncherCandidate => ({
        id: `environment:${environment.id}`,
        kind: "environment",
        title: environment.name,
        detail: `Environment · ${environment.slug}`,
        keywords: ["environment", environment.slug, environment.id],
        verbs: ["open"],
        icon: "blocks",
        group: "Environments",
        run: () => routes.environments.environment.goTo(environment.slug),
      })),
    [data, routes],
  );
}
