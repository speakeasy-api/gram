import {
  methodAccounts,
  unknown,
  type Accounts,
  type Draft,
  type Fact,
  type Mapping,
  type Method,
} from "./model";

export const accountTypes = ["personal", "team", "enterprise"] as const;
export type AccountType = (typeof accountTypes)[number];
export type AccountFilter = AccountType | "all";
export const accountLabels: Record<AccountType, string> = {
  personal: "Personal",
  team: "Team",
  enterprise: "Enterprise",
};

export const eligibilities = ["supported", "unsupported", "unknown"] as const;
export type AccountEligibility = (typeof eligibilities)[number];
export const eligibilityLabels: Record<AccountEligibility, string> = {
  supported: "Eligible",
  unsupported: "Ineligible",
  unknown: "Unknown",
};

export function isAccountType(value: string): value is AccountType {
  return (accountTypes as readonly string[]).includes(value);
}
export function isEligibility(value: string): value is AccountEligibility {
  return (eligibilities as readonly string[]).includes(value);
}

/** A mapping carries only the account types a platform differs on, so an
 * account type it omits takes the method's answer. */
export function accountEligibility(
  method: Accounts,
  mapping: Accounts | undefined,
  account: AccountType,
): AccountEligibility {
  return mapping?.[account] ?? method[account] ?? "unknown";
}

/** Narrow a capability claim to one account type. Ineligibility is a definite
 * negative; unassessed eligibility must not turn a claim into either answer. */
export function accountFact(
  draft: Draft,
  method: Method,
  mapping: Mapping | undefined,
  fact: Fact,
  account: AccountFilter,
): Fact {
  if (account === "all" || fact.status === "na") return fact;
  const eligibility = accountEligibility(
    methodAccounts(draft, method),
    mapping?.accounts,
    account,
  );
  if (eligibility === "supported") return fact;
  if (eligibility === "unsupported")
    return {
      status: "impossible",
      note: `${accountLabels[account]} accounts are not eligible for ${method.name}`,
      verify: false,
    };
  return {
    ...unknown,
    note: `${accountLabels[account]} eligibility for ${method.name} has not been assessed`,
    verify: true,
  };
}

/** Write one account type's eligibility, leaving the rest of the map alone.
 * Clearing a mapping's entry restores the method's answer for that account. */
export function withEligibility(
  accounts: Accounts,
  account: AccountType,
  value: AccountEligibility | "inherit",
): Accounts {
  const next = { ...accounts };
  if (value === "inherit") delete next[account];
  else next[account] = value;
  return next;
}
