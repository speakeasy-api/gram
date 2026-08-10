/* The single adapter seam between the budgets UI and the generated spend-rules
 * SDK. Every @gram/client import for this feature lives here: the components
 * consume app-owned types (budgets-data.ts) and the app-owned hooks below, and
 * never touch the SDK directly. SDK responses are mapped to app types once, at
 * the react-query boundary, so a regeneration that reshapes a model breaks in
 * the mappers here rather than silently rippling through the components. */
import { invalidateAllSpendRulesListEvents } from "@gram/client/react-query/spendRulesListEvents.js";
import { invalidateAllSpendRulesListRules } from "@gram/client/react-query/spendRulesListRules.js";
import { invalidateAllSpendRulesOverview } from "@gram/client/react-query/spendRulesOverview.js";
import { useSpendRulesArchiveRuleMutation } from "@gram/client/react-query/spendRulesArchiveRule.js";
import { useSpendRulesCreateRuleMutation } from "@gram/client/react-query/spendRulesCreateRule.js";
import { useSpendRulesActorAttributes } from "@gram/client/react-query/spendRulesActorAttributes.js";
import { useSpendRulesListEvents } from "@gram/client/react-query/spendRulesListEvents.js";
import { useSpendRulesListRules } from "@gram/client/react-query/spendRulesListRules.js";
import { useSpendRulesOverview } from "@gram/client/react-query/spendRulesOverview.js";
import { useSpendRulesPreviewRuleMutation } from "@gram/client/react-query/spendRulesPreviewRule.js";
import { useSpendRulesUpdateRuleMutation } from "@gram/client/react-query/spendRulesUpdateRule.js";
import type { ActorAttribute as SdkActorAttribute } from "@gram/client/models/components/actorattribute.js";
import type { PreviewSpendRuleResult as SdkPreviewSpendRuleResult } from "@gram/client/models/components/previewspendruleresult.js";
import type { SpendRule as SdkSpendRule } from "@gram/client/models/components/spendrule.js";
import type { SpendRuleActorUsage as SdkSpendRuleActorUsage } from "@gram/client/models/components/spendruleactorusage.js";
import type { SpendRuleEvent as SdkSpendRuleEvent } from "@gram/client/models/components/spendruleevent.js";
import type { SpendRulesOverviewResult as SdkSpendRulesOverviewResult } from "@gram/client/models/components/spendrulesoverviewresult.js";
import type { SpendRuleUsage as SdkSpendRuleUsage } from "@gram/client/models/components/spendruleusage.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import {
  normalizeTargetCondition,
  type ActorAttribute,
  type PreviewSpendRuleResult,
  type RuleDraft,
  type SpendEventType,
  type SpendRule,
  type SpendRuleActorUsage,
  type SpendRuleEvent,
  type SpendRulesOverviewResult,
  type SpendRuleUsage,
} from "./budgets-data";

/* -------------------------------------------------------------------------- */
/*  SDK → app mapping                                                          */
/* -------------------------------------------------------------------------- */

function mapRule(r: SdkSpendRule): SpendRule {
  return {
    id: r.id,
    organizationId: r.organizationId,
    slug: r.slug,
    urn: r.urn,
    version: r.version,
    name: r.name,
    description: r.description,
    target: normalizeTargetCondition(r.target),
    targetExpr: r.targetExpr,
    ruleExpr: r.ruleExpr,
    limitUsd: r.limitUsd,
    windowKind: r.windowKind,
    warnAtPct: r.warnAtPct,
    action: r.action,
    enabled: r.enabled,
    createdAt: r.createdAt,
    updatedAt: r.updatedAt,
  };
}

function mapUsage(u: SdkSpendRuleUsage): SpendRuleUsage {
  return {
    ruleId: u.ruleId,
    status: u.status,
    spendUsd: u.spendUsd,
    budgetUsd: u.budgetUsd,
    matchedUsers: u.matchedUsers,
    usersWarned: u.usersWarned,
    usersBreached: u.usersBreached,
    worstUsedPct: u.worstUsedPct,
  };
}

