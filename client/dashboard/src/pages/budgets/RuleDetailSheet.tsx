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
import { Pencil, Users } from "lucide-react";
import { useMemo, type JSX } from "react";
import {
  EventTypeBadge,
  RuleActionBadge,
  RuleStatusBadge,
  UsageBar,
} from "./budget-shared";
import {
  WINDOW_LABELS,
  estimateRuleUsage,
  formatUsd,
  parseRuleUrn,
  ruleActorBreakdown,
  ruleStatus,
  ruleUrn,
  targetSummary,
  timeUntilWindowReset,
  useBudgetStore,
  type ActorSpendRow,
  type SpendControlEvent,
  type SpendRule,
} from "./budgets-data";

/** Read-only drill-down for one rule: live budget state, which people are
 *  driving the spend, and the rule's lifecycle events this window. */
export function RuleDetailSheet({
  rule,
  onClose,
  onEdit,
}: {
  rule: SpendRule | null;
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
        {rule && <RuleDetail rule={rule} onEdit={onEdit} />}
      </SheetContent>
    </Sheet>
  );
}

function RuleDetail({
  rule,
  onEdit,
}: {
  rule: SpendRule;
  onEdit: (rule: SpendRule) => void;
}): JSX.Element {
  const { events: allEvents } = useBudgetStore();
  const usage = useMemo(() => estimateRuleUsage(rule), [rule]);
  const breakdown = useMemo(() => ruleActorBreakdown(rule), [rule]);
  const events = useMemo(
    () => ruleEvents(allEvents, rule.id),
    [allEvents, rule.id],
  );
  const status = ruleStatus(rule, usage);

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
          {ruleUrn(rule)}
        </p>
      </SheetHeader>

      <div className="flex-1 space-y-6 px-6 py-4">
        {status === "blocking" && (
          <p className="border-destructive/50 bg-destructive/5 text-destructive rounded-md border px-3 py-2 text-xs">
            Circuit open — requests from matched people are being rejected until
            the window resets in {timeUntilWindowReset(rule.window)}.
          </p>
        )}

        {/* Budget state */}
        <section className="space-y-2">
          <div className="flex items-baseline justify-between">
            <Type variant="small" className="font-medium">
              Budget
            </Type>
            <span className="text-muted-foreground text-xs">
              {WINDOW_LABELS[rule.window]} window · resets in{" "}
              {timeUntilWindowReset(rule.window)} · warns at {rule.warnAtPct}%
            </span>
          </div>
          <UsageBar
            usage={usage}
            limitUsd={rule.limitUsd}
            warnAtPct={rule.warnAtPct}
          />
        </section>

        {/* Who is driving the spend */}
        <section className="space-y-2">
          <div className="flex items-center gap-2">
            <Users className="size-3.5" />
            <Type variant="small" className="font-medium">
              People ({breakdown.length})
            </Type>
          </div>
          <ContributionSummary rows={breakdown} />
          {breakdown.length === 0 ? (
            <p className="text-muted-foreground text-xs">
              No one in the mock directory matches this rule right now.
            </p>
          ) : (
            <ul className="divide-border rounded-lg border divide-y">
              {breakdown.map((row) => (
                <ActorRow
                  key={row.actor.id}
                  row={row}
                  topShare={breakdown[0]!.share}
                />
              ))}
            </ul>
          )}
        </section>

        {/* Lifecycle events this window */}
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
                  version !== undefined && version !== rule.version;
                return (
                  <li key={event.id} className="space-y-1 px-3 py-2.5">
                    <div className="flex items-center justify-between gap-2">
                      <span className="flex min-w-0 items-center gap-2">
                        <EventTypeBadge type={event.type} />
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
                        {new Date(event.occurredAt).toLocaleString()}
                      </span>
                    </div>
                    <p className="text-muted-foreground text-xs">
                      {event.summary}
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

/** All events for a rule, across every version — the URN carries the rule id,
 *  so history survives renames and config changes. */
function ruleEvents(
  events: SpendControlEvent[],
  ruleId: string,
): SpendControlEvent[] {
  return events
    .filter((event) => parseRuleUrn(event.ruleUrn)?.id === ruleId)
    .sort(
      (a, b) =>
        new Date(b.occurredAt).getTime() - new Date(a.occurredAt).getTime(),
    );
}

/** One line answering "why is this budget where it is". */
function ContributionSummary({
  rows,
}: {
  rows: ActorSpendRow[];
}): JSX.Element | null {
  if (rows.length < 2) return null;
  if (rows.length <= 3) {
    const top = rows[0]!;
    return (
      <p className="text-muted-foreground text-xs">
        {top.actor.name} drives {Math.round(top.share * 100)}% of this window's
        spend.
      </p>
    );
  }
  const topThreeShare = rows
    .slice(0, 3)
    .reduce((sum, row) => sum + row.share, 0);
  return (
    <p className="text-muted-foreground text-xs">
      The top 3 of {rows.length} people account for{" "}
      {Math.round(topThreeShare * 100)}% of this window's spend.
    </p>
  );
}

function ActorRow({
  row,
  topShare,
}: {
  row: ActorSpendRow;
  topShare: number;
}): JSX.Element {
  const relative = topShare > 0 ? row.share / topShare : 0;
  return (
    <li className="space-y-1.5 px-3 py-2">
      <div className="flex items-center justify-between gap-3 text-xs">
        <div className="min-w-0">
          <div className="truncate font-medium">{row.actor.name}</div>
          <div className="text-muted-foreground truncate">
            {row.actor.department_name} · {row.actor.job_title}
          </div>
        </div>
        <div className="shrink-0 text-right">
          <div className="font-mono">{formatUsd(row.spendUsd)}</div>
          <div className="text-muted-foreground">
            {Math.round(row.share * 100)}% of spend
          </div>
        </div>
      </div>
      <div className="bg-muted h-1 w-full overflow-hidden rounded-full">
        <div
          className="bg-primary/50 h-full rounded-full"
          style={{ width: `${Math.max(4, relative * 100)}%` }}
        />
      </div>
    </li>
  );
}
