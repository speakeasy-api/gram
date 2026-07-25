/* -------------------------------------------------------------------------- */
/*  Vocabulary                                                                 */
/* -------------------------------------------------------------------------- */

export type BudgetWindow = "daily" | "weekly" | "monthly";
/** Mirrors the security policy actions: flag for review, or block requests. */
export type RuleAction = "flag" | "block";
/** Lifecycle state derived from the worst matched actor, computed server-side. */
export type RuleStatus = "healthy" | "approaching" | "flagging" | "blocking";
export type SpendEventType = "warning" | "breach";
export type RuleTargetOperator =
  | "equals"
  | "not_equals"
  | "starts_with"
  | "ends_with"
  | "contains"
  | "matches"
  | "includes";

export interface RuleTargetCondition {
  attribute: string;
  operator: RuleTargetOperator;
  value: string;
}

/** A member attribute a target condition can be written against. Fetched from
 *  the server (spendrules.listActorAttributes) — the backend CEL environment
 *  is the source of truth — so the editor can never offer an attribute the
 *  server would reject. */
export interface ActorAttribute {
  name: string;
  /** Value kind: a scalar string, or a list of strings (uses `includes`). */
  type: "string" | "list";
  description: string;
}

interface RuleTargetLike {
  attribute: string;
  operator: string;
  value: string;
}

/* -------------------------------------------------------------------------- */
/*  Models                                                                     */
/* -------------------------------------------------------------------------- */

/* App-owned budget view types. They mirror the spend-rules API responses but
 * are defined here, not re-exported from the generated SDK, so a regeneration
 * that reshapes an SDK model surfaces as a type error in the mapping layer
 * (budgets-queries.ts) rather than silently rippling through every component.
 * The SDK→app mapping happens once, at the react-query boundary. */

/** A budget rule as the UI consumes it. One immutable version row: editing a
 *  rule archives the current row and creates a successor with a fresh id. */
export interface SpendRule {
  id: string;
  organizationId: string;
  /** URL-safe identifier, unique per org and immutable; the URN embeds it. */
  slug: string;
  /** Versioned identity, e.g. `spend_rule:eng-monthly-cap:v3`. */
  urn: string;
  /** Position in the slug lineage; every edit increments it. */
  version: number;
  name: string;
  description: string;
  /** Structured actor-directory condition — who the rule covers. */
  target: RuleTargetCondition;
  /** CEL expression selecting matched members (derived from `target`). */
  targetExpr: string;
  /** CEL expression over actor usage identifying a breach. */
  ruleExpr: string;
  /** Per-person budget in USD for one window. */
  limitUsd: number;
  windowKind: BudgetWindow;
  /** Percent of the limit (1–99) at which a warning event fires. */
  warnAtPct: number;
  action: RuleAction;
  enabled: boolean;
  createdAt: Date;
  updatedAt: Date;
}

/** Server-computed current-window usage for one rule (overview endpoint). */
export interface SpendRuleUsage {
  ruleId: string;
  status: RuleStatus;
  /** Aggregate spend across matched people this window. */
  spendUsd: number;
  /** Total budget = per-person limit × matched people. */
  budgetUsd: number;
  matchedUsers: number;
  usersWarned: number;
  usersBreached: number;
  /** Highest per-person utilization, as a percent of the limit. */
  worstUsedPct: number;
}

/** One matched person's current-window spend against their per-person budget. */
export interface SpendRuleActorUsage {
  email: string;
  displayName?: string;
  userId?: string;
  spendUsd: number;
  limitUsd: number;
  usedPct: number;
  breached: boolean;
}

/** A warning or breach recorded when a rule evaluated a person's spend. Pins
 *  the config (rule name, limit) that applied when it fired. */
export interface SpendRuleEvent {
  id: string;
  ruleId: string;
  ruleName: string;
  /** Versioned URN the event fired under — the live rule may have moved on. */
  ruleUrn: string;
  eventType: SpendEventType;
  email: string;
  displayName?: string;
  userId?: string;
  spendUsd: number;
  limitUsd: number;
  windowStart: Date;
  windowEnd: Date;
  createdAt: Date;
}

/** Org-wide rollup shown as summary cards above the rules table. */
export interface SpendRulesOverviewResult {
  rules: SpendRuleUsage[];
  rulesTotal: number;
  rulesUnhealthy: number;
  totalSpendUsd: number;
  totalBudgetUsd: number;
  spendOverBudgetUsd: number;
  usersTotal: number;
  usersBreached: number;
}

/** Result of previewing a rule's target: which people it matches and their
 *  current spend against the proposed budget. */
export interface PreviewSpendRuleResult {
  actors: SpendRuleActorUsage[];
  matchedCount: number;
  windowStart: Date;
  windowEnd: Date;
}

export const WINDOW_LABELS: Record<BudgetWindow, string> = {
  daily: "Daily",
  weekly: "Weekly",
  monthly: "Monthly",
};

export const RULE_ACTION_LABELS: Record<RuleAction, string> = {
  flag: "Flag",
  block: "Block",
};

export const RULE_STATUS_LABELS: Record<RuleStatus, string> = {
  healthy: "Healthy",
  approaching: "Approaching",
  // Both breached states read "Over budget": the status describes the state,
  // and the Action badge rendered alongside already says what happens about
  // it — "Blocking" next to a BLOCK badge was saying the same thing twice.
  flagging: "Over budget",
  blocking: "Over budget",
};

export const SPEND_EVENT_TYPE_LABELS: Record<SpendEventType, string> = {
  // Each rule sets its own warn threshold; the event records the numbers
  // that applied when it fired.
  warning: "Threshold warning",
  breach: "Budget breached",
};

