/**
 * The anchor the spend cap section carries on the billing page, so the banner
 * that reports a paused organization can link straight at the field that ends
 * the pause.
 */
export const CHAT_SPEND_CAP_ANCHOR = "chat-spend-cap-section";

/**
 * Whether chat is paused because this month's spend reached the cap.
 *
 * A cap of zero is "no cap configured", not "nothing may be spent" — the usage
 * endpoint reports it for organizations that never set one, and treating it as
 * reached would pause every one of them.
 *
 * The comparison is inclusive because chat stops *at* the cap rather than past
 * it, so the boundary itself is already the paused state.
 */
export function isChatSpendCapReached(
  usage: { monthlyCredits: number; creditsUsed: number } | undefined,
): boolean {
  if (usage === undefined) return false;
  return usage.monthlyCredits > 0 && usage.creditsUsed >= usage.monthlyCredits;
}
