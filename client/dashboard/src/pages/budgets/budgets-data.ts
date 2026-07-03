import { useSyncExternalStore } from "react";
import type { ActorRecord } from "./budget-cel";
import { ACTOR_ATTRIBUTES, matchActors } from "./budget-cel";

/* -------------------------------------------------------------------------- */
/*  Actors (mocked directory-synced users)                                     */
/* -------------------------------------------------------------------------- */

export interface MockActor extends ActorRecord {
  id: string;
  name: string;
  email: string;
  department_name: string;
  job_title: string;
  employee_type: string;
  division_name: string;
  cost_center_name: string;
  groups: string[];
  roles: string[];
  /** Mocked trailing-30-day AI spend, in USD, used to estimate rule usage. */
  monthlySpendUsd: number;
}

const MOCK_ACTORS: MockActor[] = [
  {
    id: "u_ada",
    name: "Ada Okafor",
    email: "ada@acme.com",
    department_name: "Engineering",
    job_title: "Staff Engineer",
    employee_type: "full_time",
    division_name: "R&D",
    cost_center_name: "CC-1001",
    groups: ["eng-frontier", "leadership"],
    roles: ["admin"],
    monthlySpendUsd: 1840,
  },
  {
    id: "u_grace",
    name: "Grace Lindqvist",
    email: "grace@acme.com",
    department_name: "Engineering",
    job_title: "Software Engineer",
    employee_type: "full_time",
    division_name: "R&D",
    cost_center_name: "CC-1001",
    groups: ["eng-frontier"],
    roles: ["member"],
    monthlySpendUsd: 1210,
  },
  {
    id: "u_kenji",
    name: "Kenji Watanabe",
    email: "kenji@acme.com",
    department_name: "Data Science",
    job_title: "ML Engineer",
    employee_type: "full_time",
    division_name: "R&D",
    cost_center_name: "CC-2043",
    groups: ["ml-team"],
    roles: ["member"],
    monthlySpendUsd: 3120,
  },
  {
    id: "u_lena",
    name: "Lena Fischer",
    email: "lena@acme.com",
    department_name: "Data Science",
    job_title: "Manager",
    employee_type: "full_time",
    division_name: "R&D",
    cost_center_name: "CC-2043",
    groups: ["ml-team", "leadership"],
    roles: ["admin"],
    monthlySpendUsd: 640,
  },
  {
    id: "u_theo",
    name: "Theo Marsh",
    email: "theo@acme.com",
    department_name: "Design",
    job_title: "Product Designer",
    employee_type: "full_time",
    division_name: "Go-To-Market",
    cost_center_name: "CC-3100",
    groups: ["design"],
    roles: ["member"],
    monthlySpendUsd: 380,
  },
  {
    id: "u_priya",
    name: "Priya Nair",
    email: "priya@acme.com",
    department_name: "Support",
    job_title: "Support Engineer",
    employee_type: "full_time",
    division_name: "Go-To-Market",
    cost_center_name: "CC-3100",
    groups: ["support"],
    roles: ["member"],
    monthlySpendUsd: 260,
  },
  {
    id: "u_sam",
    name: "Sam Rivera",
    email: "sam@acme.com",
    department_name: "Engineering",
    job_title: "Software Engineer",
    employee_type: "intern",
    division_name: "R&D",
    cost_center_name: "CC-1001",
    groups: ["interns"],
    roles: ["viewer"],
    monthlySpendUsd: 95,
  },
  {
    id: "u_mira",
    name: "Mira Haddad",
    email: "mira@acme.com",
    department_name: "Data Science",
    job_title: "Analyst",
    employee_type: "intern",
    division_name: "R&D",
    cost_center_name: "CC-2043",
    groups: ["interns", "ml-team"],
    roles: ["viewer"],
    monthlySpendUsd: 150,
  },
  {
    id: "u_omar",
    name: "Omar Vasquez",
    email: "omar@contractor.dev",
    department_name: "Engineering",
    job_title: "Software Engineer",
    employee_type: "contractor",
    division_name: "Platform",
    cost_center_name: "CC-1001",
    groups: ["eng-frontier"],
    roles: ["member"],
    monthlySpendUsd: 720,
  },
  {
    id: "u_bea",
    name: "Bea Costa",
    email: "bea@acme.com",
    department_name: "Finance",
    job_title: "Manager",
    employee_type: "full_time",
    division_name: "Go-To-Market",
    cost_center_name: "CC-3100",
    groups: ["leadership"],
    roles: ["admin"],
    monthlySpendUsd: 210,
  },
];

