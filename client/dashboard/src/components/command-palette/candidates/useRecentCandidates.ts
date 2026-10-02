import type { IconName } from "@/components/ui/Icon/names";
import { useMemo } from "react";
import { useNavigate } from "react-router";
import { getRecentLabelOverride, useRecentlyVisited } from "../recentlyVisited";
import type { LauncherCandidate } from "./types";

// An identity detail page (`.../identities/<urn>/...`) registers the person's
// display name — their email when no name is known — as its recent label.
// The identities index itself has no person in its label and is not matched.
const IDENTITY_PAGE_RE = /\/identities\/[^/]+/;

/**
 * Recently visited pages (localStorage) as candidates. Takes the same gating
 * the palette applies today: the read only happens while the palette is open
 * and the user id has resolved, so the shared anonymous key is never read —
 * and until it has resolved there are no candidates at all, so entries read
 * for an earlier user or scope never show under the next one.
 */
export function useRecentCandidates({
  enabled,
  userId,
  orgSlug,
  projectSlug,
}: {
  enabled: boolean;
  userId: string | null;
  orgSlug: string | undefined;
  projectSlug: string | undefined;
}): LauncherCandidate[] {
  const navigate = useNavigate();
  const resolved = enabled && Boolean(userId);
  const recents = useRecentlyVisited(
    userId ?? undefined,
    orgSlug,
    projectSlug,
    resolved,
  );

  return useMemo(
    () =>
      (resolved ? recents : []).map((recent): LauncherCandidate => {
        // Prefer a live name override over a stored URL-derived fallback so
        // id-keyed pages show the resource name once it has loaded.
        const label = getRecentLabelOverride(recent.href) ?? recent.label;
        // A person's name ranks by fuzzy alone, like People: it never leaves
        // the tenant.
        const fuzzyOnly = IDENTITY_PAGE_RE.test(recent.href) || undefined;
        return {
          id: `recent:${recent.href}`,
          kind: "recent",
          title: label,
          detail: "Recently visited",
          keywords: [recent.href],
          verbs: ["open"],
          fuzzyOnly,
          icon: recent.icon as IconName | undefined,
          group: "Recently Visited",
          run: () => {
            void navigate(recent.href);
          },
        };
      }),
    [resolved, recents, navigate],
  );
}
