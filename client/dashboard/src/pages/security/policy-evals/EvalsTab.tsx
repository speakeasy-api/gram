// Policy Evals (session replay) — AGE-2704
//
// The "Evals" tab on the Policy Detail view. Renders:
//   - a list of eval runs (status, time, messages scanned, findings, cost, p50/p95 latency)
//   - a "New eval run" sheet (sampler config + confirm)
//   - a run-detail view (stats panel + findings table) that polls while running
//   - an "enough signal? enable policy" CTA
//
// Data comes from the generated @gram/client eval API hooks.

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
import {
  ArrowLeft,
  ChevronRight,
  FlaskConical,
  Loader2,
  Plus,
} from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  invalidateAllRiskGetPolicyEvalRun,
  invalidateAllRiskListPolicies,
  invalidateAllRiskListPolicyEvalRuns,
  useRiskCancelPolicyEvalRunMutation,
  useRiskCreatePolicyEvalRunMutation,
  useRiskGetPolicyEvalRun,
  useRiskListPolicyEvalFindings,
  useRiskListPolicyEvalRuns,
  useRiskPoliciesGet,
  useRiskPoliciesUpdateMutation,
} from "@gram/client/react-query/index.js";
import type { PolicyEvalFinding } from "@gram/client/models/components/policyevalfinding.js";
import type {
  PolicyEvalRun,
  PolicyEvalRunStatus,
} from "@gram/client/models/components/policyevalrun.js";

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
  const { data, isLoading } = useRiskListPolicyEvalRuns({
    policyId: riskPolicyId,
  });
  const runs = data?.runs ?? [];

  const [newRunOpen, setNewRunOpen] = useState(false);
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);

  if (selectedRunId) {
    return (
      <RunDetail
        runId={selectedRunId}
        policyId={riskPolicyId}
        policyEnabled={policyEnabled}
        onBack={() => setSelectedRunId(null)}
      />
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0 space-y-1">
          <Heading variant="h3">Evals</Heading>
          <Type small muted className="font-normal">
            Replay this policy across a sample of historical messages to see
            what it would have flagged — and what it would cost — before you
            enable it.
          </Type>
        </div>
        <Button onClick={() => setNewRunOpen(true)}>
          <Button.LeftIcon>
            <Plus className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>New eval run</Button.Text>
        </Button>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
        </div>
      ) : runs.length === 0 ? (
        <EvalsEmptyState onNewRun={() => setNewRunOpen(true)} />
      ) : (
        <RunsTable runs={runs} onSelect={(run) => setSelectedRunId(run.id)} />
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
      render: (run) => (
        <span className="text-sm">{num(run.findingsCount)}</span>
      ),
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
      render: () => <ChevronRight className="text-muted-foreground h-4 w-4" />,
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
// New eval run — sampler config + confirm.
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
  const queryClient = useQueryClient();
  const [sampleSize, setSampleSize] = useState(2000);
  const [lookbackDays, setLookbackDays] = useState(30);

  const createMutation = useRiskCreatePolicyEvalRunMutation({
    onSuccess: () => {
      void invalidateAllRiskListPolicyEvalRuns(queryClient);
      onOpenChange(false);
    },
  });

  const handleConfirm = () => {
    createMutation.mutate({
      request: {
        createPolicyEvalRunRequestBody: {
          policyId: riskPolicyId,
          sample: {
            mode: "auto",
            maxMessages: sampleSize,
            lookbackDays,
          },
        },
      },
    });
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
              Maximum number of historical messages to replay.
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
        </div>

        <SheetFooter>
          <Button
            variant="secondary"
            onClick={() => onOpenChange(false)}
            disabled={createMutation.isPending}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={createMutation.isPending || sampleSize < 1}
          >
            <Button.Text>
              {createMutation.isPending ? "Starting…" : "Start run"}
            </Button.Text>
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
  runId,
  policyId,
  policyEnabled,
  onBack,
}: {
  runId: string;
  policyId: string;
  policyEnabled: boolean;
  onBack: () => void;
}) {
  const queryClient = useQueryClient();

  const { data: run } = useRiskGetPolicyEvalRun({ id: runId }, undefined, {
    // Poll while the run is in flight so stats and status stay fresh.
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "pending" || status === "running" ? 3000 : false;
    },
  });

  const { data: findingsData } = useRiskListPolicyEvalFindings({ runId });
  const findings = findingsData?.findings ?? [];

  const cancelMutation = useRiskCancelPolicyEvalRunMutation({
    onSuccess: () => {
      void invalidateAllRiskGetPolicyEvalRun(queryClient);
      void invalidateAllRiskListPolicyEvalRuns(queryClient);
    },
  });

  const isInFlight = run?.status === "pending" || run?.status === "running";

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
              {runId}
            </Heading>
            {run && <StatusBadge status={run.status} />}
          </div>
        </div>
        {isInFlight && (
          <Button
            variant="secondary"
            disabled={cancelMutation.isPending}
            onClick={() =>
              cancelMutation.mutate({
                request: { riskIDRequestBody: { id: runId } },
              })
            }
          >
            <Button.Text>Cancel run</Button.Text>
          </Button>
        )}
      </div>

      {run ? (
        <>
          <StatsPanel run={run} />
          <EnableSignalCta
            run={run}
            policyId={policyId}
            policyEnabled={policyEnabled}
          />
          <div className="space-y-3">
            <Heading variant="h4">Findings</Heading>
            <FindingsTable findings={findings} />
          </div>
        </>
      ) : (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
        </div>
      )}
    </div>
  );
}

