// Policy Evals (session replay) — AGE-2704
//
// The "Evals" tab on the Policy Detail view. Renders:
//   - a list of eval runs (status, time, messages scanned, findings, cost, latency)
//   - a "New eval run" sheet (sampler config + confirm)
//   - a run-detail view (metrics + findings table) that polls while running
//   - an optional "enable policy" convenience CTA
//
// Evals are advisory: they preview what a policy would flag. Enabling a policy
// is always allowed via the Configuration "Details" toggle; nothing here gates
// it.
//
// Data comes from the generated @gram/client eval API hooks.

import { Type } from "@/components/ui/type";
import { Heading } from "@/components/ui/heading";
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
  RotateCw,
} from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { useState } from "react";
import { useQueryState } from "nuqs";
import { useQueryClient } from "@tanstack/react-query";
import {
  invalidateAllRiskGetPolicyEvalRun,
  invalidateAllRiskListPolicies,
  invalidateAllRiskListPolicyEvalRuns,
  useRiskCancelPolicyEvalRunMutation,
  useRiskGetPolicyEvalRun,
  useRiskListPolicyEvalFindings,
  useRiskListPolicyEvalRuns,
  useRiskPoliciesGet,
  useRiskPoliciesUpdateMutation,
} from "@gram/client/react-query/index.js";
import { invalidateAllRiskPoliciesGet } from "@gram/client/react-query/riskPoliciesGet.js";
import { invalidateAllRiskPoliciesStatus } from "@gram/client/react-query/riskPoliciesStatus.js";
import type { PolicyEvalFinding } from "@gram/client/models/components/policyevalfinding.js";
import type {
  PolicyEvalRun,
  PolicyEvalRunStatus,
} from "@gram/client/models/components/policyevalrun.js";
import type { EvalSource } from "../policy-form/use-policy-form";
import { NewRunSheet } from "./NewRunSheet";
import {
  describeSample,
  formatCount,
  formatDuration,
  formatMs,
  formatUsd,
  isRunStale,
  runTitle,
} from "./format";

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

