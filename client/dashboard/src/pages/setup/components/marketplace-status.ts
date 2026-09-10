import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";

/**
 * The marketplace is usable once its URL exists — every set of instructions
 * interpolates that URL. A connection row alone is not enough: one written
 * before marketplace tokens were minted reports connected with a repo and no
 * marketplace URL, and gating on the repo there would call the marketplace
 * published, skip the publish prompt that mints the token, and hand out
 * snippets pointing at an empty URL.
 */
export function isMarketplacePublished(
  status: PublishStatusResult | undefined,
): boolean {
  return !!(status?.connected && status.marketplaceUrl);
}
