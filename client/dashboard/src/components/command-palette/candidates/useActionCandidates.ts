import type { IconName } from "@/components/ui/Icon/names";
import { useCommandPalette } from "@/contexts/CommandPalette";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

function detailFor(group: string): string {
  switch (group) {
    case "Organization":
      return "Organization page";
    case "Tool Actions":
      return "Tool action";
    default:
      return "Page";
  }
}

/** Registered CommandActions (nav pages, tool actions) as candidates. */
export function useActionCandidates(): LauncherCandidate[] {
  const { actions } = useCommandPalette();

  return useMemo(
    () =>
      actions.map((action): LauncherCandidate => ({
        id: `action:${action.id}`,
        kind: "page",
        title: action.label,
        detail: detailFor(action.group ?? ""),
        keywords: [],
        verbs: ["open"],
        icon: action.icon as IconName | undefined,
        stage: action.stage,
        group: action.group || "Actions",
        run: () => action.onSelect(),
      })),
    [actions],
  );
}