/** Everyone in the mock directory — the denominator for people rollups. */
export const DIRECTORY_USER_COUNT = MOCK_ACTORS.length;

/* -------------------------------------------------------------------------- */
/*  Rules (v1: one condition, fixed windows, flag-or-block)                    */
/* -------------------------------------------------------------------------- */

export type BudgetWindow = "daily" | "weekly" | "monthly";
/** Mirrors the security policy actions: flag for review, or block requests. */
export type RuleAction = "flag" | "block";

export interface SpendRule {
  id: string;
  name: string;
  description: string;
  /** Single CEL comparison over actor attributes — who the rule applies to. */
  targetExpr: string;
  limitUsd: number;
  /** Fixed calendar window: resets at midnight / Monday / the 1st. */
  window: BudgetWindow;
  /** Percent of the budget (1–99) at which the rule turns Approaching and a
   *  threshold-warning event fires. */
  warnAtPct: number;
  action: RuleAction;
  /** Whether the rule is on. Disabled rules are ignored at request time. */
  enabled: boolean;
  /** Monotonic config version. Material edits bump it — see MATERIAL_FIELDS. */
  version: number;
  /** When evaluation last (re)started: creation or the last material edit.
   *  Spend before this instant does not count against the rule. */
  evaluatedFrom: string;
  createdAt: string;
}

export type RuleDraft = Omit<
  SpendRule,
  "id" | "createdAt" | "version" | "evaluatedFrom"
>;

/** Versioned identity for a rule, following the platform URN shape
 *  (`tools:http:petstore:getUser`). Events record the URN they fired under,
 *  which pins the exact config that produced them — the live rule may have
 *  moved on to a newer version since. */
export function ruleUrn(rule: Pick<SpendRule, "id" | "version">): string {
  return `spend_rules:${rule.id}:v${rule.version}`;
}

