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

/**
 * Why a card's platform instructions are held back. Every set references the
 * published marketplace. Org rollouts that read the repo through GitHub
 * (Cowork's GitHub App, Cursor's team marketplace import) also need a
 * collaborator: a repo nobody has been given access to is as unusable there as
 * one that was never published. A missing flag means the collaborator lookup
 * failed, not that there are none, so only a definite "no" holds them back.
 */
export function marketplaceHoldBacks(status: PublishStatusResult | undefined): {
  heldBack: string | undefined;
  orgHeldBack: string | undefined;
} {
  return {
    heldBack: isMarketplacePublished(status)
      ? undefined
      : "Publish the marketplace above first — these instructions reference it.",
    orgHeldBack:
      status?.hasCollaborators === false
        ? "Add a collaborator to the marketplace repo above first — the organization rollout cannot sync a repo nobody has access to."
        : undefined,
  };
}
