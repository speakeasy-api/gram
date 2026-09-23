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
  const { data } = usePlugins(undefined, undefined, { enabled });

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