export function EvalsTab({
  evalSource,
  policyId,
  policyEnabled,
  currentVersion,
  policyType,
  isDirty = false,
  canRun = true,
}: {
  /** What to evaluate: a saved policy_id (clean) or an inline candidate
   *  (create mode, or a saved policy with unsaved edits). */
  evalSource: EvalSource;
  /** The saved policy id, if any. Absent in create/draft mode: the runs list is
   *  empty and the "enable policy" CTA is hidden. */
  policyId?: string;
  /** Drives the "enable policy" CTA copy/affordance. */
  policyEnabled: boolean;
  /** Current policy version, used to flag runs made against an older config. */
  currentVersion?: number;
  /** The policy kind being evaluated. Drives judge-only UI (latency, judge
   *  warnings). Defaults to standard when unknown. */
  policyType?: "standard" | "prompt_based";
  /** True when the on-screen config has unsaved edits (candidate eval). */
  isDirty?: boolean;
  /** False when the config has nothing to evaluate; gates new eval runs. */
  canRun?: boolean;
}): JSX.Element {
  // Only a saved policy has a run history; in create/draft mode the list query
  // is disabled and we show the create-mode empty state.
  const { data, isLoading } = useRiskListPolicyEvalRuns(
    policyId ? { policyId } : undefined,
    undefined,
    { enabled: policyId != null },
  );
  const runs = policyId ? (data?.runs ?? []) : [];

  const [newRunOpen, setNewRunOpen] = useState(false);
  // The selected run lives in `?run=` so it can be set from both this tab and
  // the Configuration banner (candidate runs are otherwise invisible).
  const [selectedRunId, setSelectedRunId] = useQueryState("run");

  if (selectedRunId) {
    return (
      <RunDetail
        runId={selectedRunId}
        policyId={policyId}
        policyEnabled={policyEnabled}
        currentVersion={currentVersion}
        evalSource={evalSource}
        policyType={policyType}
        isDirty={isDirty}
        canRun={canRun}
        onBack={() => void setSelectedRunId(null)}
        onSelectRun={(id) => void setSelectedRunId(id)}
      />
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0 space-y-1">
          <Heading variant="h3">Evals</Heading>
          <Type small muted className="font-normal">
            Replay this policy across a sample of historical messages to preview
            what it would flag — and what it would cost. Evals are a
            recommendation, not a requirement.
          </Type>
          {policyId && isDirty && (
            <Type small muted className="font-normal">
              Evals reflect your unsaved edits, not the saved policy.
            </Type>
          )}
        </div>
        <Button onClick={() => setNewRunOpen(true)}>
          <Button.LeftIcon>
            <Plus className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>New eval run</Button.Text>
        </Button>
      </div>

      {policyId && isLoading ? (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
        </div>
      ) : runs.length === 0 ? (
        <EvalsEmptyState
          onNewRun={() => setNewRunOpen(true)}
          isDraft={policyId == null}
        />
      ) : (
        <RunsTable
          runs={runs}
          currentVersion={currentVersion}
          policyType={policyType}
          onSelect={(run) => void setSelectedRunId(run.id)}
        />
      )}

      <NewRunSheet
        open={newRunOpen}
        onOpenChange={setNewRunOpen}
        evalSource={evalSource}
        onCreated={(run) => void setSelectedRunId(run.id)}
        canRun={canRun}
      />
    </div>
  );
}

function RunsTable({
  runs,
  currentVersion,
  policyType,
  onSelect,
}: {
  runs: PolicyEvalRun[];
  currentVersion?: number;
  policyType?: "standard" | "prompt_based";
  onSelect: (run: PolicyEvalRun) => void;
}) {
  // Standard policies never call the judge, so latency is always "—"; drop it.
  const showLatency = policyType !== "standard";
  const columns: Column<PolicyEvalRun>[] = [
    {
      key: "status",
      header: "Status",
      width: "0.7fr",
      render: (run) => <StatusBadge status={run.status} />,
    },
    {
      key: "run",
      header: "Run",
      width: "1.5fr",
      render: (run) => (
        <div className="min-w-0">
          <div className="truncate text-sm">
            {formatDistanceToNow(run.createdAt, { addSuffix: true })}
            {run.riskPolicyVersion != null && (
              <span className="text-muted-foreground">
                {" "}
                · policy v{run.riskPolicyVersion}
              </span>
            )}
          </div>
          {isRunStale(run, currentVersion) && (
            <div className="text-muted-foreground text-xs">
              Config changed since this run
            </div>
          )}
        </div>
      ),
    },
    {
      key: "messagesScanned",
      header: "Messages scanned",
      width: "0.9fr",
      render: (run) => (
        <span className="text-sm">{formatCount(run.messagesScanned)}</span>
      ),
    },
    {
      key: "findingsCount",
      header: "Findings",
      width: "0.6fr",
      render: (run) => (
        <span className="text-sm">{formatCount(run.findingsCount)}</span>
      ),
    },
    {
      key: "totalCostUsd",
      header: "Cost",
      width: "0.6fr",
      render: (run) => (
        <span className="text-sm">{formatUsd(run.totalCostUsd)}</span>
      ),
    },
    ...(showLatency
      ? [
          {
            key: "latency",
            header: "Latency (p50/p95)",
            width: "1fr",
            render: (run: PolicyEvalRun) => (
              <span className="text-muted-foreground text-sm">
                {formatMs(run.judgeLatencyP50Ms)} /{" "}
                {formatMs(run.judgeLatencyP95Ms)}
              </span>
            ),
          } satisfies Column<PolicyEvalRun>,
        ]
      : []),
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

function EvalsEmptyState({
  onNewRun,
  isDraft,
}: {
  onNewRun: () => void;
  isDraft: boolean;
}) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <FlaskConical className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        No eval runs yet
      </Type>
      <Type small muted className="mb-4 max-w-md text-center">
        {isDraft
          ? "Run an eval to replay this draft configuration against historical messages and preview what it would flag."
          : "Run an eval to replay this policy against historical messages and preview what it would flag."}
      </Type>
      <Button onClick={onNewRun}>
        <Button.Text>New eval run</Button.Text>
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Run detail — metrics + findings table + optional "enable policy" CTA.
// ---------------------------------------------------------------------------

function RunDetail({
  runId,
  policyId,
  policyEnabled,
  currentVersion,
  evalSource,
  policyType,
  isDirty = false,
  canRun = true,
  onBack,
  onSelectRun,
}: {
  runId: string;
  policyId?: string;
  policyEnabled: boolean;
  currentVersion?: number;
  evalSource: EvalSource;
  policyType?: "standard" | "prompt_based";
  isDirty?: boolean;
  canRun?: boolean;
  onBack: () => void;
  onSelectRun: (id: string) => void;
}) {
  const queryClient = useQueryClient();
  const [reRunOpen, setReRunOpen] = useState(false);

  const {
    data: run,
    isLoading: runLoading,
    isError: runError,
  } = useRiskGetPolicyEvalRun({ id: runId }, undefined, {
    // Poll while the run is in flight so metrics and status stay fresh.
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "pending" || status === "running" ? 3000 : false;
    },
  });

  // The run can flip to "completed" a beat before its finding rows are
  // queryable, and the findings query would otherwise fetch once (empty) and
  // never refetch — leaving findingsCount=N but an empty table. Only fetch once
  // the run is terminal, and keep polling while the count says there should be
  // findings but none have loaded yet.
  const expectFindings = (run?.findingsCount ?? 0) > 0;
  const {
    data: findingsData,
    isLoading: findingsLoading,
    isSuccess: findingsLoaded,
  } = useRiskListPolicyEvalFindings({ runId }, undefined, {
    enabled:
      run != null && run.status !== "pending" && run.status !== "running",
    refetchInterval: (query) => {
      const loaded = query.state.data?.findings?.length ?? 0;
      return expectFindings && loaded === 0 ? 1500 : false;
    },
  });
  const findings = findingsData?.findings ?? [];

  const cancelMutation = useRiskCancelPolicyEvalRunMutation({
    onSuccess: () => {
      void invalidateAllRiskGetPolicyEvalRun(queryClient);
      void invalidateAllRiskListPolicyEvalRuns(queryClient);
    },
  });

  const isInFlight = run?.status === "pending" || run?.status === "running";
  const isCompleted = run?.status === "completed";
  const isFailed = run?.status === "failed";
  const isCancelled = run?.status === "cancelled";
  // The `?run=` param can point at a run from another policy (stale/shared URL).
  // Candidate runs carry no riskPolicyId, so only treat a run as foreign when it
  // names a *different* saved policy than the one in view.
  const foreignRun =
    run != null &&
    policyId != null &&
    run.riskPolicyId != null &&
    run.riskPolicyId !== policyId;
  // A completed run that scanned nothing means no sampled message matched the
  // policy's scope — distinct from "scanned and found nothing".
  const noMessagesInScope = isCompleted && (run?.messagesScanned ?? 0) === 0;
  // A prompt policy that scanned messages but made zero model calls means the
  // judge never ran — the "clean" result is not trustworthy. Guard on
  // messagesScanned so an honest empty-scope run isn't mislabeled as a failure.
  const judgeDidNotRun =
    isCompleted &&
    policyType === "prompt_based" &&
    (run?.messagesScanned ?? 0) > 0 &&
    (run?.inputTokens ?? 0) + (run?.outputTokens ?? 0) === 0;

  // A missing/deleted run, or a `?run=` that names a different policy's run,
  // gets a dedicated not-found state (no title/Re-run header for a run we can't
  // show). Only after loading settles — while loading we fall through to the
  // spinner below.
  if (foreignRun || (!run && !runLoading)) {
    return (
      <RunNotFoundPanel onBack={onBack} notFound={foreignRun || runError} />
    );
  }

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
          <div className="flex flex-wrap items-center gap-2">
            <Heading variant="h3">{run ? runTitle(run) : "Eval run"}</Heading>
            {run && <StatusBadge status={run.status} />}
            {run && isRunStale(run, currentVersion) && (
              <Badge variant="warning">
                <Badge.Text>Config changed since this run</Badge.Text>
              </Badge>
            )}
          </div>
          {run?.sample && describeSample(run.sample) && (
            <Type small muted className="font-normal">
              Sample: {describeSample(run.sample)}
            </Type>
          )}
        </div>
        <div className="flex shrink-0 gap-2">
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
          {!isInFlight && (
            <Button variant="secondary" onClick={() => setReRunOpen(true)}>
              <Button.LeftIcon>
                <RotateCw className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Re-run</Button.Text>
            </Button>
          )}
        </div>
      </div>

      {!run ? (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
        </div>
      ) : isInFlight ? (
        <InProgressPanel status={run.status} />
      ) : isFailed ? (
        <FailedPanel run={run} />
      ) : isCompleted ? (
        <>
          {judgeDidNotRun && <JudgeDidNotRunPanel />}
          {noMessagesInScope && <NoMessagesInScopePanel />}
          <MetricsPanel run={run} policyType={policyType} />
          {!noMessagesInScope && (
            <EnableSignalCta
              policyId={policyId}
              policyEnabled={policyEnabled}
              run={run}
              currentVersion={currentVersion}
              isDirty={isDirty}
            />
          )}
          <FindingsSection
            run={run}
            findings={findings}
            isLoading={findingsLoading}
            isLoaded={findingsLoaded}
            suppressAllClear={judgeDidNotRun || noMessagesInScope}
          />
        </>
      ) : isCancelled ? (
        <>
          <CancelledPanel />
          <MetricsPanel run={run} policyType={policyType} />
        </>
      ) : (
        // Any other terminal state: show what we have, no findings.
        <MetricsPanel run={run} policyType={policyType} />
      )}

      <NewRunSheet
        open={reRunOpen}
        onOpenChange={setReRunOpen}
        evalSource={evalSource}
        onCreated={(created) => {
          onSelectRun(created.id);
        }}
        canRun={canRun}
      />
    </div>
  );
}

function JudgeDidNotRunPanel() {
  return (
    <div className="border-destructive/40 bg-destructive/5 flex items-start gap-2 rounded-lg border p-4">
      <Icon
        name="triangle-alert"
        className="text-destructive mt-0.5 h-5 w-5 shrink-0"
      />
      <Type small className="text-destructive/90 font-normal">
        The LLM judge returned no results for this eval (0 model calls), so this
        is not a clean pass. The judge may be disabled, rate-limited, or every
        call failed. Try again, or re-run against a smaller sample.
      </Type>
    </div>
  );
}

function CancelledPanel() {
  return (
    <div className="border-border bg-muted/20 flex items-start gap-2 rounded-lg border p-4">
      <Icon
        name="triangle-alert"
        className="text-muted-foreground mt-0.5 h-5 w-5 shrink-0"
      />
      <Type small muted className="font-normal">
        Run cancelled — partial results below.
      </Type>
    </div>
  );
}

function NoMessagesInScopePanel() {
  return (
    <div className="border-border bg-muted/20 flex items-start gap-2 rounded-lg border p-4">
      <Icon
        name="info"
        className="text-muted-foreground mt-0.5 h-5 w-5 shrink-0"
      />
      <Type small muted className="font-normal">
        No messages matched this policy&apos;s scope in the selected sample, so
        nothing was scanned. Widen the scope or the sample window to test it.
      </Type>
    </div>
  );
}

function RunNotFoundPanel({
  onBack,
  notFound,
}: {
  onBack: () => void;
  notFound: boolean;
}) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-lg border border-dashed px-8 py-16 text-center">
      <Icon
        name="triangle-alert"
        className="text-muted-foreground mb-3 h-6 w-6"
      />
      <Type variant="subheading" className="mb-1">
        {notFound ? "Run not found" : "Couldn’t load this run"}
      </Type>
      <Type small muted className="mb-4 max-w-md font-normal">
        This eval run no longer exists or isn’t available. It may have been
        deleted or belongs to a different policy.
      </Type>
      <Button variant="secondary" onClick={onBack}>
        <Button.Text>Back to all runs</Button.Text>
      </Button>
    </div>
  );
}

