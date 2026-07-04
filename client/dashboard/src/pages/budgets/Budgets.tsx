import { MetricCard } from "@/components/chart/MetricCard";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/button";
import { SegmentedControl } from "@/components/ui/segmented-control";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  useSpendRulesCreateRuleMutation,
  useSpendRulesDeleteRuleMutation,
  useSpendRulesListEvents,
  useSpendRulesListRules,
  useSpendRulesOverview,
  useSpendRulesUpdateRuleMutation,
} from "@gram/client/react-query/index.js";
import { Table, type Column } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Inbox, Plus, Wallet } from "lucide-react";
import { useMemo, useState, type JSX } from "react";
import { Navigate } from "react-router";
import { toast } from "sonner";
import { RuleDetailSheet } from "./RuleDetailSheet";
import { RuleSheet } from "./RuleSheet";
import {
  EventTypeBadge,
  RuleActionBadge,
  RuleStatusBadge,
  TabEmptyState,
  UsageBar,
} from "./budget-shared";
import {
  WINDOW_LABELS,
  formatUsd,
  invalidateSpendControlQueries,
  parseRuleUrn,
  ruleStatusOf,
  targetSummary,
  usageByRuleId,
  type RuleAction,
  type RuleDraft,
  type SpendRule,
  type SpendRuleEvent,
  type SpendRulesOverviewResult,
  type SpendRuleUsage,
} from "./budgets-data";

type ActionFilter = "all" | RuleAction;
type BudgetTab = "rules" | "events";

export default function Budgets(): JSX.Element {
  const telemetry = useTelemetry();
  const routes = useRoutes();
  // Gated behind a PostHog flag so it can be dogfooded per org/user.
  const enabled = telemetry.isFeatureEnabled("gram-budgets-page") ?? false;

  if (!enabled) {
    return <Navigate to={routes.home.href()} replace />;
  }

  return (
    <RequireScope scope={["project:read", "project:write"]} level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <BudgetsContent />
        </Page.Body>
      </Page>
    </RequireScope>
  );
}

function BudgetsContent(): JSX.Element {
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<BudgetTab>("rules");
  const [createOpen, setCreateOpen] = useState(false);
  const [viewing, setViewing] = useState<SpendRule | null>(null);
  const [editing, setEditing] = useState<SpendRule | null>(null);

  const { data: rulesData, isLoading: rulesLoading } = useSpendRulesListRules();
  const { data: overview } = useSpendRulesOverview();
  const rules = useMemo(() => rulesData?.rules ?? [], [rulesData]);
  const usageMap = useMemo(() => usageByRuleId(overview?.rules), [overview]);

  const invalidate = () => invalidateSpendControlQueries(queryClient);

  const createMutation = useSpendRulesCreateRuleMutation({
    onSuccess: () => {
      invalidate();
      setCreateOpen(false);
      toast.success("Rule created");
    },
    onError: (error) => toast.error(error.message),
  });

  const updateMutation = useSpendRulesUpdateRuleMutation({
    onSuccess: () => {
      invalidate();
      setEditing(null);
      toast.success("Rule updated");
    },
    onError: (error) => toast.error(error.message),
  });

  const deleteMutation = useSpendRulesDeleteRuleMutation({
    onSuccess: () => {
      invalidate();
      setEditing(null);
      toast.success("Rule deleted");
    },
    onError: (error) => toast.error(error.message),
  });

  const handleCreate = (draft: RuleDraft) => {
    createMutation.mutate({
      request: { createSpendRuleRequestBody: draft },
    });
  };

  const handleUpdate = (draft: RuleDraft) => {
    if (!editing) return;
    updateMutation.mutate({
      request: {
        updateSpendRuleRequestBody: { id: editing.id, ...draft },
      },
    });
  };

  const handleDelete = () => {
    if (!editing) return;
    deleteMutation.mutate({ request: { id: editing.id } });
  };

  const handleToggle = (rule: SpendRule, on: boolean) => {
    updateMutation.mutate({
      request: {
        updateSpendRuleRequestBody: { id: rule.id, enabled: on },
      },
    });
  };

  return (
    <>
      <Page.Section>
        <Page.Section.Title stage="preview">Spend Control</Page.Section.Title>
        <Page.Section.Description>
          Give each matched person a fixed-window AI budget. Flag overspend for
          review, or block requests until the window resets. The strictest
          matching rule wins.
        </Page.Section.Description>
        <Page.Section.CTA>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            New rule
          </Button>
        </Page.Section.CTA>
        <Page.Section.Body>
          <div className="space-y-6">
            {rules.length > 0 && overview && (
              <StatusSummaryCards overview={overview} />
            )}

            <Tabs
              value={activeTab}
              onValueChange={(value) => setActiveTab(value as BudgetTab)}
            >
              <div className="border-b">
                <TabsList className="h-auto justify-start gap-4 rounded-none bg-transparent p-0 text-sm">
                  <PageTabsTrigger value="rules">Rules</PageTabsTrigger>
                  <PageTabsTrigger value="events">Events</PageTabsTrigger>
                </TabsList>
              </div>
              <TabsContent value="rules" className="mt-6">
                <RulesTab
                  rules={rules}
                  loading={rulesLoading}
                  usageMap={usageMap}
                  onNew={() => setCreateOpen(true)}
                  onView={setViewing}
                  onToggle={handleToggle}
                />
              </TabsContent>
              <TabsContent value="events" className="mt-6">
                <EventsTab rules={rules} />
              </TabsContent>
            </Tabs>
          </div>
        </Page.Section.Body>
      </Page.Section>

      <RuleDetailSheet
        rule={viewing}
        usage={viewing ? usageMap.get(viewing.id) : undefined}
        onClose={() => setViewing(null)}
        onEdit={(rule) => {
          setViewing(null);
          setEditing(rule);
        }}
      />
      <RuleSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSubmit={handleCreate}
        submitting={createMutation.isPending}
      />
      <RuleSheet
        open={editing !== null}
        onOpenChange={(open) => {
          if (!open) setEditing(null);
        }}
        rule={editing ?? undefined}
        onSubmit={handleUpdate}
        onDelete={handleDelete}
        submitting={updateMutation.isPending || deleteMutation.isPending}
      />
    </>
  );
}

