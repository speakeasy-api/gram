import { MetricCard } from "@/components/chart/MetricCard";
import { EmptyState, Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/button";
import { SegmentedControl } from "@/components/ui/segmented-control";
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
import { Table, type Column } from "@speakeasy-api/moonshine";
import { Plus, Wallet } from "lucide-react";
import { useMemo, useState, type JSX } from "react";
import { Navigate } from "react-router";
import { toast } from "sonner";
import { RuleDetailSheet } from "./RuleDetailSheet";
import { RuleSheet } from "./RuleSheet";
import {
  EventTypeBadge,
  RuleActionBadge,
  RuleStatusBadge,
  UsageBar,
} from "./budget-shared";
import {
  DIRECTORY_USER_COUNT,
  WINDOW_LABELS,
  coveredSpendSummary,
  estimateRuleUsage,
  formatUsd,
  parseRuleUrn,
  ruleStatus,
  targetSummary,
  useBudgetStore,
  type RuleAction,
  type RuleDraft,
  type RuleStatus,
  type RuleUsage,
  type SpendControlEvent,
  type SpendRule,
} from "./budgets-data";

type ActionFilter = "all" | RuleAction;
type BudgetTab = "rules" | "events";

export default function Budgets(): JSX.Element {
  const telemetry = useTelemetry();
  const routes = useRoutes();
  // Prototype: gate behind a PostHog flag so it can be dogfooded per org/user.
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
  const { rules, addRule, updateRule, removeRule } = useBudgetStore();
  const [activeTab, setActiveTab] = useState<BudgetTab>("rules");
  const [createOpen, setCreateOpen] = useState(false);
  const [viewing, setViewing] = useState<SpendRule | null>(null);
  const [editing, setEditing] = useState<SpendRule | null>(null);

  const usageByRule = useMemo(() => {
    const map = new Map<string, RuleUsage>();
    for (const rule of rules) {
      map.set(rule.id, estimateRuleUsage(rule));
    }
    return map;
  }, [rules]);

  const statusByRule = useMemo(() => {
    const map = new Map<string, RuleStatus | null>();
    for (const rule of rules) {
      const usage = usageByRule.get(rule.id);
      map.set(rule.id, usage ? ruleStatus(rule, usage) : null);
    }
    return map;
  }, [rules, usageByRule]);

  const handleCreate = (draft: RuleDraft) => {
    addRule(draft);
    setCreateOpen(false);
    toast.success("Rule created");
  };

  const handleUpdate = (draft: RuleDraft) => {
    if (!editing) return;
    updateRule(editing.id, draft);
    setEditing(null);
    toast.success("Rule updated");
  };

  const handleDelete = () => {
    if (!editing) return;
    removeRule(editing.id);
    setEditing(null);
    toast.success("Rule deleted");
  };

  return (
    <>
      <Page.Section>
        <Page.Section.Title stage="preview">Spend Control</Page.Section.Title>
        <Page.Section.Description>
          Give teams fixed-window AI budgets. Flag overspend for review, or
          block requests until the window resets. The strictest matching rule
          wins.
        </Page.Section.Description>
        <Page.Section.CTA>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            New rule
          </Button>
        </Page.Section.CTA>
        <Page.Section.Body>
          <div className="space-y-6">
            {rules.length > 0 && (
              <StatusSummaryCards
                rules={rules}
                usageByRule={usageByRule}
                statusByRule={statusByRule}
              />
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
                  usageByRule={usageByRule}
                  statusByRule={statusByRule}
                  onNew={() => setCreateOpen(true)}
                  onView={setViewing}
                  onToggle={(rule, on) => updateRule(rule.id, { enabled: on })}
                />
              </TabsContent>
              <TabsContent value="events" className="mt-6">
                <EventsTab />
              </TabsContent>
            </Tabs>
          </div>
        </Page.Section.Body>
      </Page.Section>

      <RuleDetailSheet
        rule={viewing}
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
      />
      <RuleSheet
        open={editing !== null}
        onOpenChange={(open) => {
          if (!open) setEditing(null);
        }}
        rule={editing ?? undefined}
        onSubmit={handleUpdate}
        onDelete={handleDelete}
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
  rules,
  usageByRule,
  statusByRule,
}: {
  rules: SpendRule[];
  usageByRule: Map<string, RuleUsage>;
  statusByRule: Map<string, RuleStatus | null>;
}): JSX.Element {
  const spend = coveredSpendSummary(rules);
  const spendPct =
    spend.budgetUsd > 0
      ? Math.round((spend.currentUsd / spend.budgetUsd) * 100)
      : 0;

  // People under a breached budget, split by what happens to their requests.
  // Someone who is both blocked and flagged counts as blocked.
  const blockedPeople = new Set<string>();
  const flaggedPeople = new Set<string>();
  for (const rule of rules) {
    const status = statusByRule.get(rule.id);
    if (status !== "blocking" && status !== "flagging") continue;
    const bucket = status === "blocking" ? blockedPeople : flaggedPeople;
    for (const actor of usageByRule.get(rule.id)?.matched ?? []) {
      bucket.add(actor.id);
    }
  }
  for (const id of blockedPeople) flaggedPeople.delete(id);
  const breachedPeopleCount = blockedPeople.size + flaggedPeople.size;

  const statusCounts = { approaching: 0, flagging: 0, blocking: 0 };
  for (const rule of rules) {
    const status = statusByRule.get(rule.id);
    if (status && status !== "healthy") statusCounts[status]++;
  }
  const unhealthyCount =
    statusCounts.approaching + statusCounts.flagging + statusCounts.blocking;
  const unhealthyBreakdown = (["blocking", "flagging", "approaching"] as const)
    .filter((status) => statusCounts[status] > 0)
    .map((status) => `${statusCounts[status]} ${status}`)
    .join(" · ");

  // Projected end-of-window overspend, and the rule contributing the most to
  // it. Only flag rules can overrun: a block rule's circuit opens at the
  // limit, so spend past it never happens.
  let overrunUsd = 0;
  let topOverrun: { rule: SpendRule; amountUsd: number } | null = null;
  for (const rule of rules) {
    if (!rule.enabled || rule.action !== "flag") continue;
    const usage = usageByRule.get(rule.id);
    if (!usage || usage.matched === null) continue;
    const amountUsd = Math.max(0, usage.projectedSpendUsd - rule.limitUsd);
    if (amountUsd <= 0) continue;
    overrunUsd += amountUsd;
    if (!topOverrun || amountUsd > topOverrun.amountUsd) {
      topOverrun = { rule, amountUsd };
    }
  }

  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <MetricCard
        title="Spend vs budget"
        value={spend.currentUsd}
        displayValue={`${compactUsd(spend.currentUsd)} / ${compactUsd(spend.budgetUsd)}`}
        format="number"
        subtext={`${spendPct}% of budgeted spend used · covers ${spend.peopleCount} of ${DIRECTORY_USER_COUNT} people`}
      />
      <MetricCard
        title="Users over budget"
        value={breachedPeopleCount}
        displayValue={`${breachedPeopleCount} / ${DIRECTORY_USER_COUNT}`}
        format="number"
        subtext={
          breachedPeopleCount === 0
            ? "no budgets breached"
            : `${blockedPeople.size} blocked · ${flaggedPeople.size} flagged`
        }
      />
      <MetricCard
        title="Rules needing attention"
        value={unhealthyCount}
        displayValue={`${unhealthyCount} / ${rules.length}`}
        format="number"
        subtext={
          unhealthyCount === 0 ? "all rules healthy" : unhealthyBreakdown
        }
      />
      <MetricCard
        title="Projected overrun"
        value={overrunUsd}
        displayValue={formatUsd(overrunUsd)}
        format="number"
        tooltip="Estimated end-of-window spend past the limit, across flag rules. Block rules can't overrun — their circuit opens at the limit."
        subtext={
          topOverrun
            ? `${topOverrun.rule.name} drives ${formatUsd(topOverrun.amountUsd)}`
            : "every budget on pace for its window"
        }
      />
    </div>
  );
}

function RulesTab({
  rules,
  usageByRule,
  statusByRule,
  onNew,
  onView,
  onToggle,
}: {
  rules: SpendRule[];
  usageByRule: Map<string, RuleUsage>;
  statusByRule: Map<string, RuleStatus | null>;
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
        r.targetExpr.toLowerCase().includes(q)
      );
    });
  }, [rules, query, actionFilter]);

  const columns = useMemo<Column<SpendRule>[]>(
    () => buildRuleColumns({ usageByRule, statusByRule, onToggle }),
    [usageByRule, statusByRule, onToggle],
  );

  if (rules.length === 0) {
    return (
      <EmptyState
        heading="No spend rules yet"
        description="Create a rule to cap AI spend for a group of people."
        graphic={<Wallet className="text-muted-foreground size-16" />}
        nonEmptyProjectCTA={
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
  usageByRule,
  statusByRule,
  onToggle,
}: {
  usageByRule: Map<string, RuleUsage>;
  statusByRule: Map<string, RuleStatus | null>;
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
            {targetSummary(rule.targetExpr)}
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
          {WINDOW_LABELS[rule.window]}
        </span>
      ),
    },
    {
      key: "budget",
      header: "Budget",
      width: "220px",
      render: (rule) => {
        const usage = usageByRule.get(rule.id);
        if (!usage) return null;
        return (
          <span className={cn("block", dim(rule))}>
            <UsageBar
              usage={usage}
              limitUsd={rule.limitUsd}
              warnAtPct={rule.warnAtPct}
            />
          </span>
        );
      },
    },
    {
      key: "status",
      header: "Status",
      width: "0.8fr",
      render: (rule) => (
        <RuleStatusBadge status={statusByRule.get(rule.id) ?? null} />
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

type EventFilter = "all" | "warning" | "breach";

function eventMatchesFilter(
  event: SpendControlEvent,
  filter: EventFilter,
): boolean {
  switch (filter) {
    case "all":
      return true;
    case "warning":
      return event.type === "warning";
    case "breach":
      return event.type === "breach";
  }
}

function EventsTab(): JSX.Element {
  const { rules, events } = useBudgetStore();
  const [filter, setFilter] = useState<EventFilter>("all");

  const versionByRuleId = useMemo(() => {
    const map = new Map<string, number>();
    for (const rule of rules) map.set(rule.id, rule.version);
    return map;
  }, [rules]);

  const filtered = useMemo(
    () =>
      events
        .filter((event) => eventMatchesFilter(event, filter))
        .sort(
          (a, b) =>
            new Date(b.occurredAt).getTime() - new Date(a.occurredAt).getTime(),
        ),
    [events, filter],
  );

  const columns = useMemo<Column<SpendControlEvent>[]>(
    () => [
      {
        key: "time",
        header: "Timestamp",
        width: "170px",
        render: (event) => (
          <span className="text-muted-foreground font-mono text-xs">
            {new Date(event.occurredAt).toLocaleString()}
          </span>
        ),
      },
      {
        key: "type",
        header: "Event",
        width: "160px",
        render: (event) => <EventTypeBadge type={event.type} />,
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
        key: "summary",
        header: "What happened",
        width: "2fr",
        render: (event) => (
          <span className="text-muted-foreground block text-xs">
            {event.summary}
          </span>
        ),
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

  return (
    <div className="space-y-3">
      <Page.Toolbar>
        <Page.Toolbar.Count>
          {filtered.length} {filtered.length === 1 ? "event" : "events"}
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
        data={filtered}
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
  event: SpendControlEvent;
  versionByRuleId: Map<string, number>;
}): JSX.Element {
  const ref = parseRuleUrn(event.ruleUrn);
  const currentVersion = ref ? versionByRuleId.get(ref.id) : undefined;
  const marker =
    ref === null
      ? null
      : currentVersion === undefined
        ? `v${ref.version} · rule deleted`
        : currentVersion !== ref.version
          ? `v${ref.version} · now v${currentVersion}`
          : null;

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

function EventSpendCell({ event }: { event: SpendControlEvent }): JSX.Element {
  if (event.spendUsd === undefined || event.limitUsd === undefined) {
    return <span className="text-muted-foreground text-sm">—</span>;
  }
  const over = event.spendUsd > event.limitUsd;
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