/** Form shape for creating or editing a rule. Server-generated fields
 *  (id, urn, version, timestamps) are never edited. */
export interface RuleDraft {
  name: string;
  description: string;
  /** Structured actor directory attribute condition — who the rule covers. */
  target: RuleTargetCondition;
  /** Per-person budget in USD for one window. */
  limitUsd: number;
  /** Fixed UTC calendar window: resets at midnight / Monday / the 1st. */
  windowKind: BudgetWindow;
  /** Percent of the budget (1–99) at which a warning event fires. */
  warnAtPct: number;
  action: RuleAction;
  enabled: boolean;
}

export function defaultRuleDraft(): RuleDraft {
  return {
    name: "",
    description: "",
    target: {
      attribute: "department_name",
      operator: "equals",
      value: "",
    },
    limitUsd: 1000,
    windowKind: "monthly",
    warnAtPct: 80,
    action: "block",
    enabled: true,
  };
}

export function toDraft(rule: SpendRule): RuleDraft {
  return {
    name: rule.name,
    description: rule.description,
    target: normalizeTargetCondition(rule.target),
    limitUsd: rule.limitUsd,
    windowKind: rule.windowKind,
    warnAtPct: rule.warnAtPct,
    action: rule.action,
    enabled: rule.enabled,
  };
}

/* -------------------------------------------------------------------------- */
/*  URNs                                                                       */
/* -------------------------------------------------------------------------- */

/** Events record the versioned URN (`spend_rule:<slug>:v<version>`) they
 *  fired under, which pins the exact config that produced them — the live
 *  rule may have moved on to a newer version since. The slug is unique per
 *  org and immutable after creation. */
export function parseRuleUrn(
  urn: string,
): { slug: string; version: number } | null {
  const match = /^spend_rule:([a-z0-9_-]+):v(\d+)$/.exec(urn);
  if (!match) return null;
  return { slug: match[1]!, version: Number(match[2]!) };
}

/* -------------------------------------------------------------------------- */
/*  Derived display helpers                                                    */
/* -------------------------------------------------------------------------- */

/** Server-computed usage keyed by rule id, from the overview endpoint. */
export function usageByRuleId(
  usages: SpendRuleUsage[] | undefined,
): Map<string, SpendRuleUsage> {
  const map = new Map<string, SpendRuleUsage>();
  for (const usage of usages ?? []) map.set(usage.ruleId, usage);
  return map;
}

export function ruleStatusOf(
  rule: Pick<SpendRule, "enabled">,
  usage: SpendRuleUsage | undefined,
): RuleStatus | null {
  if (!rule.enabled || !usage) return null;
  return usage.status;
}

/** Human countdown until the rule's fixed UTC window resets, e.g. "27d 5h". */
export function timeUntilWindowReset(windowKind: BudgetWindow): string {
  const now = new Date();
  let next: Date;
  switch (windowKind) {
    case "daily":
      next = new Date(
        Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1),
      );
      break;
    case "weekly": {
      const day = (now.getUTCDay() + 6) % 7; // Monday = 0
      next = new Date(
        Date.UTC(
          now.getUTCFullYear(),
          now.getUTCMonth(),
          now.getUTCDate() + (7 - day),
        ),
      );
      break;
    }
    case "monthly":
      next = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + 1, 1));
      break;
  }
  const hours = Math.max(
    1,
    Math.round((next.getTime() - now.getTime()) / 3_600_000),
  );
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  const remainder = hours % 24;
  return remainder > 0 ? `${days}d ${remainder}h` : `${days}d`;
}

/** All events for a rule, across every version, most recent first. */
export function sortEventsByRecency(
  events: SpendRuleEvent[],
): SpendRuleEvent[] {
  return [...events].sort(
    (a, b) => b.createdAt.getTime() - a.createdAt.getTime(),
  );
}

/* -------------------------------------------------------------------------- */
/*  Formatting helpers                                                         */
/* -------------------------------------------------------------------------- */

export function formatUsd(amount: number): string {
  // Always keep cents: enforcement uses the exact double-precision limit/spend,
  // so rounding larger amounts to whole dollars here would show a figure the
  // evaluator never used.
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: 2,
  }).format(amount);
}

export function targetSummary(target: RuleTargetLike): string {
  return `${targetAttributeLabel(target.attribute)} ${operatorSummary(target.operator)} ${target.value}`;
}

/** Title-case an attribute name for display, e.g. department_name → Department
 *  Name. Kept local so summaries render without the fetched attribute catalog. */
function targetAttributeLabel(name: string): string {
  return name
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export function normalizeTargetCondition(
  target: RuleTargetLike,
): RuleTargetCondition {
  return {
    attribute: target.attribute,
    operator: isRuleTargetOperator(target.operator)
      ? target.operator
      : "contains",
    value: target.value,
  };
}

function isRuleTargetOperator(
  operator: string,
): operator is RuleTargetOperator {
  return (
    operator === "equals" ||
    operator === "not_equals" ||
    operator === "starts_with" ||
    operator === "ends_with" ||
    operator === "contains" ||
    operator === "matches" ||
    operator === "includes"
  );
}

function operatorSummary(operator: string): string {
  switch (operator) {
    case "equals":
      return "is";
    case "not_equals":
      return "is not";
    case "starts_with":
      return "starts with";
    case "ends_with":
      return "ends with";
    case "matches":
      return "matches";
    case "includes":
      return "includes";
    default:
      return "contains";
  }
}
