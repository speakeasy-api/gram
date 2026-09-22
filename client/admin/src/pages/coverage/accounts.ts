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
  conditions = "",
): Fact {
  if (account === "all" || fact.status === "na") return fact;
  const accountPattern =
    account === "personal"
      ? /personal accounts?:/i
      : /(?:team plans?|enterprise(?: accounts?| plans?)?):/i;
  const source = accountPattern.test(conditions) ? conditions : method.plans;
  const plans = source.toLowerCase();
  if (
    plans.includes("needs clarification") ||
    plans.includes("not applicable in source")
  )
    return { ...unknown, note: source, verify: true };
  // CSV imports preserve labelled source cells; the bundled catalog uses prose.
  const labelled =
    /(?:team plans?|personal accounts?|enterprise(?: accounts?| plans?)?):/.test(
      plans,
    );
  let eligible: boolean | undefined;
  if (labelled) {
    const claims = plans.split(/[;\n]/);
    const patterns = {
      personal: /^personal accounts?:/,
      team: /^team plans?:/,
      enterprise: /^enterprise(?: accounts?| plans?)?:/,
    };
    let claim = claims.find((part) => patterns[account].test(part.trim()));
    if (!claim && account === "enterprise")
      claim = claims.find((part) => patterns.team.test(part.trim()));
    const value = claim?.split(":").slice(1).join(":").trim() ?? "";
    if (value.includes("enterprise only")) eligible = account === "enterprise";
    else if (
      /☠|❌|×|not possible|not supported|unsupported|^no$|^false$/.test(value)
    )
      eligible = false;
    else if (/✅|✓|^supported$|^yes$|^true$/.test(value)) eligible = true;
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
  if (eligible === undefined) return { ...unknown, note: source, verify: true };
  if (!eligible)
    return {
      status: "impossible",
      note: `${accountLabels[account]} accounts are not eligible for this method`,
      verify: false,
    };
  return fact;
}
