import type { AdminRegistryPromotionCandidate } from "@gram/admin-client/models/components/adminregistrypromotioncandidate";

const GUIDES_TREE_URL =
  "https://github.com/speakeasy-api/mcp-setup-docs/tree/main/guides/";

export type SlugGuideHint =
  | { kind: "existing"; title: string; url: string }
  | { kind: "endpoint"; title: string; url: string }
  | { kind: "none"; version: string };

/**
 * Describes how a setup documentation slug relates to the guides pinned in
 * the server. Matches exact slugs only. Returns null when the candidate
 * carries no guide list or the slug is empty.
 */
export function slugGuideHint(
  candidate: Pick<
    AdminRegistryPromotionCandidate,
    "setupGuides" | "setupGuidesVersion" | "endpointGuideSlug"
  >,
  slug: string,
): SlugGuideHint | null {
  if (!candidate.setupGuides || slug === "") return null;
  const guide = candidate.setupGuides.find((entry) => entry.slug === slug);
  if (!guide)
    return { kind: "none", version: candidate.setupGuidesVersion ?? "" };
  const url = GUIDES_TREE_URL + encodeURIComponent(guide.slug);
  return {
    kind: guide.slug === candidate.endpointGuideSlug ? "endpoint" : "existing",
    title: guide.title,
    url,
  };
}
