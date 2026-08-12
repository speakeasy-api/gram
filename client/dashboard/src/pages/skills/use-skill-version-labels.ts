import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { useSkillVersionsInfinite } from "@gram/client/react-query/skillVersions.js";

export function useSkillVersionLabels(
  skillId: string,
  versionCount: number,
): {
  versionLabels: Map<string, string>;
  versionsLoading: boolean;
  versionsError: Error | null;
} {
  const versionsQuery = useSkillVersionsInfinite({ id: skillId }, undefined, {
    throwOnError: false,
    enabled: !!skillId,
  });
  useDrainInfiniteQuery(versionsQuery, !!skillId);

  const versions =
    versionsQuery.data?.pages.flatMap((page) => page.result.versions) ?? [];
  const versionLabels = new Map(
    [...versions]
      .sort((left, right) => {
        const createdAtDifference =
          left.createdAt.getTime() - right.createdAt.getTime();
        if (createdAtDifference !== 0) return createdAtDifference;
        if (left.id < right.id) return -1;
        if (left.id > right.id) return 1;
        return 0;
      })
      .map((version, index) => [
        version.id,
        `v${versionCount - versions.length + index + 1} (${version.canonicalSha256.slice(0, 8)})`,
      ]),
  );

  return {
    versionLabels,
    versionsLoading:
      !versionsQuery.error &&
      (versionsQuery.isPending ||
        versionsQuery.hasNextPage ||
        versionsQuery.isFetchingNextPage),
    versionsError: versionsQuery.error,
  };
}
