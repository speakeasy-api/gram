import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import type { SkillEditSuggestion } from "@gram/client/models/components/skilleditsuggestion.js";
import { useSkillSuggestionsInfinite } from "@gram/client/react-query/skillSuggestions.js";
import { useMemo } from "react";

export function useOpenSkillSuggestions(): {
  query: ReturnType<typeof useSkillSuggestionsInfinite>;
  suggestions: SkillEditSuggestion[];
  skillIds: Set<string>;
  total: number;
  fullyLoaded: boolean;
} {
  const query = useSkillSuggestionsInfinite({ limit: 50 }, undefined, {
    throwOnError: false,
  });
  useDrainInfiniteQuery(query);
  const suggestions = useMemo(
    () => query.data?.pages.flatMap((page) => page.result.suggestions) ?? [],
    [query.data?.pages],
  );
  const skillIds = useMemo(
    () => new Set(suggestions.map((suggestion) => suggestion.skillId)),
    [suggestions],
  );
  const fullyLoaded =
    !!query.data &&
    !query.hasNextPage &&
    !query.isFetchingNextPage &&
    !query.error;
  const serverTotal = query.data?.pages[0]?.result.totalOpenCount ?? 0;
  const total = fullyLoaded ? suggestions.length : serverTotal;
  return { query, suggestions, skillIds, total, fullyLoaded };
}
