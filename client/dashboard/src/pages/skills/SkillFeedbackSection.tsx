import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { SettingsSection } from "@/components/detail/settings-section";
import type { SkillFeedback } from "@gram/client/models/components/skillfeedback.js";
import type { SkillFeedbackCounts } from "@gram/client/models/components/skillfeedbackcounts.js";
import type { SkillFeedbackMetrics } from "@gram/client/models/components/skillfeedbackmetrics.js";
import type { SkillFeedbackTimelinePoint } from "@gram/client/models/components/skillfeedbacktimelinepoint.js";
import { useSkillFeedbackInfinite } from "@gram/client/react-query/skillFeedback.js";
import { useTriggerSkillSuggestionMutation } from "@gram/client/react-query/triggerSkillSuggestion.js";
import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";
import { useMemo, useState } from "react";
import { toast } from "sonner";

const OUTCOMES: Array<{
  key: Exclude<keyof SkillFeedbackCounts, "total">;
  label: string;
  className: string;
}> = [
  { key: "helped", label: "Helped", className: "bg-success-default" },
  {
    key: "partiallyHelped",
    label: "Partially helped",
    className: "bg-success-default/55",
  },
  { key: "didNotHelp", label: "Did not help", className: "bg-warning" },
  {
    key: "misleading",
    label: "Misleading",
    className: "bg-destructive/60",
  },
  { key: "harmful", label: "Harmful", className: "bg-destructive" },
];

type FeedbackGroup = {
  key: string;
  note: string;
  items: SkillFeedback[];
};

type IndexedFeedbackGroup = FeedbackGroup & {
  tokenSets: Set<string>[];
};

function percentage(part: number, total: number): number {
  return total === 0 ? 0 : (part / total) * 100;
}

function groupFeedback(feedback: SkillFeedback[]): FeedbackGroup[] {
  const groups: IndexedFeedbackGroup[] = [];
  const groupsByKey = new Map<string, IndexedFeedbackGroup>();
  const groupsByToken = new Map<string, Set<IndexedFeedbackGroup>>();
  for (const item of feedback) {
    if (!item.note) continue;
    const key = item.note
      .toLocaleLowerCase()
      .replace(/[^\p{L}\p{N}]+/gu, " ")
      .trim();
    const tokens = new Set(key.split(" "));
    const candidates = new Map<IndexedFeedbackGroup, number>();
    for (const token of tokens) {
      for (const candidate of groupsByToken.get(token) ?? []) {
        candidates.set(candidate, (candidates.get(candidate) ?? 0) + 1);
      }
    }
    let group =
      groupsByKey.get(key) ??
      [...candidates].find(
        ([candidate, shared]) =>
          shared >= 2 &&
          candidate.tokenSets.some((candidateTokens) => {
            const memberShared = [...tokens].filter((token) =>
              candidateTokens.has(token),
            ).length;
            return (
              memberShared >= 2 &&
              (2 * memberShared) / (tokens.size + candidateTokens.size) >= 0.7
            );
          }),
      )?.[0];
    if (group) {
      group.items.push(item);
      group.tokenSets.push(tokens);
    } else {
      group = { key, note: item.note, items: [item], tokenSets: [tokens] };
      groups.push(group);
    }
    groupsByKey.set(key, group);
    for (const token of tokens) {
      const indexed = groupsByToken.get(token) ?? new Set();
      indexed.add(group);
      groupsByToken.set(token, indexed);
    }
  }
  return groups;
}

