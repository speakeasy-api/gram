import type { QueryClient } from "@tanstack/react-query";
import {
  invalidateAllSpendRulesListEvents,
  invalidateAllSpendRulesListRules,
  invalidateAllSpendRulesOverview,
} from "@gram/client/react-query/index.js";
import type { SpendRule } from "@gram/client/models/components/spendrule.js";
import type { SpendRuleEvent } from "@gram/client/models/components/spendruleevent.js";
import type { SpendRuleUsage } from "@gram/client/models/components/spendruleusage.js";
import { ACTOR_ATTRIBUTES } from "./budget-cel";

export type {
  PreviewSpendRuleResult,
  SpendRule,
  SpendRuleActorUsage,
  SpendRuleEvent,
  SpendRulesOverviewResult,
  SpendRuleUsage,
} from "@gram/client/models/components/index.js";

/* -------------------------------------------------------------------------- */
/*  Vocabulary                                                                 */
/* -------------------------------------------------------------------------- */

export type BudgetWindow = "daily" | "weekly" | "monthly";
/** Mirrors the security policy actions: flag for review, or block requests. */
export type RuleAction = "flag" | "block";
/** Lifecycle state derived from the worst matched actor, computed server-side. */
export type RuleStatus = "healthy" | "approaching" | "flagging" | "blocking";
export type SpendEventType = "warning" | "breach";

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
  flagging: "Flagging",
  blocking: "Blocking",
};

export const SPEND_EVENT_TYPE_LABELS: Record<SpendEventType, string> = {
  // Each rule sets its own warn threshold; the event records the numbers
  // that applied when it fired.
  warning: "Threshold warning",
  breach: "Budget breached",
};

/** Form shape for creating or editing a rule. Server-generated fields
 *  (id, urn, version, evaluatedFrom, timestamps) are never edited. */
export interface RuleDraft {
  name: string;
  description: string;
  /** CEL comparison over actor directory attributes — who the rule covers. */
  targetExpr: string;
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
    targetExpr: 'department_name == "Engineering"',
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
    targetExpr: rule.targetExpr,
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

/** Events record the versioned URN (`spend_rule:<slug>:<version>`) they
 *  fired under, which pins the exact config that produced them — the live
 *  rule may have moved on to a newer version since. The slug is unique per
 *  org and immutable after creation. */
export function parseRuleUrn(
  urn: string,
): { slug: string; version: number } | null {
  const match = /^spend_rule:([a-z0-9_-]+):(\d+)$/.exec(urn);
  if (!match) return null;
  return { slug: match[1]!, version: Number(match[2]!) };
}

/* -------------------------------------------------------------------------- */
/*  Query invalidation                                                         */
/* -------------------------------------------------------------------------- */

/** Rules, overview, and events all describe the same state; a rule mutation
 *  refreshes every spend-control query so no tab shows stale numbers. */
export function invalidateSpendControlQueries(client: QueryClient): void {
  void invalidateAllSpendRulesListRules(client);
  void invalidateAllSpendRulesOverview(client);
  void invalidateAllSpendRulesListEvents(client);
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
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: amount >= 100 ? 0 : 2,
  }).format(amount);
}

export function targetSummary(expr: string): string {
  const parts = expr
    .split(/\s+&&\s+/)
    .map((part) => summarizeTargetPart(part.trim()))
    .filter((summary) => summary !== null);
  if (parts.length === 0) return "Custom attribute conditions";
  return parts.join(" and ");
}

function summarizeTargetPart(part: string): string | null {
  const comparison = /^([A-Za-z_]\w*)\s*(==|!=)\s*"((?:\\.|[^"])*)"$/.exec(
    part,
  );
  if (comparison) {
    const label = targetAttributeLabel(comparison[1]!);
    const op = comparison[2] === "==" ? "is" : "is not";
    return `${label} ${op} ${unescapeSummaryValue(comparison[3]!)}`;
  }

  const listMembership = /^"((?:\\.|[^"])*)"\s+in\s+([A-Za-z_]\w*)$/.exec(part);
  if (listMembership) {
    return `${targetAttributeLabel(listMembership[2]!)} includes ${unescapeSummaryValue(listMembership[1]!)}`;
  }

  const call =
    /^([A-Za-z_]\w*)\.(startsWith|endsWith|contains|matches)\("((?:\\.|[^"])*)"\)$/.exec(
      part,
    );
  if (call) {
    return `${targetAttributeLabel(call[1]!)} ${methodSummary(call[2]!)} ${unescapeSummaryValue(call[3]!)}`;
  }

  return null;
}

function targetAttributeLabel(name: string): string {
  const attr = ACTOR_ATTRIBUTES.find((a) => a.name === name);
  return (attr?.name ?? name)
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function methodSummary(method: string): string {
  switch (method) {
    case "startsWith":
      return "starts with";
    case "endsWith":
      return "ends with";
    case "matches":
      return "matches";
    default:
      return "contains";
  }
}

function unescapeSummaryValue(value: string): string {
  return value.replace(/\\"/g, '"').replace(/\\\\/g, "\\");
}
