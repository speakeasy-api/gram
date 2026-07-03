import { useSyncExternalStore } from "react";
import type { ActorRecord } from "./budget-cel";
import { ACTOR_ATTRIBUTES, matchActors } from "./budget-cel";

/* -------------------------------------------------------------------------- */
/*  Models & providers                                                         */
/* -------------------------------------------------------------------------- */

/** Cost tier of a model — drives how much of an actor's spend is "in scope"
 *  when a rule is scoped to specific models/providers. */
export type ModelTier = "frontier" | "standard" | "open";

export interface ModelDef {
  id: string;
  label: string;
  provider: string;
  tier: ModelTier;
}

export const PROVIDERS = ["OpenAI", "Anthropic", "Google", "Mistral"] as const;

export const MODELS: ModelDef[] = [
  { id: "gpt-5.4", label: "GPT-5.4", provider: "OpenAI", tier: "frontier" },
  {
    id: "gpt-5-mini",
    label: "GPT-5 mini",
    provider: "OpenAI",
    tier: "standard",
  },
  {
    id: "claude-opus-4.8",
    label: "Claude Opus 4.8",
    provider: "Anthropic",
    tier: "frontier",
  },
  {
    id: "claude-sonnet-5",
    label: "Claude Sonnet 5",
    provider: "Anthropic",
    tier: "standard",
  },
  {
    id: "gemini-3-pro",
    label: "Gemini 3 Pro",
    provider: "Google",
    tier: "frontier",
  },
  {
    id: "gemini-3-flash",
    label: "Gemini 3 Flash",
    provider: "Google",
    tier: "standard",
  },
  {
    id: "mistral-large",
    label: "Mistral Large",
    provider: "Mistral",
    tier: "open",
  },
];

export const MODEL_BY_ID = new Map(MODELS.map((m) => [m.id, m]));

/** Approximate share of an actor's spend attributable to each tier — used to
 *  estimate in-scope spend when a rule targets specific models/providers. */
const TIER_SHARE: Record<ModelTier, number> = {
  frontier: 0.65,
  standard: 0.3,
  open: 0.05,
};

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

