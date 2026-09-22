import { unknown, type Fact, type Method } from "./model";

export const accountTypes = ["personal", "team", "enterprise"] as const;
export type AccountType = (typeof accountTypes)[number];
export type AccountFilter = AccountType | "all";
export const accountLabels: Record<AccountType, string> = {
  personal: "Personal",
  team: "Team",
  enterprise: "Enterprise",
};

/** The catalog's team eligibility includes enterprise accounts. Unassessed
 * eligibility must not turn a capability claim into confirmed account support. */
export function accountFact(
  method: Method,
  fact: Fact,
  account: AccountFilter,
): Fact {
  if (account === "all" || fact.status === "na") return fact;
  const plans = method.plans.toLowerCase();
  if (
    plans.includes("needs clarification") ||
    plans.includes("not applicable in source")
  )
    return { ...unknown, note: method.plans, verify: true };
  // CSV imports preserve labelled source cells; the bundled catalog uses prose.
  const labelled = /(?:team plans|personal accounts):/.test(plans);
  let eligible: boolean | undefined;
  if (labelled) {
    const label = account === "personal" ? "personal accounts" : "team plans";
    const claim = plans
      .split(";")
      .find((part) => part.trim().startsWith(`${label}:`));
    if (claim?.includes("enterprise only")) eligible = account === "enterprise";
    else if (claim && /✅|✓/.test(claim)) eligible = true;
    else if (claim && /☠|❌|×|not possible|not supported/.test(claim))
      eligible = false;
  } else {
    const eligibility = plans.split(";")[0] ?? "";
    if (/\b(?:personal|team|enterprise)\b/.test(eligibility)) {
      const accounts = {
        personal: /\bpersonal\b/.test(eligibility),
        team:
          /\bteam\b/.test(eligibility) &&
          !eligibility.includes("enterprise only"),
        enterprise: /\b(?:team|enterprise)\b/.test(eligibility),
      };
      eligible = accounts[account];
    }
  }
  if (eligible === undefined)
    return { ...unknown, note: method.plans, verify: true };
  if (!eligible)
    return {
      status: "impossible",
      note: `${accountLabels[account]} accounts are not eligible for this method`,
      verify: false,
    };
  return fact;
}
