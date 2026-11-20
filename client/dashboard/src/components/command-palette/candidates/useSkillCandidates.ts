import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useSkillsInfinite } from "@gram/client/react-query/skills.js";
import { useEffect, useMemo, useRef } from "react";
import type { LauncherCandidate } from "./types";

const SKILL_PAGE_SIZE = 200;
const SKILL_INVENTORY_STALE_TIME_MS = 30_000;

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
    staleTime: SKILL_INVENTORY_STALE_TIME_MS,
    retry: false,
    throwOnError: false,
  });

  // The palette has no pagination UI. Drain the cursor while it is open so a
  // skill remains searchable regardless of which API page contains it. A
  // failed page gets one retry on the next open without looping while open.
  const opens = useRef(0);
  const lastFetchOpen = useRef(0);
  useEffect(() => {
    if (enabled) opens.current += 1;
  }, [enabled]);
  useEffect(() => {
    if (!enabled || !hasNextPage || isFetchingNextPage) return;
    if (isFetchNextPageError && lastFetchOpen.current === opens.current) return;
    lastFetchOpen.current = opens.current;
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