export function parseRuleUrn(
  urn: string,
): { id: string; version: number } | null {
  const match = /^spend_rules:(.+):v(\d+)$/.exec(urn);
  if (!match) return null;
  return { id: match[1]!, version: Number(match[2]!) };
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

let nextId = 1;
function makeId(prefix: string): string {
  return `${prefix}_${Date.now().toString(36)}_${nextId++}`;
}

const SEED_RULES: SpendRule[] = [
  {
    id: "rule_seed_eng",
    name: "Engineering frontier cap",
    description:
      "Engineering gets a generous monthly budget. Overspend is flagged for review.",
    targetExpr: 'department_name == "Engineering"',
    limitUsd: 5000,
    window: "monthly",
    warnAtPct: 80,
    action: "flag",
    enabled: true,
    // v1 capped at $4,000 and breached in June; the limit was raised to
    // $5,000 on Jun 20, which restarted evaluation as v2. The June breach
    // event still references spend_rules:rule_seed_eng:v1.
    version: 2,
    evaluatedFrom: "2026-06-20T15:00:00.000Z",
    createdAt: "2026-06-01T00:00:00.000Z",
  },
  {
    id: "rule_seed_interns",
    name: "Intern hard limit",
    description:
      "Interns are capped at a low monthly budget and blocked once they hit it.",
    targetExpr: 'employee_type == "intern"',
    limitUsd: 200,
    window: "monthly",
    warnAtPct: 80,
    action: "block",
    enabled: true,
    version: 1,
    evaluatedFrom: "2026-06-05T00:00:00.000Z",
    createdAt: "2026-06-05T00:00:00.000Z",
  },
  {
    id: "rule_seed_ml",
    name: "ML team watch",
    description:
      "Watch the data science team's spend and flag overspend for review.",
    targetExpr: '"ml-team" in groups',
    limitUsd: 3500,
    window: "monthly",
    warnAtPct: 80,
    action: "flag",
    enabled: true,
    version: 1,
    evaluatedFrom: "2026-06-10T00:00:00.000Z",
    createdAt: "2026-06-10T00:00:00.000Z",
  },
  {
    id: "rule_seed_contractors",
    name: "Contractor weekly guardrail",
    description:
      "Contractors get a weekly budget to keep short engagements predictable.",
    targetExpr: 'employee_type == "contractor"',
    limitUsd: 400,
    window: "weekly",
    // Small weekly budgets burn fast — warn earlier than the default.
    warnAtPct: 50,
    action: "block",
    enabled: false,
    version: 1,
    evaluatedFrom: "2026-06-14T00:00:00.000Z",
    createdAt: "2026-06-14T00:00:00.000Z",
  },
];

/* -------------------------------------------------------------------------- */
/*  Rule status (lifecycle state derived from usage)                           */
/* -------------------------------------------------------------------------- */

export type RuleStatus = "healthy" | "approaching" | "flagging" | "blocking";

export const RULE_STATUS_LABELS: Record<RuleStatus, string> = {
  healthy: "Healthy",
  approaching: "Approaching",
  flagging: "Flagging",
  blocking: "Blocking",
};

/**
 * Current lifecycle state of a rule: healthy → approaching (past the rule's
 * warn threshold) → flagging/blocking (over limit, depending on the rule's
 * action). Null when the rule is disabled or its target can't be evaluated.
 */
export function ruleStatus(
  rule: Pick<SpendRule, "action" | "enabled" | "warnAtPct">,
  usage: RuleUsage,
): RuleStatus | null {
  if (!rule.enabled || usage.matched === null) return null;
  if (usage.utilization >= 1) {
    return rule.action === "block" ? "blocking" : "flagging";
  }
  if (usage.utilization >= rule.warnAtPct / 100) return "approaching";
  return "healthy";
}

/** Human countdown until the rule's fixed window resets, e.g. "27d 5h". */
export function timeUntilWindowReset(window: BudgetWindow): string {
  const now = new Date();
  let next: Date;
  switch (window) {
    case "daily":
      next = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1);
      break;
    case "weekly": {
      const day = (now.getDay() + 6) % 7; // Monday = 0
      next = new Date(
        now.getFullYear(),
        now.getMonth(),
        now.getDate() + (7 - day),
      );
      break;
    }
    case "monthly":
      next = new Date(now.getFullYear(), now.getMonth() + 1, 1);
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

/* -------------------------------------------------------------------------- */
/*  Events (budget lifecycle transitions, not per-request noise)               */
/* -------------------------------------------------------------------------- */

/** Two event kinds for v1: a rule passed its warn threshold, or it breached
 *  its budget. A breach on a block rule also means requests are now rejected —
 *  that consequence lives in the breach summary rather than a separate event.
 *  Rule config changes are not budget events; they go to the standard audit
 *  log. */
export type SpendEventType = "warning" | "breach";

export const SPEND_EVENT_TYPE_LABELS: Record<SpendEventType, string> = {
  // Each rule sets its own warn threshold; the event summary records the
  // percentage that applied when the event fired.
  warning: "Threshold warning",
  breach: "Budget breached",
};

export interface SpendControlEvent {
  id: string;
  occurredAt: string;
  type: SpendEventType;
  /** Versioned URN of the rule config the event fired under. The live rule
   *  may be newer (or deleted) — the URN pins how to read the numbers here. */
  ruleUrn: string;
  /** Rule name at the time of the event. Survives renames and deletes. */
  ruleName: string;
  /** One-line human explanation of what happened and what it means. */
  summary: string;
  spendUsd?: number;
  limitUsd?: number;
}

/** Seeded history. Note the Engineering arc: v1 (capped at $4,000) breached
 *  in June, then the limit was raised to $5,000 — bumping the rule to v2 and
 *  restarting evaluation — and the July warning fired under v2. The v1 breach
 *  stays interpretable via its URN even though the live config moved on. */
const SEED_EVENTS: SpendControlEvent[] = [
  {
    id: "evt_b_001",
    occurredAt: "2026-07-03T13:47:00.000Z",
    type: "breach",
    ruleUrn: "spend_rules:rule_seed_ml:v1",
    ruleName: "ML team watch",
    summary:
      "Data Science crossed its $3,500 monthly budget. Requests still flow — overspend is flagged for review.",
    spendUsd: 3540,
    limitUsd: 3500,
  },
  {
    id: "evt_b_003",
    occurredAt: "2026-07-02T21:38:00.000Z",
    type: "breach",
    ruleUrn: "spend_rules:rule_seed_interns:v1",
    ruleName: "Intern hard limit",
    summary:
      "Interns crossed their $200 monthly budget two days into the window. Requests from 2 matched people are rejected until the window resets on Aug 1.",
    spendUsd: 204,
    limitUsd: 200,
  },
  {
    id: "evt_b_004",
    occurredAt: "2026-07-02T16:05:00.000Z",
    type: "warning",
    ruleUrn: "spend_rules:rule_seed_eng:v2",
    ruleName: "Engineering frontier cap",
    summary:
      "Engineering passed its 80% warn threshold with 28 days left in the window.",
    spendUsd: 4020,
    limitUsd: 5000,
  },
  {
    id: "evt_b_005",
    occurredAt: "2026-07-01T23:30:00.000Z",
    type: "warning",
    ruleUrn: "spend_rules:rule_seed_ml:v1",
    ruleName: "ML team watch",
    summary: "Data Science passed its 80% warn threshold.",
    spendUsd: 2840,
    limitUsd: 3500,
  },
  {
    id: "evt_b_006",
    occurredAt: "2026-07-01T20:10:00.000Z",
    type: "warning",
    ruleUrn: "spend_rules:rule_seed_interns:v1",
    ruleName: "Intern hard limit",
    summary:
      "Interns passed their 80% warn threshold within the first day of the window.",
    spendUsd: 163,
    limitUsd: 200,
  },
  {
    id: "evt_b_009",
    occurredAt: "2026-06-18T09:12:00.000Z",
    type: "breach",
    ruleUrn: "spend_rules:rule_seed_eng:v1",
    ruleName: "Engineering frontier cap",
    summary:
      "Engineering crossed its $4,000 monthly budget. Requests still flow — overspend is flagged for review.",
    spendUsd: 4085,
    limitUsd: 4000,
  },
];

/* -------------------------------------------------------------------------- */
/*  In-memory store (prototype only — no persistence, resets on reload)        */
/* -------------------------------------------------------------------------- */

interface BudgetSnapshot {
  rules: SpendRule[];
  events: SpendControlEvent[];
}

let rules: SpendRule[] = [...SEED_RULES];
let events: SpendControlEvent[] = [...SEED_EVENTS];
let snapshot: BudgetSnapshot = { rules, events };
const listeners = new Set<() => void>();

function emit(): void {
  snapshot = { rules: [...rules], events: [...events] };
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export interface BudgetStore {
  rules: SpendRule[];
  events: SpendControlEvent[];
  addRule: (draft: RuleDraft) => SpendRule;
  updateRule: (id: string, patch: Partial<RuleDraft>) => void;
  removeRule: (id: string) => void;
}

/** Fields that change what the rule evaluates. Editing any of them bumps the
 *  version and restarts evaluation from scratch — accumulated window state is
 *  dropped, and the old version lives on only in the events it produced.
 *  Renames, description tweaks, and enable/disable keep state. The change
 *  itself is recorded in the standard audit log, not as a budget event. */
const MATERIAL_FIELDS = [
  "targetExpr",
  "limitUsd",
  "window",
  "warnAtPct",
  "action",
] as const;

export function useBudgetStore(): BudgetStore {
  const snap = useSyncExternalStore(
    subscribe,
    () => snapshot,
    () => snapshot,
  );

  return {
    rules: snap.rules,
    events: snap.events,
    addRule: (draft) => {
      const rule: SpendRule = {
        ...draft,
        id: makeId("rule"),
        version: 1,
        evaluatedFrom: new Date().toISOString(),
        createdAt: new Date().toISOString(),
      };
      rules.push(rule);
      emit();
      return rule;
    },
    updateRule: (id, patch) => {
      rules = rules.map((r) => {
        if (r.id !== id) return r;
        const next = { ...r, ...patch };
        const material = MATERIAL_FIELDS.some(
          (field) => next[field] !== r[field],
        );
        if (!material) return next;
        return {
          ...next,
          version: r.version + 1,
          evaluatedFrom: new Date().toISOString(),
        };
      });
      emit();
    },
    removeRule: (id) => {
      // Events of a removed rule stay — their URN keeps them interpretable.
      rules = rules.filter((r) => r.id !== id);
      emit();
    },
  };
}

/* -------------------------------------------------------------------------- */
/*  Spend estimation                                                           */
/* -------------------------------------------------------------------------- */

function windowStart(window: BudgetWindow, now: Date): Date {
  switch (window) {
    case "daily":
      return new Date(now.getFullYear(), now.getMonth(), now.getDate());
    case "weekly": {
      const day = (now.getDay() + 6) % 7; // Monday = 0
      return new Date(now.getFullYear(), now.getMonth(), now.getDate() - day);
    }
    case "monthly":
      return new Date(now.getFullYear(), now.getMonth(), 1);
  }
}

function windowLengthMs(window: BudgetWindow, now: Date): number {
  switch (window) {
    case "daily":
      return 24 * 3_600_000;
    case "weekly":
      return 7 * 24 * 3_600_000;
    case "monthly": {
      const daysInMonth = new Date(
        now.getFullYear(),
        now.getMonth() + 1,
        0,
      ).getDate();
      return daysInMonth * 24 * 3_600_000;
    }
  }
}

/** Fractions of the current window the rule evaluates over. A rule (re)starts
 *  evaluating at `evaluatedFrom` — spend before that instant never counts, so
 *  a rule edited mid-window accumulates from the edit, not the window start.
 *  `elapsed` covers evaluation start → now; `total` covers evaluation start →
 *  window end (what a full-pace projection can reach). */
function evaluationFractions(
  window: BudgetWindow,
  evaluatedFrom?: string,
): { elapsed: number; total: number } {
  const now = new Date();
  const start = windowStart(window, now);
  const lengthMs = windowLengthMs(window, now);
  const from = evaluatedFrom ? new Date(evaluatedFrom) : start;
  const effective = from > start ? from : start;
  const clamp = (fraction: number) => Math.min(1, Math.max(0, fraction));
  return {
    elapsed: clamp((now.getTime() - effective.getTime()) / lengthMs),
    total: clamp((start.getTime() + lengthMs - effective.getTime()) / lengthMs),
  };
}

const WINDOW_FACTOR: Record<BudgetWindow, number> = {
  daily: 1 / 30,
  weekly: 7 / 30,
  monthly: 1,
};

/** Authored current-window spend for the seed rules so the prototype demos
 *  every state (approaching, flagging, blocking) regardless of today's date.
 *  Keyed by versioned URN: a material edit bumps the version, the lookup
 *  misses, and the rule starts evaluating from scratch. */
const SEEDED_WINDOW_SPEND: Record<string, number> = {
  "spend_rules:rule_seed_eng:v2": 4180, // 83.6% of $5,000 → approaching
  "spend_rules:rule_seed_interns:v1": 212, // 106% of $200, block → blocking
  "spend_rules:rule_seed_ml:v1": 3720, // 106% of $3,500, flag → flagging
  "spend_rules:rule_seed_contractors:v1": 118, // ~30% of $400; disabled anyway
};

export interface RuleUsage {
  /** Actors the rule currently applies to (null when target can't evaluate). */
  matched: MockActor[] | null;
  /** Estimated spend so far this window, in USD. */
  currentSpendUsd: number;
  /** Projected full-window spend, in USD. */
  projectedSpendUsd: number;
  /** currentSpend / limit, clamped to [0, 1.5] for the bar. */
  utilization: number;
  /** True when the rule is projected to exceed its limit this window. */
  projectedOverLimit: boolean;
}

/** Estimate a rule's usage against the mock directory. Deterministic. */
export function estimateRuleUsage(
  rule: Pick<SpendRule, "targetExpr" | "limitUsd" | "window"> &
    Partial<Pick<SpendRule, "id" | "version" | "evaluatedFrom">>,
  actors: MockActor[] = MOCK_ACTORS,
): RuleUsage {
  const matched = matchActors(rule.targetExpr, actors);
  const monthlyBase = (matched ?? []).reduce(
    (sum, actor) => sum + actor.monthlySpendUsd,
    0,
  );
  const windowBase = monthlyBase * WINDOW_FACTOR[rule.window];
  const seeded =
    rule.id !== undefined && rule.version !== undefined
      ? SEEDED_WINDOW_SPEND[ruleUrn({ id: rule.id, version: rule.version })]
      : undefined;
  const { elapsed, total } = evaluationFractions(
    rule.window,
    rule.evaluatedFrom,
  );
  const current = seeded ?? windowBase * elapsed;
  // Full-pace spend over the part of the window the rule evaluates. A rule
  // edited mid-window can't retroactively spend the part it wasn't watching.
  const projected = Math.max(windowBase * total, current);
  const utilization =
    rule.limitUsd > 0 ? Math.min(1.5, current / rule.limitUsd) : 0;
  return {
    matched,
    currentSpendUsd: current,
    projectedSpendUsd: projected,
    utilization,
    projectedOverLimit: projected > rule.limitUsd,
  };
}

export interface ActorSpendRow {
  actor: MockActor;
  /** This actor's estimated share of the rule's current-window spend, in USD. */
  spendUsd: number;
  /** Fraction of the rule's current-window spend attributable to this actor. */
  share: number;
}

/** Per-person contribution to a rule's current-window spend, largest first.
 *  Answers "which individuals drove this budget" in the rule drill-down. */
export function ruleActorBreakdown(
  rule: Pick<SpendRule, "targetExpr" | "limitUsd" | "window"> &
    Partial<Pick<SpendRule, "id" | "version" | "evaluatedFrom">>,
): ActorSpendRow[] {
  const usage = estimateRuleUsage(rule);
  const matched = usage.matched ?? [];
  const monthlyBase = matched.reduce(
    (sum, actor) => sum + actor.monthlySpendUsd,
    0,
  );
  if (monthlyBase === 0) return [];
  return matched
    .map((actor) => {
      const share = actor.monthlySpendUsd / monthlyBase;
      return { actor, spendUsd: usage.currentSpendUsd * share, share };
    })
    .sort((a, b) => b.spendUsd - a.spendUsd);
}

export interface CoveredSpendSummary {
  /** Sum of enabled rules' current-window spend, in USD. */
  currentUsd: number;
  /** Upper bound: sum of enabled rules' limits, in USD. */
  budgetUsd: number;
  /** Unique people covered by at least one enabled rule. */
  peopleCount: number;
}

/** Org-level rollup for the summary cards. Numerator and denominator are the
 *  same shape — per-rule sums — so a person under two budgets counts against
 *  both, exactly like both limits count toward the total. The people count is
 *  deduplicated, since a person is covered or not. */
export function coveredSpendSummary(rules: SpendRule[]): CoveredSpendSummary {
  const enabled = rules.filter((rule) => rule.enabled);
  const people = new Set<string>();
  let currentUsd = 0;
  for (const rule of enabled) {
    const usage = estimateRuleUsage(rule);
    currentUsd += usage.currentSpendUsd;
    for (const actor of usage.matched ?? []) people.add(actor.id);
  }
  return {
    currentUsd,
    budgetUsd: enabled.reduce((sum, rule) => sum + rule.limitUsd, 0),
    peopleCount: people.size,
  };
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

export function defaultRuleDraft(): RuleDraft {
  return {
    name: "",
    description: "",
    targetExpr: 'department_name == "Engineering"',
    limitUsd: 1000,
    window: "monthly",
    warnAtPct: 80,
    action: "block",
    enabled: true,
  };
}