function mapActor(a: SdkSpendRuleActorUsage): SpendRuleActorUsage {
  return {
    email: a.email,
    displayName: a.displayName,
    userId: a.userId,
    spendUsd: a.spendUsd,
    limitUsd: a.limitUsd,
    usedPct: a.usedPct,
    breached: a.breached,
  };
}

function mapEvent(e: SdkSpendRuleEvent): SpendRuleEvent {
  return {
    id: e.id,
    ruleId: e.ruleId,
    ruleName: e.ruleName,
    ruleUrn: e.ruleUrn,
    eventType: e.eventType,
    email: e.email,
    displayName: e.displayName,
    userId: e.userId,
    spendUsd: e.spendUsd,
    limitUsd: e.limitUsd,
    windowStart: e.windowStart,
    windowEnd: e.windowEnd,
    createdAt: e.createdAt,
  };
}

function mapOverview(o: SdkSpendRulesOverviewResult): SpendRulesOverviewResult {
  return {
    rules: o.rules.map(mapUsage),
    rulesTotal: o.rulesTotal,
    rulesUnhealthy: o.rulesUnhealthy,
    totalSpendUsd: o.totalSpendUsd,
    totalBudgetUsd: o.totalBudgetUsd,
    spendOverBudgetUsd: o.spendOverBudgetUsd,
    usersTotal: o.usersTotal,
    usersBreached: o.usersBreached,
  };
}

function mapPreview(p: SdkPreviewSpendRuleResult): PreviewSpendRuleResult {
  return {
    actors: p.actors.map(mapActor),
    matchedCount: p.matchedCount,
    windowStart: p.windowStart,
    windowEnd: p.windowEnd,
  };
}

function mapActorAttribute(a: SdkActorAttribute): ActorAttribute {
  return { name: a.name, type: a.type, description: a.description };
}

/* -------------------------------------------------------------------------- */
/*  Queries                                                                    */
/* -------------------------------------------------------------------------- */

export interface BudgetRulesQuery {
  rules: SpendRule[];
  isLoading: boolean;
  isError: boolean;
  refetch: () => void;
}

/** All budget rules for the current org, mapped to app types. */
export function useBudgetRules(): BudgetRulesQuery {
  const query = useSpendRulesListRules();
  const rules = useMemo(
    () => (query.data?.rules ?? []).map(mapRule),
    [query.data],
  );
  return {
    rules,
    isLoading: query.isLoading,
    isError: query.isError,
    refetch: () => void query.refetch(),
  };
}

/** Org-wide spend rollup, or undefined until the first fetch resolves. */
export function useBudgetOverview(): {
  overview: SpendRulesOverviewResult | undefined;
} {
  const query = useSpendRulesOverview();
  const overview = useMemo(
    () => (query.data ? mapOverview(query.data) : undefined),
    [query.data],
  );
  return { overview };
}

/** The member-attribute catalog for the rule editor. Static server reference
 *  data (backed by the CEL environment), so it's cached indefinitely. */
export function useActorAttributes(): {
  attributes: ActorAttribute[];
  isLoading: boolean;
  isError: boolean;
} {
  const query = useSpendRulesActorAttributes(undefined, undefined, {
    staleTime: Infinity,
  });
  const attributes = useMemo(
    () => (query.data?.attributes ?? []).map(mapActorAttribute),
    [query.data],
  );
  return {
    attributes,
    isLoading: query.isLoading,
    isError: query.isError,
  };
}

export interface BudgetEventsParams {
  eventType?: SpendEventType;
  cursor?: string;
  limit?: number;
  ruleId?: string;
}

export interface BudgetEventsQuery {
  events: SpendRuleEvent[];
  nextCursor?: string;
  isLoading: boolean;
  isFetching: boolean;
  isError: boolean;
  refetch: () => void;
}

/** One page of budget events for the given filter/cursor, mapped to app types.
 *  Callers accumulate pages themselves; this returns just the fetched page. */
export function useBudgetEvents(params: BudgetEventsParams): BudgetEventsQuery {
  const query = useSpendRulesListEvents(params);
  const events = useMemo(
    () => (query.data?.events ?? []).map(mapEvent),
    [query.data],
  );
  return {
    events,
    nextCursor: query.data?.nextCursor,
    isLoading: query.isLoading,
    isFetching: query.isFetching,
    isError: query.isError,
    refetch: () => void query.refetch(),
  };
}

