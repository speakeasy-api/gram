import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import {
  useSpendRulesListEvents,
  useSpendRulesPreviewRuleMutation,
} from "@gram/client/react-query/index.js";
import { Loader2, Pencil, Users } from "lucide-react";
import { useEffect, useMemo, useState, type JSX } from "react";
import {
  EventTypeBadge,
  RuleActionBadge,
  RuleStatusBadge,
  UsageBar,
} from "./budget-shared";
import {
  WINDOW_LABELS,
  formatUsd,
  parseRuleUrn,
  ruleStatusOf,
  sortEventsByRecency,
  targetSummary,
  timeUntilWindowReset,
  type PreviewSpendRuleResult,
  type SpendRule,
  type SpendRuleActorUsage,
  type SpendRuleUsage,
} from "./budgets-data";

/** Read-only drill-down for one rule: live budget state, which people are
 *  driving the spend, and the rule's lifecycle events. */
export function RuleDetailSheet({
  rule,
  usage,
  onClose,
  onEdit,
}: {
  rule: SpendRule | null;
  /** Server-computed current-window usage from the overview endpoint.
   *  Undefined for disabled rules (the evaluator skips them). */
  usage: SpendRuleUsage | undefined;
  onClose: () => void;
  onEdit: (rule: SpendRule) => void;
}): JSX.Element {
  return (
    <Sheet
      open={rule !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent className="flex flex-col overflow-y-auto sm:max-w-xl">
        {rule && <RuleDetail rule={rule} usage={usage} onEdit={onEdit} />}
      </SheetContent>
    </Sheet>
  );
}

/** Per-actor current-window spend for one rule, fetched once per open via the
 *  preview endpoint with the rule's stored config. */
function useRuleActorBreakdown(rule: SpendRule): {
  preview: PreviewSpendRuleResult | null;
  loading: boolean;
} {
  const previewMutation = useSpendRulesPreviewRuleMutation();
  const [preview, setPreview] = useState<PreviewSpendRuleResult | null>(null);
  const { mutate } = previewMutation;

  useEffect(() => {
    mutate(
      {
        request: {
          previewSpendRuleRequestBody: {
            targetExpr: rule.targetExpr,
            limitUsd: rule.limitUsd,
            windowKind: rule.windowKind,
            evaluatedFrom: rule.evaluatedFrom,
          },
        },
      },
      { onSuccess: (data) => setPreview(data) },
    );
  }, [
    rule.targetExpr,
    rule.limitUsd,
    rule.windowKind,
    rule.evaluatedFrom,
    mutate,
  ]);

  return { preview, loading: previewMutation.isPending };
}

function RuleDetail({
  rule,
  usage,
  onEdit,
}: {
  rule: SpendRule;
  usage: SpendRuleUsage | undefined;
  onEdit: (rule: SpendRule) => void;
}): JSX.Element {
  const status = ruleStatusOf(rule, usage);
  const { preview, loading: actorsLoading } = useRuleActorBreakdown(rule);
  const { data: eventsData } = useSpendRulesListEvents({
    ruleId: rule.id,
    limit: 50,
  });
  const events = useMemo(
    () => sortEventsByRecency(eventsData?.events ?? []),
    [eventsData],
  );

  const actors = preview?.actors ?? [];

  return (
    <>
      <SheetHeader className="px-6 pt-6">
        <div className="flex items-center gap-2">
          <SheetTitle className="min-w-0 truncate">{rule.name}</SheetTitle>
          <RuleStatusBadge status={status} />
          <RuleActionBadge action={rule.action} />
        </div>
        <SheetDescription>
          {rule.description || "No description."}
        </SheetDescription>
        <p className="text-muted-foreground text-xs">
          Applies to: {targetSummary(rule.targetExpr)}
        </p>
        <p
          className="text-muted-foreground font-mono text-xs"
          title="Versioned rule identity. Events cite the URN they fired under; material edits bump the version and restart evaluation."
        >
          {rule.urn}
        </p>
      </SheetHeader>

      <div className="flex-1 space-y-6 px-6 py-4">
        {status === "blocking" && (
          <p className="border-destructive/50 bg-destructive/5 text-destructive rounded-md border px-3 py-2 text-xs">
            Circuit open — requests from people over their budget are being
            rejected until the window resets in{" "}
            {timeUntilWindowReset(rule.windowKind)}.
          </p>
        )}

        {/* Budget state */}
        <section className="space-y-2">
          <div className="flex items-baseline justify-between">
            <Type variant="small" className="font-medium">
              Budget
            </Type>
            <span className="text-muted-foreground text-xs">
              {WINDOW_LABELS[rule.windowKind]} window · resets in{" "}
              {timeUntilWindowReset(rule.windowKind)} · warns at{" "}
              {rule.warnAtPct}%
            </span>
          </div>
          {usage ? (
            <>
              <UsageBar
                spendUsd={usage.spendUsd}
                limitUsd={usage.budgetUsd}
                warnAtPct={rule.warnAtPct}
              />
              <p className="text-muted-foreground text-xs">
                {formatUsd(rule.limitUsd)} per person · {usage.matchedUsers}{" "}
                matched {usage.matchedUsers === 1 ? "person" : "people"}
              </p>
            </>
          ) : (
            <p className="text-muted-foreground text-xs">
              {formatUsd(rule.limitUsd)} per person.{" "}
              {rule.enabled
                ? "Live usage appears after the next evaluation cycle."
                : "This rule is disabled, so it is not being evaluated."}
            </p>
          )}
        </section>

        {/* Who is driving the spend */}
        <section className="space-y-2">
          <div className="flex items-center gap-2">
            <Users className="size-3.5" />
            <Type variant="small" className="font-medium">
              People {preview ? `(${preview.matchedCount})` : ""}
            </Type>
            {actorsLoading && (
              <Loader2 className="text-muted-foreground size-3.5 animate-spin" />
            )}
          </div>
          {actors.length === 0 ? (
            <p className="text-muted-foreground text-xs">
              {actorsLoading
                ? "Matching directory users…"
                : "No directory-synced people match this rule right now."}
            </p>
          ) : (
            <ul className="divide-border rounded-lg border divide-y">
              {actors.map((actor) => (
                <ActorRow key={actor.email} actor={actor} />
              ))}
            </ul>
          )}
        </section>

        {/* Lifecycle events */}
        <section className="space-y-2">
          <Type variant="small" className="font-medium">
            Events
          </Type>
          {events.length === 0 ? (
            <p className="text-muted-foreground text-xs">
              No budget events recorded for this rule yet.
            </p>
          ) : (
            <ul className="divide-border rounded-lg border divide-y">
              {events.map((event) => {
                const version = parseRuleUrn(event.ruleUrn)?.version;
                const fromOldVersion =
                  version !== undefined && version !== Number(rule.version);
                return (
                  <li key={event.id} className="space-y-1 px-3 py-2.5">
                    <div className="flex items-center justify-between gap-2">
                      <span className="flex min-w-0 items-center gap-2">
                        <EventTypeBadge type={event.eventType} />
                        {fromOldVersion && (
                          <span
                            className="text-muted-foreground shrink-0 font-mono text-xs"
                            title={event.ruleUrn}
                          >
                            v{version}
                          </span>
                        )}
                      </span>
                      <span className="text-muted-foreground shrink-0 font-mono text-xs">
                        {event.createdAt.toLocaleString()}
                      </span>
                    </div>
                    <p className="text-muted-foreground text-xs">
                      {event.displayName || event.email} ·{" "}
                      {formatUsd(event.spendUsd)} of {formatUsd(event.limitUsd)}
                    </p>
                  </li>
                );
              })}
            </ul>
          )}
        </section>
      </div>

      <SheetFooter className="border-border flex-row items-center justify-end border-t px-6 py-4">
        <Button variant="secondary" onClick={() => onEdit(rule)}>
          <Pencil className="mr-2 h-4 w-4" />
          Edit rule
        </Button>
      </SheetFooter>
    </>
  );
}

/** One matched person: spend against their per-person budget. */
function ActorRow({ actor }: { actor: SpendRuleActorUsage }): JSX.Element {
  const over = actor.spendUsd >= actor.limitUsd;
  const pct = Math.min(150, Math.round(actor.usedPct));
  return (
    <li className="space-y-1.5 px-3 py-2">
      <div className="flex items-center justify-between gap-3 text-xs">
        <div className="min-w-0">
          <div className="truncate font-medium">
            {actor.displayName || actor.email}
          </div>
          {actor.displayName && (
            <div className="text-muted-foreground truncate">{actor.email}</div>
          )}
        </div>
        <div className="shrink-0 text-right">
          <div className="font-mono">
            {formatUsd(actor.spendUsd)} of {formatUsd(actor.limitUsd)}
          </div>
          <div className="text-muted-foreground">{pct}% of budget</div>
        </div>
      </div>
      <div className="bg-muted h-1 w-full overflow-hidden rounded-full">
        <div
          className={
            over
              ? "bg-destructive h-full rounded-full"
              : "bg-primary/50 h-full rounded-full"
          }
          style={{
            width: `${Math.max(4, Math.min(100, actor.usedPct))}%`,
          }}
        />
      </div>
    </li>
  );
}