function StatsPanel({ run }: { run: PolicyEvalRun }) {
  const stats: { label: string; value: string }[] = [
    { label: "Messages scanned", value: num(run.messagesScanned) },
    { label: "Findings", value: num(run.findingsCount) },
    { label: "Total cost", value: usd(run.totalCostUsd) },
    { label: "Input tokens", value: num(run.inputTokens ?? 0) },
    { label: "Output tokens", value: num(run.outputTokens ?? 0) },
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
  policyId,
  policyEnabled,
}: {
  run: PolicyEvalRun;
  policyId: string;
  policyEnabled: boolean;
}) {
  const queryClient = useQueryClient();
  const { data: policy } = useRiskPoliciesGet({ id: policyId }, undefined, {
    enabled: !policyEnabled,
  });
  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      void invalidateAllRiskListPolicies(queryClient);
    },
  });

  if (policyEnabled) {
    return null;
  }

  // Trivial heuristic stand-in for "enough signal".
  const enoughSignal = run.status === "completed" && run.findingsCount > 0;

  const handleEnable = () => {
    if (!policy) {
      return;
    }
    // Echo the policy's current configuration with enabled:true — updateRiskPolicy
    // overwrites detection fields, so a partial update would clobber them.
    updateMutation.mutate({
      request: {
        updateRiskPolicyRequestBody: {
          id: policy.id,
          name: policy.name,
          enabled: true,
          action: policy.action,
          audienceType: policy.audienceType,
          audiencePrincipalUrns: policy.audiencePrincipalUrns,
          autoName: policy.autoName,
          sources: policy.sources,
          presidioEntities: policy.presidioEntities,
          promptInjectionRules: policy.promptInjectionRules,
          disabledRules: policy.disabledRules,
          customRuleIds: policy.customRuleIds,
          messageTypes: policy.messageTypes,
          scopeInclude: policy.scopeInclude,
          scopeExempt: policy.scopeExempt,
          userMessage: policy.userMessage,
          prompt: policy.prompt,
          modelConfig: policy.modelConfig,
        },
      },
    });
  };

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
        disabled={!enoughSignal || !policy || updateMutation.isPending}
        onClick={handleEnable}
      >
        <Button.Text>
          {updateMutation.isPending ? "Enabling…" : "Enable policy"}
        </Button.Text>
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
      header: "Match",
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

  return <Table columns={columns} data={findings} rowKey={(f) => f.id} />;
}