export function SkillFeedbackSection({
  skillId,
  projectId,
}: {
  skillId: string;
  projectId: string;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const query = useSkillFeedbackInfinite(
    { id: skillId, limit: 50 },
    undefined,
    {
      enabled: open,
      throwOnError: false,
    },
  );
  const trigger = useTriggerSkillSuggestionMutation();
  const result = query.data?.pages[0]?.result;
  const groups = useMemo(
    () =>
      groupFeedback(
        query.data?.pages.flatMap((page) => page.result.feedback) ?? [],
      ),
    [query.data?.pages],
  );

  const triggerSuggestion = async (): Promise<void> => {
    try {
      await trigger.mutateAsync({
        request: {
          triggerSkillSuggestionRequestBody: { id: skillId },
        },
      });
      toast.success("Suggestion analysis queued");
    } catch {
      // The mutation error is rendered next to the action.
    }
  };

  return (
    <SettingsSection>
      <Collapsible open={open} onOpenChange={setOpen}>
        <SettingsSection.Panel>
          <CollapsibleTrigger className="hover:bg-muted/30 flex w-full items-center justify-between gap-4 p-5 text-left">
            <span className="block">
              <Text as="span" variant="subheading" className="block">
                All agent reviews
              </Text>
              <Text as="span" small muted className="block">
                See collection health, recurring findings, and the evidence used
                to improve this skill.
              </Text>
            </span>
            <Icon
              name="chevron-right"
              className={cn(
                "text-muted-foreground h-4 w-4 transition-transform",
                open && "rotate-90",
              )}
            />
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className="space-y-6 border-t p-5">
              {query.isPending && (
                <div className="space-y-3" aria-label="Loading agent feedback">
                  <Skeleton className="h-24 w-full" />
                  <Skeleton className="h-40 w-full" />
                </div>
              )}
              {query.error && !query.data && (
                <div className="space-y-2">
                  <ErrorAlert
                    title="Unable to load agent feedback"
                    error={query.error}
                  />
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => void query.refetch()}
                  >
                    Retry
                  </Button>
                </div>
              )}
              {result && (
                <FeedbackOverview
                  counts={result.counts}
                  metrics={result.metrics}
                  timeline={result.timeline}
                  groups={groups}
                  hasMore={query.hasNextPage}
                  loadingMore={query.isFetchingNextPage}
                  onLoadMore={() => void query.fetchNextPage()}
                  projectId={projectId}
                  triggering={trigger.isPending}
                  triggerError={trigger.error}
                  onTrigger={() => void triggerSuggestion()}
                />
              )}
            </div>
          </CollapsibleContent>
        </SettingsSection.Panel>
      </Collapsible>
    </SettingsSection>
  );
}

function FeedbackOverview({
  counts,
  metrics,
  timeline,
  groups,
  hasMore,
  loadingMore,
  onLoadMore,
  projectId,
  triggering,
  triggerError,
  onTrigger,
}: {
  counts: SkillFeedbackCounts;
  metrics: SkillFeedbackMetrics;
  timeline: SkillFeedbackTimelinePoint[];
  groups: FeedbackGroup[];
  hasMore: boolean;
  loadingMore: boolean;
  onLoadMore: () => void;
  projectId: string;
  triggering: boolean;
  triggerError: Error | null;
  onTrigger: () => void;
}): JSX.Element {
  const coverage = percentage(
    metrics.feedbackActivationsInWindow,
    metrics.activationsInWindow,
  );
  const conversion = percentage(metrics.converted, counts.total);

  return (
    <>
      <StatTileGroup>
        <StatTile
          title="30-day feedback"
          value={metrics.feedbackInWindow}
          tone="information"
          format="number"
          subtext="Reports collected"
        />
        <StatTile
          title="Unreviewed"
          value={metrics.unreviewed}
          tone={metrics.unreviewed > 0 ? "warning" : "neutral"}
          format="number"
          subtext="Awaiting suggestion analysis"
        />
        <StatTile
          title="Activation coverage"
          value={coverage}
          tone="success"
          format="percent"
          displayValue={
            metrics.activationsInWindow === 0
              ? "N/A"
              : `${coverage.toFixed(1)}%`
          }
          subtext={`${metrics.feedbackActivationsInWindow.toLocaleString()} of ${metrics.activationsInWindow.toLocaleString()} activations produced feedback`}
        />
        <StatTile
          title="Suggestion conversion"
          value={conversion}
          tone="success"
          format="percent"
          displayValue={
            counts.total === 0 ? "N/A" : `${conversion.toFixed(1)}%`
          }
          subtext={`${metrics.converted.toLocaleString()} of ${counts.total.toLocaleString()} reports cited`}
        />
      </StatTileGroup>

      <OutcomeDistribution counts={counts} />
      <FeedbackTimeline timeline={timeline} />

      <div className="bg-muted/20 flex flex-wrap items-center justify-between gap-3 border p-4">
        <div>
          <Text variant="subheading">Turn reviews into an edit</Text>
          <Text small muted>
            Run analysis now using unresolved reviews and efficacy evidence.
          </Text>
        </div>
        <RequireScope
          scope="skill:write"
          resourceId={projectId}
          level="component"
          reason="You need write access to run suggestion analysis."
        >
          <Button size="sm" onClick={onTrigger} disabled={triggering}>
            {triggering ? "Queueing..." : "Generate suggestion"}
          </Button>
        </RequireScope>
      </div>
      {triggerError && (
        <ErrorAlert
          title="Unable to queue suggestion analysis"
          error={triggerError}
        />
      )}

      <GroupedFindings
        groups={groups}
        hasMore={hasMore}
        loadingMore={loadingMore}
        onLoadMore={onLoadMore}
      />
    </>
  );
}

