// Policy Evals (session replay) — AGE-2704
//
// Scaffold for the "Evals" tab on the Policy Detail view. Renders:
//   - a list of eval runs (status, time, messages scanned, findings, cost, p50/p95 latency)
//   - a "New eval run" sheet stub (sampler config + cost estimate + confirm)
//   - a run-detail view (stats panel + findings table)
//   - an "enough signal? enable policy" CTA
//
// All data is currently mock. Every spot that will call the backend is marked
// with `// TODO(AGE-2704): replace with generated eval API client`.

import { Type } from "@/components/ui/type";
import { Heading } from "@/components/ui/heading";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Input } from "@/components/ui/input";
import { Slider } from "@/components/ui/slider";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import {
  Badge,
  Button,
  type BadgeProps,
  type Column,
  Icon,
  Table,
} from "@speakeasy-api/moonshine";
import { ArrowLeft, ChevronRight, FlaskConical, Plus } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { useMemo, useState } from "react";
import {
  getMockEvalFindings,
  getMockEvalRuns,
  type PolicyEvalFinding,
  type PolicyEvalRun,
  type PolicyEvalRunStatus,
} from "./types";

const STATUS_BADGE: Record<
  PolicyEvalRunStatus,
  { label: string; variant: NonNullable<BadgeProps["variant"]> }
> = {
  pending: { label: "Pending", variant: "neutral" },
  running: { label: "Running", variant: "information" },
  completed: { label: "Completed", variant: "success" },
  cancelled: { label: "Cancelled", variant: "neutral" },
  failed: { label: "Failed", variant: "destructive" },
};

function StatusBadge({ status }: { status: PolicyEvalRunStatus }) {
  const cfg = STATUS_BADGE[status] ?? STATUS_BADGE.pending;
  return (
    <Badge variant={cfg.variant}>
      <Badge.Text>{cfg.label}</Badge.Text>
    </Badge>
  );
}

const usd = (n: number) =>
  n.toLocaleString(undefined, { style: "currency", currency: "USD" });
const num = (n: number) => n.toLocaleString();
const ms = (n?: number) => (n == null ? "—" : `${num(n)} ms`);

