import { useEffect, useState } from "react";

export interface GithubUserResult {
  login: string;
  avatar_url: string;
  type: string;
}

async function searchGithubUsers(
  query: string,
  signal: AbortSignal,
): Promise<GithubUserResult[]> {
  const resp = await fetch(
    `https://api.github.com/search/users?q=${encodeURIComponent(query)}&per_page=5`,
    {
      signal,
      headers: { Accept: "application/vnd.github+json" },
    },
  );
  if (!resp.ok) throw new Error("GitHub search unavailable");
  const data = (await resp.json()) as { items?: GithubUserResult[] };
  return data.items ?? [];
}

export const MIN_GITHUB_SEARCH_LENGTH = 2;
const EMPTY_RESULTS: GithubUserResult[] = [];

export function useGitHubAccountSearch(
  query: string,
  organizationsOnly = false,
): {
  results: GithubUserResult[];
  loading: boolean;
  failed: boolean;
} {
  const term = query.trim();
  const enabled =
    term.length >= (organizationsOnly ? 1 : MIN_GITHUB_SEARCH_LENGTH);
  const searchQuery = organizationsOnly ? `${term} in:login type:org` : term;
  const [response, setResponse] = useState<{
    query: string;
    items: GithubUserResult[];
    failed: boolean;
  }>();

  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    const load = async () => {
      try {
        const items = await searchGithubUsers(searchQuery, controller.signal);
        if (!controller.signal.aborted) {
          setResponse({
            query: searchQuery,
            items: organizationsOnly
              ? items.filter((item) => item.type === "Organization")
              : items,
            failed: false,
          });
        }
      } catch {
        if (!controller.signal.aborted) {
          setResponse({ query: searchQuery, items: [], failed: true });
        }
      }
    };
    const timer = setTimeout(() => void load(), 300);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [enabled, searchQuery, organizationsOnly]);

  const current =
    enabled && response?.query === searchQuery ? response : undefined;
  return {
    results: current?.items ?? EMPTY_RESULTS,
    loading: enabled && !current,
    failed: current?.failed ?? false,
  };
}