function OutcomeDistribution({
  counts,
}: {
  counts: SkillFeedbackCounts;
}): JSX.Element {
  return (
    <section className="space-y-3">
      <div>
        <Text variant="subheading">Outcome distribution</Text>
        <Text small muted>
          {counts.total.toLocaleString()} resolved reports, all time
        </Text>
      </div>
      {counts.total === 0 ? (
        <Text small muted>
          No resolved outcomes yet.
        </Text>
      ) : (
        <>
          <div
            className="bg-muted flex h-3 overflow-hidden rounded-full"
            aria-label="Agent review outcome distribution"
          >
            {OUTCOMES.map(({ key, label, className }) => (
              <div
                key={key}
                className={className}
                style={{ width: `${percentage(counts[key], counts.total)}%` }}
                title={`${label}: ${counts[key].toLocaleString()}`}
              />
            ))}
          </div>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-5">
            {OUTCOMES.map(({ key, label, className }) => (
              <div key={key} className="flex items-center gap-2 text-xs">
                <span className={cn("size-2 rounded-full", className)} />
                <span className="text-muted-foreground">{label}</span>
                <span className="ml-auto font-medium tabular-nums">
                  {counts[key].toLocaleString()}
                </span>
              </div>
            ))}
          </div>
        </>
      )}
    </section>
  );
}

function FeedbackTimeline({
  timeline,
}: {
  timeline: SkillFeedbackTimelinePoint[];
}): JSX.Element {
  const maximum = Math.max(1, ...timeline.map((point) => point.feedbackCount));
  return (
    <section className="space-y-3">
      <div>
        <Text variant="subheading">Feedback volume</Text>
        <Text small muted>
          Daily reports over the last 30 days
        </Text>
      </div>
      <div
        className="bg-muted/20 flex h-28 items-end gap-1 border px-3 pt-3"
        aria-label="Daily feedback volume over the last 30 days"
      >
        {timeline.map((point) => (
          <div
            key={point.bucketStart.toISOString()}
            className="bg-primary/70 hover:bg-primary min-h-0.5 flex-1 transition-colors"
            style={{ height: `${percentage(point.feedbackCount, maximum)}%` }}
            title={`${dateTimeFormatters.monthDay.format(point.bucketStart)}: ${point.feedbackCount.toLocaleString()} reports`}
          />
        ))}
      </div>
    </section>
  );
}

function GroupedFindings({
  groups,
  hasMore,
  loadingMore,
  onLoadMore,
}: {
  groups: FeedbackGroup[];
  hasMore: boolean;
  loadingMore: boolean;
  onLoadMore: () => void;
}): JSX.Element {
  return (
    <section className="space-y-3">
      <div>
        <Text variant="subheading">Recurring findings</Text>
        <Text small muted>
          Similar recent notes are grouped. Expand a finding for its full
          evidence and attribution.
        </Text>
      </div>
      {groups.length === 0 ? (
        <Text small muted>
          No notes among recent feedback.
        </Text>
      ) : (
        <div className="divide-y overflow-hidden border">
          {groups.map((group) => (
            <details key={group.key} className="group">
              <summary className="hover:bg-muted/30 flex cursor-pointer list-none items-start gap-3 p-4">
                <Icon
                  name="chevron-right"
                  className="text-muted-foreground mt-0.5 size-4 shrink-0 transition-transform group-open:rotate-90"
                />
                <Text small className="min-w-0 flex-1">
                  {group.note}
                </Text>
                <Badge variant="neutral">{group.items.length}</Badge>
              </summary>
              <div className="bg-muted/15 divide-y border-t">
                {group.items.map((feedback) => (
                  <div key={feedback.id} className="space-y-2 px-5 py-3 pl-11">
                    <div className="flex flex-wrap items-center gap-2">
                      <Badge variant="neutral">
                        {feedback.outcome.replaceAll("_", " ")}
                      </Badge>
                      <Badge variant="neutral">
                        {feedback.source === "dev" ? "Developer" : "Assistant"}
                      </Badge>
                      <Text small muted>
                        {feedback.skillVersionId
                          ? `Version ${feedback.skillVersionId.slice(0, 8)}`
                          : "Version unknown"}
                      </Text>
                      <Text
                        small
                        muted
                        title={dateTimeFormatters.full.format(
                          feedback.createdAt,
                        )}
                      >
                        <HumanizeDateTime date={feedback.createdAt} />
                      </Text>
                    </div>
                    <Text small>{feedback.note}</Text>
                  </div>
                ))}
              </div>
            </details>
          ))}
        </div>
      )}
      {hasMore && (
        <div className="flex justify-center">
          <Button
            size="sm"
            variant="secondary"
            disabled={loadingMore}
            onClick={onLoadMore}
          >
            {loadingMore ? "Loading..." : "Load more reviews"}
          </Button>
        </div>
      )}
    </section>
  );
}