export const MOCK_ACTORS: MockActor[] = [
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

/* -------------------------------------------------------------------------- */
/*  Rules                                                                      */
/* -------------------------------------------------------------------------- */

export type BudgetWindow = "daily" | "weekly" | "monthly";
export type WindowReset = "fixed" | "rolling";
export type BreachAction = "block" | "route_fallback" | "alert_only";

export interface SpendRule {
  id: string;
  name: string;
  description: string;
  /** CEL predicate over actor attributes — who the rule applies to. */
  targetExpr: string;
  limitUsd: number;
  window: BudgetWindow;
  reset: WindowReset;
  /** Models the limit applies to; empty = all models. */
  models: string[];
  /** Providers the limit applies to; empty = all providers. */
  providers: string[];
  breachAction: BreachAction;
  /** Fallback model when breachAction is route_fallback. */
  fallbackModel: string;
  /** Whether the rule is on. Disabled rules are ignored at request time. */
  enabled: boolean;
  createdAt: string;
}

export type RuleDraft = Omit<SpendRule, "id" | "createdAt">;

export const WINDOW_LABELS: Record<BudgetWindow, string> = {
  daily: "Daily",
  weekly: "Weekly",
  monthly: "Monthly",
};

export const BREACH_ACTION_LABELS: Record<BreachAction, string> = {
  block: "Block requests",
  route_fallback: "Route to fallback model",
  alert_only: "Alert only",
};

let nextId = 1;
function makeId(): string {
  return `rule_${Date.now().toString(36)}_${nextId++}`;
}

const SEED_RULES: SpendRule[] = [
  {
    id: "rule_seed_eng",
    name: "Engineering frontier cap",
    description:
      "Engineers get a generous monthly budget on frontier models, then fall back to a cheaper model.",
    targetExpr: 'department_name == "Engineering" && job_title != "Manager"',
    limitUsd: 5000,
    window: "monthly",
    reset: "fixed",
    models: ["gpt-5.4", "claude-opus-4.8", "gemini-3-pro"],
    providers: [],
    breachAction: "route_fallback",
    fallbackModel: "gpt-5-mini",
    enabled: true,
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
    reset: "fixed",
    models: [],
    providers: [],
    breachAction: "block",
    fallbackModel: "",
    enabled: true,
    createdAt: "2026-06-05T00:00:00.000Z",
  },
  {
    id: "rule_seed_ml",
    name: "ML team alert threshold",
    description:
      "Watch the data science team's spend and alert admins when it crosses the threshold.",
    targetExpr: '"ml-team" in groups',
    limitUsd: 8000,
    window: "monthly",
    reset: "rolling",
    models: [],
    providers: ["Anthropic", "OpenAI"],
    breachAction: "alert_only",
    fallbackModel: "",
    enabled: true,
    createdAt: "2026-06-10T00:00:00.000Z",
  },
  {
    id: "rule_seed_contractors",
    name: "Contractor weekly guardrail",
    description:
      "Contractors get a weekly budget on frontier models to keep short engagements predictable.",
    targetExpr: 'employee_type == "contractor"',
    limitUsd: 400,
    window: "weekly",
    reset: "rolling",
    models: ["gpt-5.4", "claude-opus-4.8"],
    providers: [],
    breachAction: "route_fallback",
    fallbackModel: "mistral-large",
    enabled: false,
    createdAt: "2026-06-14T00:00:00.000Z",
  },
];

/* -------------------------------------------------------------------------- */
/*  Events (mocked budget outcomes)                                            */
/* -------------------------------------------------------------------------- */

export interface SpendControlEvent {
  id: string;
  occurredAt: string;
  actorName: string;
  actorEmail: string;
  ruleName: string;
  action: BreachAction;
  model: string;
  fallbackModel?: string;
  spendUsd: number;
  limitUsd: number;
}

export const SPEND_CONTROL_EVENTS: SpendControlEvent[] = [
  {
    id: "evt_budget_001",
    occurredAt: "2026-07-02T12:58:00.000Z",
    actorName: "Sam Rivera",
    actorEmail: "sam@acme.com",
    ruleName: "Intern hard limit",
    action: "block",
    model: "claude-opus-4.8",
    spendUsd: 214,
    limitUsd: 200,
  },
  {
    id: "evt_budget_002",
    occurredAt: "2026-07-02T11:43:00.000Z",
    actorName: "Omar Vasquez",
    actorEmail: "omar@contractor.dev",
    ruleName: "Contractor weekly guardrail",
    action: "route_fallback",
    model: "claude-opus-4.8",
    fallbackModel: "mistral-large",
    spendUsd: 436,
    limitUsd: 400,
  },
  {
    id: "evt_budget_003",
    occurredAt: "2026-07-02T09:21:00.000Z",
    actorName: "Kenji Watanabe",
    actorEmail: "kenji@acme.com",
    ruleName: "ML team alert threshold",
    action: "alert_only",
    model: "gpt-5.4",
    spendUsd: 8420,
    limitUsd: 8000,
  },
  {
    id: "evt_budget_004",
    occurredAt: "2026-07-01T17:08:00.000Z",
    actorName: "Grace Lindqvist",
    actorEmail: "grace@acme.com",
    ruleName: "Engineering frontier cap",
    action: "route_fallback",
    model: "gemini-3-pro",
    fallbackModel: "gpt-5-mini",
    spendUsd: 5210,
    limitUsd: 5000,
  },
  {
    id: "evt_budget_005",
    occurredAt: "2026-07-01T14:32:00.000Z",
    actorName: "Mira Haddad",
    actorEmail: "mira@acme.com",
    ruleName: "Intern hard limit",
    action: "block",
    model: "gpt-5.4",
    spendUsd: 206,
    limitUsd: 200,
  },
  {
    id: "evt_budget_006",
    occurredAt: "2026-07-01T10:05:00.000Z",
    actorName: "Kenji Watanabe",
    actorEmail: "kenji@acme.com",
    ruleName: "ML team alert threshold",
    action: "alert_only",
    model: "claude-opus-4.8",
    spendUsd: 8110,
    limitUsd: 8000,
  },
  {
    id: "evt_budget_007",
    occurredAt: "2026-06-30T19:47:00.000Z",
    actorName: "Omar Vasquez",
    actorEmail: "omar@contractor.dev",
    ruleName: "Contractor weekly guardrail",
    action: "route_fallback",
    model: "gpt-5.4",
    fallbackModel: "mistral-large",
    spendUsd: 402,
    limitUsd: 400,
  },
  {
    id: "evt_budget_008",
    occurredAt: "2026-06-30T15:12:00.000Z",
    actorName: "Sam Rivera",
    actorEmail: "sam@acme.com",
    ruleName: "Intern hard limit",
    action: "block",
    model: "gemini-3-pro",
    spendUsd: 231,
    limitUsd: 200,
  },
  {
    id: "evt_budget_009",
    occurredAt: "2026-06-30T08:26:00.000Z",
    actorName: "Ada Okafor",
    actorEmail: "ada@acme.com",
    ruleName: "Engineering frontier cap",
    action: "route_fallback",
    model: "claude-opus-4.8",
    fallbackModel: "gpt-5-mini",
    spendUsd: 5480,
    limitUsd: 5000,
  },
];

/* -------------------------------------------------------------------------- */
/*  In-memory store (prototype only — no persistence, resets on reload)        */
/* -------------------------------------------------------------------------- */

let rules: SpendRule[] = [...SEED_RULES];
const listeners = new Set<() => void>();

function emit(): void {
  rules = [...rules];
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
  addRule: (draft: RuleDraft) => SpendRule;
  updateRule: (id: string, patch: Partial<RuleDraft>) => void;
  removeRule: (id: string) => void;
}

export function useBudgetStore(): BudgetStore {
  const snapshot = useSyncExternalStore(
    subscribe,
    () => rules,
    () => rules,
  );

  return {
    rules: snapshot,
    addRule: (draft) => {
      const rule: SpendRule = {
        ...draft,
        id: makeId(),
        createdAt: new Date().toISOString(),
      };
      rules.push(rule);
      emit();
      return rule;
    },
    updateRule: (id, patch) => {
      rules = rules.map((r) => (r.id === id ? { ...r, ...patch } : r));
      emit();
    },
    removeRule: (id) => {
      rules = rules.filter((r) => r.id !== id);
      emit();
    },
  };
}

/* -------------------------------------------------------------------------- */
/*  Spend estimation                                                           */
/* -------------------------------------------------------------------------- */

/** Fraction of a full monthly spend attributable to the rule's model/provider
 *  scope. Returns 1 when unscoped (applies to everything). */
export function scopeFraction(models: string[], providers: string[]): number {
  if (models.length > 0) {
    const tiers = new Set(
      models.map((id) => MODEL_BY_ID.get(id)?.tier).filter(Boolean),
    );
    let share = 0;
    for (const tier of tiers) share += TIER_SHARE[tier as ModelTier];
    return Math.min(1, share || 0.1);
  }
  if (providers.length > 0) {
    return Math.min(1, providers.length / PROVIDERS.length + 0.15);
  }
  return 1;
}

/** Fraction of the current window that has elapsed — makes the "spend so far"
 *  bar move deterministically with the calendar rather than looking static. */
function windowElapsedFraction(window: BudgetWindow): number {
  const now = new Date();
  switch (window) {
    case "daily": {
      const secs = now.getHours() * 3600 + now.getMinutes() * 60;
      return secs / (24 * 3600);
    }
    case "weekly": {
      const day = (now.getDay() + 6) % 7; // Monday = 0
      return (day + now.getHours() / 24) / 7;
    }
    case "monthly": {
      const daysInMonth = new Date(
        now.getFullYear(),
        now.getMonth() + 1,
        0,
      ).getDate();
      return (now.getDate() - 1 + now.getHours() / 24) / daysInMonth;
    }
  }
}

const WINDOW_FACTOR: Record<BudgetWindow, number> = {
  daily: 1 / 30,
  weekly: 7 / 30,
  monthly: 1,
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
  rule: Pick<
    SpendRule,
    "targetExpr" | "limitUsd" | "window" | "models" | "providers"
  >,
  actors: MockActor[] = MOCK_ACTORS,
): RuleUsage {
  const matched = matchActors(rule.targetExpr, actors);
  const monthlyBase = (matched ?? []).reduce(
    (sum, actor) => sum + actor.monthlySpendUsd,
    0,
  );
  const inScopeMonthly =
    monthlyBase * scopeFraction(rule.models, rule.providers);
  const projected = inScopeMonthly * WINDOW_FACTOR[rule.window];
  const current = projected * windowElapsedFraction(rule.window);
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

/** Short human summary of a rule's model/provider scope. */
export function scopeSummary(models: string[], providers: string[]): string {
  if (models.length > 0) {
    const labels = models.map((id) => MODEL_BY_ID.get(id)?.label ?? id);
    if (labels.length <= 2) return labels.join(", ");
    return `${labels.slice(0, 2).join(", ")} +${labels.length - 2}`;
  }
  if (providers.length > 0) return providers.join(", ");
  return "All models";
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
    reset: "fixed",
    models: [],
    providers: [],
    breachAction: "block",
    fallbackModel: "",
    enabled: true,
  };
}
