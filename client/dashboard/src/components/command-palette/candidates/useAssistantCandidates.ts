import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

export function useAssistantCandidates({
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
  const { data } = useAssistantsList({ gramProject }, undefined, {
    enabled,
    retry: false,
    throwOnError: false,
  });

  return useMemo(
    () =>
      (data?.assistants ?? []).map((assistant): LauncherCandidate => ({
        id: `assistant:${assistant.id}`,
        kind: "assistant",
        title: assistant.name,
        detail: `Assistant · ${assistant.status}`,
        keywords: ["assistant", assistant.id],
        verbs: ["open"],
        icon: "bot",
        group: "Assistants",
        run: () => routes.assistants.detail.goTo(assistant.id),
      })),
    [data, routes],
  );
}