/** Card-sized dollar amounts: "$7.9K" instead of "$7,922". */
function compactUsd(amount: number): string {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(amount);
}

/** At-a-glance rollup an admin scans before reading any table. Every card is
 *  a ratio of the same shape: how much of what we govern is in trouble. */
function StatusSummaryCards({
  overview,
}: {
  overview: SpendRulesOverviewResult;
}): JSX.Element {
  const spendPct =
    overview.totalBudgetUsd > 0
      ? Math.round((overview.totalSpendUsd / overview.totalBudgetUsd) * 100)
      : 0;

  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <MetricCard
        title="Spend vs budget"
        value={overview.totalSpendUsd}
        displayValue={`${compactUsd(overview.totalSpendUsd)} / ${compactUsd(overview.totalBudgetUsd)}`}
        format="number"
        subtext={`${spendPct}% of budgeted spend used across enabled rules`}
      />
      <MetricCard
        title="Users over budget"
        value={overview.usersBreached}
        displayValue={`${overview.usersBreached} / ${overview.usersTotal}`}
        format="number"
        subtext={
          overview.usersBreached === 0
            ? "no budgets breached"
            : "people at or past a per-person limit"
        }
      />
      <MetricCard
        title="Rules needing attention"
        value={overview.rulesUnhealthy}
        displayValue={`${overview.rulesUnhealthy} / ${overview.rulesTotal}`}
        format="number"
        subtext={
          overview.rulesUnhealthy === 0
            ? "all rules healthy"
            : "rules approaching, flagging, or blocking"
        }
      />
      <MetricCard
        title="Projected overrun"
        value={overview.projectedOverrunUsd}
        displayValue={formatUsd(overview.projectedOverrunUsd)}
        format="number"
        tooltip="Estimated end-of-window spend past budget, extrapolated from spend so far across enabled rules."
        subtext={
          overview.projectedOverrunUsd === 0
            ? "every budget on pace for its window"
            : "spend past budget at the current pace"
        }
      />
    </div>
  );
}

