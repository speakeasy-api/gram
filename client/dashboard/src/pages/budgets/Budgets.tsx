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
import { useRoutes } from "@/routes";
import { cn } from "@/lib/utils";
import { Table, type Column } from "@speakeasy-api/moonshine";
import { Plus, Wallet } from "lucide-react";
import { useMemo, useState, type JSX } from "react";
import { Navigate } from "react-router";
import { toast } from "sonner";
import { RuleSheet } from "./RuleSheet";
import { BreachActionBadge, UsageBar } from "./budget-shared";
import {
  MODEL_BY_ID,
  SPEND_CONTROL_EVENTS,
  estimateRuleUsage,
  formatUsd,
  scopeSummary,
  targetSummary,
  useBudgetStore,
  WINDOW_LABELS,
  type BreachAction,
  type RuleDraft,
  type RuleUsage,
  type SpendControlEvent,
  type SpendRule,
} from "./budgets-data";

type ActionFilter = "all" | BreachAction;
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
  const [editing, setEditing] = useState<SpendRule | null>(null);

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
          Set budgets for actors, scoped to models and a time window. If a
          request matches multiple rules, the strictest exhausted budget wins
          automatically.
        </Page.Section.Description>
        <Page.Section.CTA>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            New rule
          </Button>
        </Page.Section.CTA>
        <Page.Section.Body>
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
                onNew={() => setCreateOpen(true)}
                onEdit={setEditing}
                onToggle={(rule, on) => updateRule(rule.id, { enabled: on })}
              />
            </TabsContent>
            <TabsContent value="events" className="mt-6">
              <EventsTab />
            </TabsContent>
          </Tabs>
        </Page.Section.Body>
      </Page.Section>

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

function RulesTab({
  rules,
  onNew,
  onEdit,
  onToggle,
}: {
  rules: SpendRule[];
  onNew: () => void;
  onEdit: (rule: SpendRule) => void;
  onToggle: (rule: SpendRule, on: boolean) => void;
}): JSX.Element {
  const [query, setQuery] = useState("");
  const [actionFilter, setActionFilter] = useState<ActionFilter>("all");

  const usageByRule = useMemo(() => {
    const map = new Map<string, RuleUsage>();
    for (const rule of rules) {
      map.set(rule.id, estimateRuleUsage(rule));
    }
    return map;
  }, [rules]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return rules.filter((r) => {
      if (actionFilter !== "all" && r.breachAction !== actionFilter) {
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
    () => buildRuleColumns({ usageByRule, onToggle }),
    [usageByRule, onToggle],
  );

  if (rules.length === 0) {
    return (
      <EmptyState
        heading="No spend rules yet"
        description="Create a rule to cap AI spend for a group of actors."
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
              { value: "block", label: "Block" },
              { value: "route_fallback", label: "Route" },
              { value: "alert_only", label: "Alert" },
            ]}
          />
        </Page.Toolbar.Actions>
      </Page.Toolbar>

      <Table
        columns={columns}
        data={filtered}
        rowKey={(rule) => rule.id}
        onRowClick={(rule) => onEdit(rule)}
        noResultsMessage={<Type>No rules match your filters.</Type>}
      />
    </div>
  );
}

function buildRuleColumns({
  usageByRule,
  onToggle,
}: {
  usageByRule: Map<string, RuleUsage>;
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
      key: "scope",
      header: "Scope",
      width: "1fr",
      render: (rule) => (
        <span className={cn("text-muted-foreground text-sm", dim(rule))}>
          {scopeSummary(rule.models, rule.providers)}
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
            <UsageBar usage={usage} limitUsd={rule.limitUsd} />
          </span>
        );
      },
    },
    {
      key: "action",
      header: "Action",
      width: "0.8fr",
      render: (rule) => (
        <span className={cn("inline-flex", dim(rule))}>
          <BreachActionBadge action={rule.breachAction} />
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

function EventsTab(): JSX.Element {
  const columns = useMemo<Column<SpendControlEvent>[]>(
    () => [
      {
        key: "time",
        header: "Timestamp",
        width: "180px",
        render: (event) => (
          <span className="text-muted-foreground font-mono text-xs">
            {new Date(event.occurredAt).toLocaleString()}
          </span>
        ),
      },
      {
        key: "action",
        header: "Action",
        width: "0.9fr",
        render: (event) => (
          <span className="inline-flex">
            <BreachActionBadge action={event.action} />
          </span>
        ),
      },
      {
        key: "rule",
        header: "Rule",
        width: "1.1fr",
        render: (event) => (
          <span className="block truncate text-sm" title={event.ruleName}>
            {event.ruleName}
          </span>
        ),
      },
      {
        key: "actor",
        header: "Actor",
        width: "1.2fr",
        render: (event) => (
          <span className="block min-w-0">
            <span className="block truncate font-medium">
              {event.actorName}
            </span>
            <span className="text-muted-foreground block truncate text-xs">
              {event.actorEmail}
            </span>
          </span>
        ),
      },
      {
        key: "model",
        header: "Model",
        width: "1.1fr",
        render: (event) => (
          <span className="text-muted-foreground block truncate font-mono text-xs">
            {eventModelSummary(event)}
          </span>
        ),
      },
      {
        key: "spend",
        header: "Spend / Limit",
        width: "0.9fr",
        render: (event) => (
          <span className="text-sm">
            <span className="text-destructive font-medium">
              {formatUsd(event.spendUsd)}
            </span>
            <span className="text-muted-foreground">
              {" "}
              / {formatUsd(event.limitUsd)}
            </span>
          </span>
        ),
      },
    ],
    [],
  );

  return (
    <div className="space-y-3">
      <Table
        columns={columns}
        data={SPEND_CONTROL_EVENTS}
        rowKey={(event) => event.id}
        noResultsMessage={<Type>No budget events yet.</Type>}
      />
      <Type small muted>
        Showing {SPEND_CONTROL_EVENTS.length}{" "}
        {SPEND_CONTROL_EVENTS.length === 1 ? "event" : "events"}
      </Type>
    </div>
  );
}

function modelLabel(id: string): string {
  return MODEL_BY_ID.get(id)?.label ?? id;
}

function eventModelSummary(event: SpendControlEvent): string {
  if (event.action === "route_fallback" && event.fallbackModel) {
    return `${modelLabel(event.model)} → ${modelLabel(event.fallbackModel)}`;
  }
  return modelLabel(event.model);
}