function InProgressPanel({ status }: { status: PolicyEvalRunStatus }) {
  const label = status === "pending" ? "Queued…" : "Scanning…";
  const sub =
    status === "pending"
      ? "This run is queued and will start shortly."
      : "Replaying the policy across the sample. Metrics appear as it runs.";
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-lg border border-dashed px-8 py-16">
      <Loader2 className="text-muted-foreground mb-3 h-6 w-6 animate-spin" />
      <Type variant="subheading" className="mb-1">
        {label}
      </Type>
      <Type small muted className="text-center">
        {sub}
      </Type>
    </div>
  );
}

function FailedPanel({ run }: { run: PolicyEvalRun }) {
  const duration = formatDuration(run.startedAt, run.completedAt);
  return (
    <div className="border-destructive/40 bg-destructive/5 space-y-2 rounded-lg border p-4">
      <div className="flex items-center gap-2">
        <Icon name="triangle-alert" className="text-destructive h-5 w-5" />
        <div className="text-destructive text-sm font-medium">
          This run failed
        </div>
      </div>
      <Type small className="text-destructive/90 font-normal">
        {run.error || "This run failed before producing results."}
      </Type>
      {duration !== "—" && (
        <Type small muted className="font-normal">
          Failed after {duration}.
        </Type>
      )}
    </div>
  );
}

