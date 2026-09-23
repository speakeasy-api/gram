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
  const { data } = useListEnvironments(undefined, undefined, {
    enabled,
    refetchOnWindowFocus: false,
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
