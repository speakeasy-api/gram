import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

function detailFor(serverCount: number | undefined): string {
  if (serverCount === undefined) return "Plugin";
  return serverCount === 1
    ? "Plugin · 1 server"
    : `Plugin · ${serverCount} servers`;
}

export function usePluginCandidates({
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
  const { data } = usePlugins({ gramProject }, undefined, {
    enabled,
    throwOnError: false,
  });

  return useMemo(
    () =>
      (data?.plugins ?? []).map((plugin): LauncherCandidate => ({
        id: `plugin:${plugin.id}`,
        kind: "plugin",
        title: plugin.name,
        detail: detailFor(plugin.serverCount ?? plugin.servers?.length),
        keywords: ["plugin", plugin.slug, plugin.id],
        verbs: ["open"],
        icon: "puzzle",
        group: "Plugins",
        run: () => routes.plugins.detail.goTo(plugin.id),
      })),
    [data, routes],
  );
}
