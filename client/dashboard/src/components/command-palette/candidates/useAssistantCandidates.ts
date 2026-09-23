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
  const { data } = useAssistantsList(undefined, undefined, {
    enabled,
    retry: false,
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