function MetricsPanel({
  run,
  policyType,
}: {
  run: PolicyEvalRun;
  policyType?: "standard" | "prompt_based";
}) {
  // Lead with the numbers a policy owner cares about; tuck infra detail below.
  const headline: { label: string; value: string }[] = [
    { label: "Findings", value: formatCount(run.findingsCount) },
    { label: "Messages scanned", value: formatCount(run.messagesScanned) },
    { label: "Cost", value: formatUsd(run.totalCostUsd) },
    {
      label: "Duration",
      value: formatDuration(run.startedAt, run.completedAt),
    },
  ];

  // Standard policies never call the judge, so latency is always "—"; drop it.
  const showLatency = policyType !== "standard";
  const details: { label: string; value: string }[] = [
    ...(showLatency
      ? [
          {
            label: "Latency (p50/p95)",
            value:
              run.judgeLatencyP50Ms == null && run.judgeLatencyP95Ms == null
                ? "—"
                : `${formatMs(run.judgeLatencyP50Ms)} / ${formatMs(run.judgeLatencyP95Ms)}`,
          },
        ]
      : []),
    { label: "Input tokens", value: formatCount(run.inputTokens) },
    { label: "Output tokens", value: formatCount(run.outputTokens) },
    {
      label: "Finished",
      value: run.completedAt
        ? formatDistanceToNow(run.completedAt, { addSuffix: true })
        : "—",
    },
  ];

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border bg-border sm:grid-cols-4">
        {headline.map((s) => (
          <div key={s.label} className="bg-background p-4">
            <Type small muted className="font-normal">
              {s.label}
            </Type>
            <div className="mt-1 text-lg font-semibold">{s.value}</div>
          </div>
        ))}
      </div>
      <div className="text-muted-foreground flex flex-wrap gap-x-6 gap-y-1 px-1 text-xs">
        {details.map((s) => (
          <span key={s.label}>
            {s.label}: <span className="text-foreground">{s.value}</span>
          </span>
        ))}
      </div>
    </div>
  );
}