function RulesTab({
  rules,
  loading,
  usageMap,
  onNew,
  onView,
  onToggle,
}: {
  rules: SpendRule[];
  loading: boolean;
  usageMap: Map<string, SpendRuleUsage>;
  onNew: () => void;
  onView: (rule: SpendRule) => void;
  onToggle: (rule: SpendRule, on: boolean) => void;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const [actionFilter, setActionFilter] = useState<ActionFilter>("all");

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return rules.filter((r) => {
      if (actionFilter !== "all" && r.action !== actionFilter) {
        return false;
      }
      if (!q) return true;
      return (
        r.name.toLowerCase().includes(q) ||
        r.description.toLowerCase().includes(q) ||
        targetSummary(r.target).toLowerCase().includes(q)
      );
    });
  }, [rules, query, actionFilter]);

  const columns = useMemo<Column<SpendRule>[]>(
    () => buildRuleColumns({ usageMap, onToggle }),
    [usageMap, onToggle],
  );

  if (loading) {
    return <SkeletonTable />;
  }

  if (rules.length === 0) {
    return (
      <TabEmptyState
        icon={Wallet}
        title="No spend rules"
        description="Spend rules give each matched person a fixed-window AI budget — flag overspend for review, or block requests until the window resets. Create your first rule to get started."
        action={
          <Button onClick={onNew}>
            <Plus className="mr-2 h-4 w-4" />
            New rule
          </Button>
        }
      />
    );
  }

  return (
    <div className="space-y-4">
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={query}
          onChange={setQuery}
          placeholder="Search rules"
          debounceMs={150}
        />
        <Page.Toolbar.Count>
          {filtered.length} of {rules.length} rules
        </Page.Toolbar.Count>
        <Page.Toolbar.Actions>
          <SegmentedControl<ActionFilter>
            value={actionFilter}
            onChange={setActionFilter}
            options={[
              { value: "all", label: "All" },
              { value: "flag", label: "Flag" },
              { value: "block", label: "Block" },
            ]}
          />
        </Page.Toolbar.Actions>
      </Page.Toolbar>

      <Table
        columns={columns}
        data={filtered}
        rowKey={(rule) => rule.id}
        onRowClick={(rule) => onView(rule)}
        noResultsMessage={<Type>No rules match your filters.</Type>}
      />
    </div>
  );
}

function buildRuleColumns({
  usageMap,
  onToggle,
}: {
  usageMap: Map<string, SpendRuleUsage>;
  onToggle: (rule: SpendRule, on: boolean) => void;
}): Column<SpendRule>[] {
  const dim = (rule: SpendRule) => (rule.enabled ? "" : "opacity-50");

  return [
    {
      key: "name",
      header: "Name",
      width: "1.4fr",
      render: (rule) => (
        <span className={cn("block min-w-0", dim(rule))}>
          <span className="block truncate font-medium">{rule.name}</span>
          <span className="text-muted-foreground block truncate text-xs">
            {targetSummary(rule.target)}
          </span>
        </span>
      ),
    },
    {
      key: "window",
      header: "Window",
      width: "0.6fr",
      render: (rule) => (
        <span className={cn("text-muted-foreground text-sm", dim(rule))}>
          {WINDOW_LABELS[rule.windowKind]}
        </span>
      ),
    },
    {
      key: "budget",
      header: "Budget",
      width: "220px",
      render: (rule) => <RuleBudgetCell rule={rule} usageMap={usageMap} />,
    },
    {
      key: "status",
      header: "Status",
      width: "0.8fr",
      render: (rule) => (
        <RuleStatusBadge status={ruleStatusOf(rule, usageMap.get(rule.id))} />
      ),
    },
    {
      key: "action",
      header: "Action",
      width: "0.6fr",
      render: (rule) => (
        <span className={cn("inline-flex", dim(rule))}>
          <RuleActionBadge action={rule.action} />
        </span>
      ),
    },
    {
      key: "enabled",
      header: "Enabled",
      width: "0.4fr",
      render: (rule) => (
        <div onClick={(e) => e.stopPropagation()}>
          <Switch
            checked={rule.enabled}
            onCheckedChange={(checked) => onToggle(rule, checked)}
            aria-label={`Enable ${rule.name}`}
          />
        </div>
      ),
    },
  ];
}

/** Aggregate current-window spend across matched people vs the total budget
 *  (per-person limit × matched people). Disabled rules have no live usage —
 *  show the per-person limit instead. */
function RuleBudgetCell({
  rule,
  usageMap,
}: {
  rule: SpendRule;
  usageMap: Map<string, SpendRuleUsage>;
}): JSX.Element {
  const usage = rule.enabled ? usageMap.get(rule.id) : undefined;
  if (!usage) {
    return (
      <span
        className={cn(
          "text-muted-foreground text-sm",
          !rule.enabled && "opacity-50",
        )}
      >
        {formatUsd(rule.limitUsd)}/person
      </span>
    );
  }
  return (
    <span className="block">
      <UsageBar
        spendUsd={usage.spendUsd}
        limitUsd={usage.budgetUsd}
        warnAtPct={rule.warnAtPct}
      />
      <span className="text-muted-foreground block text-xs">
        {formatUsd(rule.limitUsd)}/person · {usage.matchedUsers}{" "}
        {usage.matchedUsers === 1 ? "person" : "people"}
      </span>
    </span>
  );
}

type EventFilter = "all" | "warning" | "breach";