/* -------------------------------------------------------------------------- */
/*  Mutations                                                                  */
/* -------------------------------------------------------------------------- */

export interface BudgetMutationCallbacks {
  onSuccess?: () => void;
  /** Receives the error message string, so callers never see SDK error types. */
  onError?: (message: string) => void;
}

/** Rule edits archive the current version row and create a successor. */
export type UpdateBudgetRuleInput = { id: string } & Partial<RuleDraft>;

export function useCreateBudgetRule(cb: BudgetMutationCallbacks = {}): {
  create: (draft: RuleDraft) => void;
  isPending: boolean;
} {
  const mutation = useSpendRulesCreateRuleMutation({
    onSuccess: () => cb.onSuccess?.(),
    onError: (error) => cb.onError?.(error.message),
  });
  const { mutate } = mutation;
  const create = useCallback(
    (draft: RuleDraft) => {
      mutate({ request: { createSpendRuleRequestBody: draft } });
    },
    [mutate],
  );
  return { create, isPending: mutation.isPending };
}

export function useUpdateBudgetRule(cb: BudgetMutationCallbacks = {}): {
  update: (input: UpdateBudgetRuleInput) => void;
  isPending: boolean;
} {
  const mutation = useSpendRulesUpdateRuleMutation({
    onSuccess: () => cb.onSuccess?.(),
    onError: (error) => cb.onError?.(error.message),
  });
  const { mutate } = mutation;
  const update = useCallback(
    (input: UpdateBudgetRuleInput) => {
      mutate({ request: { updateSpendRuleRequestBody: input } });
    },
    [mutate],
  );
  return { update, isPending: mutation.isPending };
}

export function useArchiveBudgetRule(cb: BudgetMutationCallbacks = {}): {
  archive: (id: string) => void;
  isPending: boolean;
} {
  const mutation = useSpendRulesArchiveRuleMutation({
    onSuccess: () => cb.onSuccess?.(),
    onError: (error) => cb.onError?.(error.message),
  });
  const { mutate } = mutation;
  const archive = useCallback(
    (id: string) => {
      mutate({ request: { riskIDRequestBody: { id } } });
    },
    [mutate],
  );
  return { archive, isPending: mutation.isPending };
}

/** The rule fields a preview depends on — a subset of the full draft. */
export type PreviewRuleInput = Pick<
  RuleDraft,
  "target" | "limitUsd" | "warnAtPct" | "windowKind"
>;

export function usePreviewBudgetRule(): {
  preview: (
    input: PreviewRuleInput,
    opts?: { onSuccess?: (result: PreviewSpendRuleResult) => void },
  ) => void;
  isPending: boolean;
  isError: boolean;
} {
  const mutation = useSpendRulesPreviewRuleMutation();
  const { mutate } = mutation;
  const preview = useCallback(
    (
      input: PreviewRuleInput,
      opts?: { onSuccess?: (result: PreviewSpendRuleResult) => void },
    ) => {
      mutate(
        {
          request: {
            previewSpendRuleRequestBody: {
              target: input.target,
              limitUsd: input.limitUsd,
              warnAtPct: input.warnAtPct,
              windowKind: input.windowKind,
            },
          },
        },
        { onSuccess: (data) => opts?.onSuccess?.(mapPreview(data)) },
      );
    },
    [mutate],
  );
  return { preview, isPending: mutation.isPending, isError: mutation.isError };
}

/* -------------------------------------------------------------------------- */
/*  Invalidation                                                               */
/* -------------------------------------------------------------------------- */

/** Rules, overview, and events all describe the same state; a rule mutation
 *  refreshes every budgets query so no tab shows stale numbers. */
export function useInvalidateBudgetQueries(): () => void {
  const client = useQueryClient();
  return useCallback(() => {
    void invalidateAllSpendRulesListRules(client);
    void invalidateAllSpendRulesOverview(client);
    void invalidateAllSpendRulesListEvents(client);
  }, [client]);
}