export function EvalsTab({
  riskPolicyId,
  policyEnabled,
}: {
  riskPolicyId: string;
  /** Drives the "enable policy" CTA copy/affordance. */
  policyEnabled: boolean;
}): JSX.Element {
  // TODO(AGE-2704): replace with generated eval API client.
  // e.g. const { data: runs, isLoading } = useListPolicyEvalRuns({ riskPolicyId });
  const runs = useMemo(() => getMockEvalRuns(riskPolicyId), [riskPolicyId]);

  const [newRunOpen, setNewRunOpen] = useState(false);
  const [selectedRun, setSelectedRun] = useState<PolicyEvalRun | null>(null);

  if (selectedRun) {
    return (
      <RunDetail
        run={selectedRun}
        policyEnabled={policyEnabled}
        onBack={() => setSelectedRun(null)}
      />
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0 space-y-1">
          <Heading variant="h3">Evals</Heading>
          <Type small muted className="font-normal">
            Replay this policy across a sample of historical messages to see what
            it would have flagged — and what it would cost — before you enable it.
          </Type>
        </div>
        <Button onClick={() => setNewRunOpen(true)}>
          <Button.LeftIcon>
            <Plus className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>New eval run</Button.Text>
        </Button>
      </div>

      {runs.length === 0 ? (
        <EvalsEmptyState onNewRun={() => setNewRunOpen(true)} />
      ) : (
        <RunsTable runs={runs} onSelect={setSelectedRun} />
      )}

      <NewRunSheet
        open={newRunOpen}
        onOpenChange={setNewRunOpen}
        riskPolicyId={riskPolicyId}
      />
    </div>
  );
}

function RunsTable({
  runs,
  onSelect,
}: {
  runs: PolicyEvalRun[];
  onSelect: (run: PolicyEvalRun) => void;
}) {
  const columns: Column<PolicyEvalRun>[] = [
    {
      key: "status",
      header: "Status",
      width: "0.7fr",
      render: (run) => <StatusBadge status={run.status} />,
    },
    {
      key: "createdAt",
      header: "Created",
      width: "0.9fr",
      render: (run) => (
        <span className="text-muted-foreground text-sm">
          {formatDistanceToNow(run.createdAt, { addSuffix: true })}
        </span>
      ),
    },
    {
      key: "messagesScanned",
      header: "Messages scanned",
      width: "0.9fr",
      render: (run) => (
        <span className="text-sm">{num(run.messagesScanned)}</span>
      ),
    },
    {
      key: "findingsCount",
      header: "Findings",
      width: "0.6fr",
      render: (run) => <span className="text-sm">{num(run.findingsCount)}</span>,
    },
    {
      key: "totalCostUsd",
      header: "Cost",
      width: "0.6fr",
      render: (run) => <span className="text-sm">{usd(run.totalCostUsd)}</span>,
    },
    {
      key: "latency",
      header: "Judge p50 / p95",
      width: "1fr",
      render: (run) => (
        <span className="text-muted-foreground text-sm">
          {ms(run.judgeLatencyP50Ms)} / {ms(run.judgeLatencyP95Ms)}
        </span>
      ),
    },
    {
      key: "chevron",
      header: "",
      width: "0.2fr",
      render: () => (
        <ChevronRight className="text-muted-foreground h-4 w-4" />
      ),
    },
  ];

  return (
    <Table
      columns={columns}
      data={runs}
      rowKey={(run) => run.id}
      onRowClick={onSelect}
    />
  );
}

function EvalsEmptyState({ onNewRun }: { onNewRun: () => void }) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <FlaskConical className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No eval runs yet
      </Type>
      <Type small muted className="mb-4 max-w-md text-center">
        Run an eval to replay this policy against historical messages and gather
        signal before enabling it.
      </Type>
      <Button onClick={onNewRun}>
        <Button.Text>New eval run</Button.Text>
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// New eval run — sampler config + cost estimate + confirm (placeholder).
// ---------------------------------------------------------------------------

function NewRunSheet({
  open,
  onOpenChange,
  riskPolicyId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  riskPolicyId: string;
}) {
  // Sampler config — local-only placeholders. These feed the (future) run-create call.
  const [sampleSize, setSampleSize] = useState(2000);
  const [lookbackDays, setLookbackDays] = useState(30);

  // TODO(AGE-2704): replace with generated eval API client.
  // A real cost estimate should come from a server-side estimate endpoint,
  // e.g. usePolicyEvalCostEstimate({ riskPolicyId, sampleSize, lookbackDays }).
  // This is a crude placeholder so the confirm flow is wired end-to-end.
  const estimatedCostUsd = (sampleSize / 1000) * 0.4;

  const handleConfirm = () => {
    // TODO(AGE-2704): replace with generated eval API client.
    // e.g. createPolicyEvalRun.mutate({ riskPolicyId, sampleSize, lookbackDays })
    //   then invalidate the runs query and close.
    void riskPolicyId;
    onOpenChange(false);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex flex-col">
        <SheetHeader>
          <SheetTitle>New eval run</SheetTitle>
          <SheetDescription>
            Configure how this policy is replayed over historical messages.
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-4">
          <div className="space-y-2">
            <Label>Sample size</Label>
            <Input
              type="number"
              value={String(sampleSize)}
              onChange={(v) => setSampleSize(Number(v) || 0)}
            />
            <Type small muted className="font-normal">
              Number of historical messages to replay.
            </Type>
          </div>

          <div className="space-y-2">
            <Label>Lookback window: {lookbackDays} days</Label>
            <Slider
              min={1}
              max={90}
              step={1}
              value={lookbackDays}
              onChange={(v) => setLookbackDays(Math.round(v))}
            />
          </div>

          {/* Cost estimate (placeholder) */}
          <div className="bg-muted/30 rounded-lg border p-4">
            <div className="flex items-center justify-between">
              <Type small muted className="font-normal">
                Estimated cost
              </Type>
              <span className="font-medium">{usd(estimatedCostUsd)}</span>
            </div>
            <Type small muted className="mt-1 font-normal">
              {/* TODO(AGE-2704): replace with server-provided estimate. */}
              Rough estimate — the real figure will come from the eval API.
            </Type>
          </div>
        </div>

        <SheetFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button onClick={handleConfirm}>
            <Button.Text>Start run</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

// ---------------------------------------------------------------------------
// Run detail — stats panel + findings table + "enable policy" CTA.
// ---------------------------------------------------------------------------

function RunDetail({
  run,
  policyEnabled,
  onBack,
}: {
  run: PolicyEvalRun;
  policyEnabled: boolean;
  onBack: () => void;
}) {
  // TODO(AGE-2704): replace with generated eval API client.
  // e.g. const { data: findings } = useListPolicyEvalFindings({ runId: run.id });
  const findings = useMemo(() => getMockEvalFindings(run.id), [run.id]);

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0 space-y-2">
          <button
            type="button"
            onClick={onBack}
            className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm"
          >
            <ArrowLeft className="h-4 w-4" />
            All runs
          </button>
          <div className="flex items-center gap-2">
            <Heading variant="h3" className="font-mono normal-case">
              {run.id}
            </Heading>
            <StatusBadge status={run.status} />
          </div>
        </div>
      </div>

      <StatsPanel run={run} />

      {/* "Enough signal? enable policy" CTA */}
      <EnableSignalCta run={run} policyEnabled={policyEnabled} />

      <div className="space-y-3">
        <Heading variant="h4">Findings</Heading>
        <FindingsTable findings={findings} />
      </div>
    </div>
  );
}

function StatsPanel({ run }: { run: PolicyEvalRun }) {
  const stats: { label: string; value: string }[] = [
    { label: "Messages scanned", value: num(run.messagesScanned) },
    { label: "Findings", value: num(run.findingsCount) },
    { label: "Total cost", value: usd(run.totalCostUsd) },
    { label: "Input tokens", value: num(run.inputTokens) },
    { label: "Output tokens", value: num(run.outputTokens) },
    { label: "Judge p50", value: ms(run.judgeLatencyP50Ms) },
    { label: "Judge p95", value: ms(run.judgeLatencyP95Ms) },
    {
      label: "Started",
      value: run.startedAt
        ? formatDistanceToNow(run.startedAt, { addSuffix: true })
        : "—",
    },
  ];
  return (
    <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border bg-border sm:grid-cols-4">
      {stats.map((s) => (
        <div key={s.label} className="bg-background p-4">
          <Type small muted className="font-normal">
            {s.label}
          </Type>
          <div className="mt-1 text-lg font-semibold">{s.value}</div>
        </div>
      ))}
    </div>
  );
}

function EnableSignalCta({
  run,
  policyEnabled,
}: {
  run: PolicyEvalRun;
  policyEnabled: boolean;
}) {
  if (policyEnabled) {
    return null;
  }
  // Trivial heuristic stand-in for "enough signal". The real threshold logic
  // will live server-side / in product analytics.
  const enoughSignal = run.status === "completed" && run.findingsCount > 0;
  return (
    <div
      className={cn(
        "flex items-center justify-between gap-4 rounded-lg border p-4",
        enoughSignal ? "border-foreground bg-muted/40" : "bg-muted/20",
      )}
    >
      <div className="flex items-center gap-3">
        <Icon
          name="shield-check"
          className="text-muted-foreground h-5 w-5 shrink-0"
        />
        <div>
          <div className="text-sm font-medium">
            {enoughSignal
              ? "Enough signal — ready to enable this policy?"
              : "Gather more signal before enabling"}
          </div>
          <Type small muted className="font-normal">
            {enoughSignal
              ? `This run surfaced ${num(run.findingsCount)} findings across ${num(
                  run.messagesScanned,
                )} messages.`
              : "Run more evals, or wait for this one to complete, to be confident."}
          </Type>
        </div>
      </div>
      <Button
        disabled={!enoughSignal}
        onClick={() => {
          // TODO(AGE-2704): replace with generated eval API client.
          // Enabling should reuse the existing risk-policy update mutation
          // (useRiskPoliciesUpdateMutation) with enabled: true.
        }}
      >
        <Button.Text>Enable policy</Button.Text>
      </Button>
    </div>
  );
}

function FindingsTable({ findings }: { findings: PolicyEvalFinding[] }) {
  const columns: Column<PolicyEvalFinding>[] = [
    {
      key: "source",
      header: "Source",
      width: "0.7fr",
      render: (f) => (
        <Badge variant="neutral">
          <Badge.Text>{f.source}</Badge.Text>
        </Badge>
      ),
    },
    {
      key: "ruleId",
      header: "Rule",
      width: "0.9fr",
      render: (f) => (
        <span className="font-mono text-xs">{f.ruleId ?? "—"}</span>
      ),
    },
    {
      key: "context",
      header: "Sample message",
      width: "1.4fr",
      render: (f) => (
        <div className="min-w-0">
          <div className="truncate text-sm">{f.chatTitle ?? "—"}</div>
          <div className="text-muted-foreground truncate text-xs">
            {f.chatUserId ?? "unknown user"}
          </div>
        </div>
      ),
    },
    {
      key: "match",
      header: "Match (redacted)",
      width: "1.2fr",
      render: (f) => (
        <span className="text-muted-foreground truncate font-mono text-xs">
          {f.match ?? "—"}
        </span>
      ),
    },
    {
      key: "confidence",
      header: "Confidence",
      width: "0.6fr",
      render: (f) => (
        <span className="text-sm">
          {f.confidence == null ? "—" : `${Math.round(f.confidence * 100)}%`}
        </span>
      ),
    },
  ];

  if (findings.length === 0) {
    return (
      <div className="bg-muted/20 rounded-lg border border-dashed px-8 py-10 text-center">
        <Type small muted>
          No findings produced by this run.
        </Type>
      </div>
    );
  }

  return (
    <Table columns={columns} data={findings} rowKey={(f) => f.id} />
  );
}