function EventsTab({ rules }: { rules: SpendRule[] }): JSX.Element {
  const [filter, setFilter] = useState<EventFilter>("all");

  const { data, isLoading } = useSpendRulesListEvents({
    eventType: filter === "all" ? undefined : filter,
    limit: 200,
  });
  const events = useMemo(() => data?.events ?? [], [data]);

  const versionByRuleId = useMemo(() => {
    const map = new Map<string, number>();
    for (const rule of rules) map.set(rule.id, Number(rule.version));
    return map;
  }, [rules]);

  const columns = useMemo<Column<SpendRuleEvent>[]>(
    () => [
      {
        key: "time",
        header: "Timestamp",
        width: "170px",
        render: (event) => (
          <span className="text-muted-foreground font-mono text-xs">
            {event.createdAt.toLocaleString()}
          </span>
        ),
      },
      {
        key: "type",
        header: "Event",
        width: "160px",
        render: (event) => <EventTypeBadge type={event.eventType} />,
      },
      {
        key: "rule",
        header: "Rule",
        width: "1fr",
        render: (event) => (
          <EventRuleCell event={event} versionByRuleId={versionByRuleId} />
        ),
      },
      {
        key: "person",
        header: "Person",
        width: "1fr",
        render: (event) => <EventPersonCell event={event} />,
      },
      {
        key: "spend",
        header: "Spend",
        width: "150px",
        render: (event) => <EventSpendCell event={event} />,
      },
    ],
    [versionByRuleId],
  );

  if (isLoading) {
    return <SkeletonTable />;
  }

  // A filtered-empty list keeps the toolbar so the filter can be changed;
  // a truly empty history gets the full empty-state card instead of a bare
  // table row.
  if (events.length === 0 && filter === "all") {
    return (
      <TabEmptyState
        icon={Inbox}
        title="No budget events"
        description={
          rules.length === 0
            ? "Create a spend rule first — warnings and breaches are recorded here as people approach or exceed their budgets."
            : "Warnings and breaches appear here as enabled rules evaluate each person's spend against their budget."
        }
      />
    );
  }

  return (
    <div className="space-y-3">
      <Page.Toolbar>
        <Page.Toolbar.Count>
          {events.length} {events.length === 1 ? "event" : "events"}
        </Page.Toolbar.Count>
        <Page.Toolbar.Actions>
          <SegmentedControl<EventFilter>
            value={filter}
            onChange={setFilter}
            options={[
              { value: "all", label: "All" },
              { value: "warning", label: "Warnings" },
              { value: "breach", label: "Breaches" },
            ]}
          />
        </Page.Toolbar.Actions>
      </Page.Toolbar>

      <Table
        columns={columns}
        data={events}
        rowKey={(event) => event.id}
        noResultsMessage={<Type>No budget events match this filter.</Type>}
      />
    </div>
  );
}

/** Rule name as recorded on the event, plus a version marker whenever the
 *  event fired under a config that is no longer live — an older version, or a
 *  rule that has since been deleted. The full URN sits in the hover title. */
function EventRuleCell({
  event,
  versionByRuleId,
}: {
  event: SpendRuleEvent;
  versionByRuleId: Map<string, number>;
}): JSX.Element {
  const ref = parseRuleUrn(event.ruleUrn);
  const currentVersion = versionByRuleId.get(event.ruleId);
  const marker = versionMarker(ref, currentVersion);

  return (
    <span className="block min-w-0" title={event.ruleUrn}>
      <span className="block truncate text-sm">{event.ruleName}</span>
      {marker && (
        <span className="text-muted-foreground block truncate font-mono text-xs">
          {marker}
        </span>
      )}
    </span>
  );
}

function versionMarker(
  ref: { slug: string; version: number } | null,
  currentVersion: number | undefined,
): string | null {
  if (ref === null) return null;
  if (currentVersion === undefined) return `v${ref.version} · rule deleted`;
  if (currentVersion !== ref.version) {
    return `v${ref.version} · now v${currentVersion}`;
  }
  return null;
}

function EventPersonCell({ event }: { event: SpendRuleEvent }): JSX.Element {
  return (
    <span className="block min-w-0">
      <span className="block truncate text-sm">
        {event.displayName || event.email}
      </span>
      {event.displayName && (
        <span className="text-muted-foreground block truncate text-xs">
          {event.email}
        </span>
      )}
    </span>
  );
}

function EventSpendCell({ event }: { event: SpendRuleEvent }): JSX.Element {
  const over = event.spendUsd >= event.limitUsd;
  return (
    <span className="text-sm whitespace-nowrap">
      <span className={cn(over && "text-destructive font-medium")}>
        {formatUsd(event.spendUsd)}
      </span>
      <span className="text-muted-foreground">
        {" "}
        of {formatUsd(event.limitUsd)}
      </span>
    </span>
  );
}
