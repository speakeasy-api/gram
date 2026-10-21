import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useSkillsInfinite } from "@gram/client/react-query/skills.js";
import { useEffect, useMemo } from "react";
import type { LauncherCandidate } from "./types";

const SKILL_PAGE_SIZE = 200;

export function useSkillCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const gramProject = useProjectSlugForRequests();
  const {
    data,
    fetchNextPage,
    hasNextPage,
    isFetchNextPageError,
    isFetchingNextPage,
  } = useSkillsInfinite({ gramProject, limit: SKILL_PAGE_SIZE }, undefined, {
    enabled,
    retry: false,
    throwOnError: false,
  });

  // The palette has no pagination UI. Drain the cursor while it is open so a
  // skill remains searchable regardless of which API page contains it. Stop
  // after a failed next-page request instead of retrying in a render loop.
  useEffect(() => {
    if (
      !enabled ||
      !hasNextPage ||
      isFetchingNextPage ||
      isFetchNextPageError
    ) {
      return;
    }

    void fetchNextPage().catch(() => undefined);
  }, [
    enabled,
    fetchNextPage,
    hasNextPage,
    isFetchNextPageError,
    isFetchingNextPage,
  ]);

  return useMemo(
    () =>
      (data?.pages ?? []).flatMap((page) =>
        page.result.skills.map((skill): LauncherCandidate => ({
          id: `skill:${skill.id}`,
          kind: "skill",
          title: skill.displayName,
          detail: `Skill · ${skill.classification}`,
          keywords: ["skill", skill.name, skill.id, ...skill.tags],
          verbs: ["open"],
          icon: "terminal",
          group: "Skills",
          run: () => routes.skills.detail.overview.goTo(skill.id),
        })),
      ),
    [data?.pages, routes],
  );
}
