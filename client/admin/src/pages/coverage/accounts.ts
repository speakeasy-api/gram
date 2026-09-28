import {
  unknown,
  type Eligibility,
  type Fact,
  type Method,
  type PlatformSupport,
} from "./model";

export const accountTypes = ["personal", "team", "enterprise"] as const;
export type AccountType = (typeof accountTypes)[number];
export type AccountFilter = AccountType | "all";
export const accountLabels: Record<AccountType, string> = {
  personal: "Personal",
  team: "Team",
  enterprise: "Enterprise",
};

function narrow(fact: Fact, eligibility: Eligibility, account: AccountFilter) {
  if (account === "all" || fact.status === "na") return fact;
  if (eligibility === "unsupported")
    return {
      status: "impossible" as const,
      note: `${accountLabels[account]} accounts are not eligible for this method`,
      verify: false,
    };
  if (eligibility === "unknown")
    return {
      ...unknown,
      note: "Account eligibility is not known",
      verify: true,
    };
  return fact;
}

/** Narrow a cell to one account type: an ineligible account cannot have the
 * capability, and unknown eligibility leaves it unknown and to be verified. */
export function accountFact(
  support: PlatformSupport | undefined,
  fact: Fact,
  account: AccountFilter,
): Fact {
  const eligibility =
    account === "all" || !support ? "unknown" : support.accounts[account];
  return narrow(fact, eligibility, account);
}

/** A method's eligibility across its platforms: eligible where any is. */
export function methodEligibility(
  method: Method,
  account: AccountType,
): Eligibility {
  const all = method.platforms.map((support) => support.accounts[account]);
  if (all.includes("supported")) return "supported";
  if (all.includes("unknown")) return "unknown";
  return "unsupported";
}

/** Narrow a method's claim, platform aside, to one account type. */
export function methodAccountFact(
  method: Method,
  fact: Fact,
  account: AccountFilter,
): Fact {
  const eligibility =
    account === "all" ? "unknown" : methodEligibility(method, account);
  return narrow(fact, eligibility, account);
}
