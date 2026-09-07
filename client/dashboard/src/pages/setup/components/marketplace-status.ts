import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";

/** The marketplace is usable once a GitHub repo has actually been published. */
export function isMarketplacePublished(
  status: PublishStatusResult | undefined,
): boolean {
  return !!(status?.connected && status.repoUrl);
}
