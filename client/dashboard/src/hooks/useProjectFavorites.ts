import { useCallback, useMemo } from "react";

import { useLocalStorageState } from "@/hooks/useLocalStorageState";

const STORAGE_PREFIX = "gram:org-favorites:";

export function useProjectFavorites(orgId: string) {
  const [favoriteIds, setFavoriteIds] = useLocalStorageState<string[]>(
    `${STORAGE_PREFIX}${orgId}`,
    [],
  );

  const favoriteSet = useMemo(() => new Set(favoriteIds), [favoriteIds]);

  const isFavorite = useCallback(
    (projectId: string) => favoriteSet.has(projectId),
    [favoriteSet],
  );

  const toggleFavorite = useCallback(
    (projectId: string) => {
      setFavoriteIds((prev) =>
        prev.includes(projectId)
          ? prev.filter((id) => id !== projectId)
          : [...prev, projectId],
      );
    },
    [setFavoriteIds],
  );

  return { favoriteIds, favoriteSet, isFavorite, toggleFavorite };
}
