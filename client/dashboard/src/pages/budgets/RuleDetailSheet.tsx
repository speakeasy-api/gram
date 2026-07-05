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
import { Loader2, Pencil, Search, Users } from "lucide-react";
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
            target: rule.target,
            limitUsd: rule.limitUsd,
            warnAtPct: rule.warnAtPct,
            windowKind: rule.windowKind,
          },
        },
      },
      { onSuccess: (data) => setPreview(data) },
    );
  }, [rule.target, rule.limitUsd, rule.warnAtPct, rule.windowKind, mutate]);

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
          Applies to: {targetSummary(rule.target)}
        </p>
        <p
          className="text-muted-foreground font-mono text-xs"
          title="Versioned rule identity. Events cite the URN they fired under; material edits bump the version."
        >
          {rule.urn}
        </p>
      </SheetHeader>

      <div className="flex-1 space-y-6 px-6 py-4">
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
        <PeopleSection
          actors={actors}
          matchedCount={preview?.matchedCount ?? 0}
          loading={actorsLoading}
        />

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

/** Number of people shown before the list collapses behind a "show all"
 *  toggle. Large orgs can match hundreds of people; the top spenders are the
 *  ones worth surfacing first. */
const ACTOR_PREVIEW_LIMIT = 5;

/** Matched people driving the spend, sorted by spend (highest first) from the
 *  server. Shows the top few by default and lets the admin search or expand
 *  the full returned list so a large org doesn't flood the sheet. */
function PeopleSection({
  actors,
  matchedCount,
  loading,
}: {
  actors: SpendRuleActorUsage[];
  matchedCount: number;
  loading: boolean;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState(false);

  const searching = query.trim().length > 0;

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return actors;
    return actors.filter(
      (actor) =>
        actor.email.toLowerCase().includes(q) ||
        (actor.displayName ?? "").toLowerCase().includes(q),
    );
  }, [actors, query]);

  const visible =
    searching || expanded ? filtered : filtered.slice(0, ACTOR_PREVIEW_LIMIT);
  const hiddenCount = filtered.length - visible.length;
  const searchable = actors.length > ACTOR_PREVIEW_LIMIT;
  // The server caps the returned list; note when more people match than we can
  // search through here.
  const cappedCount = matchedCount - actors.length;

  return (
    <section className="space-y-2">
      <div className="flex items-center gap-2">
        <Users className="size-3.5" />
        <Type variant="small" className="font-medium">
          People {matchedCount > 0 ? `(${matchedCount})` : ""}
        </Type>
        {loading && (
          <Loader2 className="text-muted-foreground size-3.5 animate-spin" />
        )}
      </div>

      {searchable && (
        <div className="relative">
          <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search people"
            className="border-input focus-visible:border-ring focus-visible:ring-ring/50 h-8 w-full rounded-md border bg-transparent pr-3 pl-8 text-xs outline-none focus-visible:ring-[3px]"
          />
        </div>
      )}

      {actors.length === 0 ? (
        <p className="text-muted-foreground text-xs">
          {loading
            ? "Matching members…"
            : "No members match this rule right now."}
        </p>
      ) : visible.length === 0 ? (
        <p className="text-muted-foreground text-xs">
          No people match “{query.trim()}”.
        </p>
      ) : (
        <ul className="divide-border rounded-lg border divide-y">
          {visible.map((actor) => (
            <ActorRow key={actor.email} actor={actor} />
          ))}
        </ul>
      )}

      {!searching && hiddenCount > 0 && (
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className="text-muted-foreground hover:text-foreground text-xs underline underline-offset-2"
        >
          Show all {filtered.length} people
        </button>
      )}
      {!searching && expanded && filtered.length > ACTOR_PREVIEW_LIMIT && (
        <button
          type="button"
          onClick={() => setExpanded(false)}
          className="text-muted-foreground hover:text-foreground text-xs underline underline-offset-2"
        >
          Show less
        </button>
      )}
      {cappedCount > 0 && (
        <p className="text-muted-foreground text-xs">
          Showing the top {actors.length} spenders of {matchedCount} matched
          people.
        </p>
      )}
    </section>
  );
}

/** One matched person: spend against their per-person budget. */
function ActorRow({ actor }: { actor: SpendRuleActorUsage }): JSX.Element {
  const over = actor.breached;
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