function FindingsSection({
  run,
  findings,
  isLoading,
  isLoaded,
  suppressAllClear,
}: {
  run: PolicyEvalRun;
  findings: PolicyEvalFinding[];
  isLoading: boolean;
  isLoaded: boolean;
  /** When true, never show the reassuring "ran clean" all-clear (the judge did
   *  not actually run, so an empty result is not a real pass). */
  suppressAllClear: boolean;
}) {
  return (
    <div className="space-y-3">
      <div className="flex items-baseline gap-2">
        <Heading variant="h4">Findings</Heading>
        <span className="text-muted-foreground text-sm">
          {formatCount(run.findingsCount)}
        </span>
      </div>
      {/* `findingsCount` (from the run) is authoritative; the list can lag it
          briefly. While the count says there should be findings but none have
          loaded, keep the loader — never flash the "ran clean" all-clear. */}
      {isLoading ||
      !isLoaded ||
      (run.findingsCount > 0 && findings.length === 0) ? (
        <div className="bg-muted/20 flex items-center justify-center rounded-lg border border-dashed py-12">
          <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
        </div>
      ) : (
        <FindingsTable
          findings={findings}
          suppressAllClear={suppressAllClear}
        />
      )}
    </div>
  );
}

function EnableSignalCta({
  policyId,
  policyEnabled,
  run,
  currentVersion,
  isDirty,
}: {
  policyId?: string;
  policyEnabled: boolean;
  run: PolicyEvalRun;
  currentVersion?: number;
  isDirty: boolean;
}) {
  const queryClient = useQueryClient();
  const { data: policy } = useRiskPoliciesGet(
    { id: policyId ?? "" },
    undefined,
    {
      enabled: !!policyId && !policyEnabled,
    },
  );
  const updateMutation = useRiskPoliciesUpdateMutation({
    onSuccess: () => {
      void invalidateAllRiskListPolicies(queryClient);
      // The Policy Detail hero badge + this CTA read from RiskPoliciesGet; without
      // this the page keeps showing "Disabled" after a successful enable.
      void invalidateAllRiskPoliciesGet(queryClient);
      void invalidateAllRiskPoliciesStatus(queryClient);
    },
  });

  // No CTA for a draft (no saved policy to enable) or an already-enabled policy.
  if (!policyId || policyEnabled) {
    return null;
  }

  // The one-click enable is only honest when the viewed run reflects the current
  // saved config: not stale, and no unsaved edits on screen. Otherwise the run
  // doesn't represent what would actually be enabled.
  const stale = isRunStale(run, currentVersion);
  if (stale || isDirty) {
    return (
      <div className="bg-muted/20 flex items-center gap-3 rounded-lg border p-4">
        <Icon
          name="shield-check"
          className="text-muted-foreground h-5 w-5 shrink-0"
        />
        <Type small muted className="font-normal">
          Save and re-run against the current config to enable from an eval.
        </Type>
      </div>
    );
  }

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
    <div className="bg-muted/20 flex items-center justify-between gap-4 rounded-lg border p-4">
      <div className="flex items-center gap-3">
        <Icon
          name="shield-check"
          className="text-muted-foreground h-5 w-5 shrink-0"
        />
        <div>
          <div className="text-sm font-medium">
            Looks good? You can enable this policy.
          </div>
          <Type small muted className="font-normal">
            Enabling starts scanning live messages. You can change this anytime
            from Configuration.
          </Type>
        </div>
      </div>
      <Button
        variant="secondary"
        disabled={!policy || updateMutation.isPending}
        onClick={handleEnable}
      >
        <Button.Text>
          {updateMutation.isPending ? "Enabling…" : "Enable policy"}
        </Button.Text>
      </Button>
    </div>
  );
}

function FindingsTable({
  findings,
  suppressAllClear,
}: {
  findings: PolicyEvalFinding[];
  suppressAllClear: boolean;
}) {
  const columns: Column<PolicyEvalFinding>[] = [
    {
      key: "session",
      header: "Session",
      width: "1.4fr",
      render: (f) => (
        <div className="min-w-0">
          <div className="truncate text-sm">
            {f.chatTitle ??
              (f.chatId ? `${f.chatId.slice(0, 8)}…` : "Unknown session")}
          </div>
          <div className="text-muted-foreground truncate text-xs">
            {f.chatUserId ?? "unknown user"}
          </div>
        </div>
      ),
    },
    {
      key: "source",
      header: "Source / rule",
      width: "1fr",
      render: (f) => (
        <div className="flex min-w-0 items-center gap-2">
          <Badge variant="neutral">
            <Badge.Text>{f.source}</Badge.Text>
          </Badge>
          {f.ruleId && (
            <span className="text-muted-foreground truncate font-mono text-xs">
              {f.ruleId}
            </span>
          )}
        </div>
      ),
    },
    {
      key: "match",
      header: "Match",
      width: "1.3fr",
      render: (f) => (
        <span className="text-muted-foreground block truncate font-mono text-xs">
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
    {
      key: "tags",
      header: "Tags",
      width: "1fr",
      render: (f) =>
        f.tags && f.tags.length > 0 ? (
          <div className="flex flex-wrap gap-1">
            {f.tags.map((tag) => (
              <Badge key={tag} variant="neutral">
                <Badge.Text>{tag}</Badge.Text>
              </Badge>
            ))}
          </div>
        ) : (
          <span className="text-muted-foreground text-sm">—</span>
        ),
    },
  ];

  if (findings.length === 0) {
    // A completed run with no findings ran clean — show the schema (header row)
    // plus an explicit "nothing flagged" message, not a bare gray box.
    return (
      <div className="overflow-hidden rounded-lg border">
        <div
          className="text-muted-foreground bg-muted/30 grid items-center gap-4 border-b px-4 py-2.5 text-xs font-medium"
          style={{ gridTemplateColumns: "1.4fr 1fr 1.3fr 0.6fr 1fr" }}
        >
          <span>Session</span>
          <span>Source / rule</span>
          <span>Match</span>
          <span>Confidence</span>
          <span>Tags</span>
        </div>
        <div className="px-8 py-10 text-center">
          <Type small muted>
            {suppressAllClear
              ? "No findings were recorded for this run."
              : "No findings — this policy would not have flagged anything in this sample."}
          </Type>
        </div>
      </div>
    );
  }

  return <Table columns={columns} data={findings} rowKey={(f) => f.id} />;
}
