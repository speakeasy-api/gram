import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type JSX } from "react";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { hooksRolloutQuery, invalidateHooksRollout } from "@/lib/adminQueries";
import {
  cancelHooksRolloutStage,
  errorMessage,
  pauseHooksRolloutStage,
  resumeHooksRolloutStage,
  startHooksRolloutStage,
  type AdminHooksRollout,
  type AdminHooksRolloutStage,
} from "@/lib/gramAdminApi";
import { validHooksRolloutVersion } from "@/lib/hooksRollout";
import { cn } from "@/lib/utils";

const STAGE_STEPS = [
  { percent: 0, label: "Canary" },
  { percent: 25, label: "25%" },
  { percent: 100, label: "100%" },
] as const;

type StageAction =
  | { kind: "start"; version: number }
  | { kind: "pause"; reason: string }
  | { kind: "resume" }
  | { kind: "cancel"; reason: string };

function runStageAction(action: StageAction): Promise<AdminHooksRollout> {
  switch (action.kind) {
    case "start":
      return startHooksRolloutStage(action.version);
    case "pause":
      return pauseHooksRolloutStage(action.reason);
    case "resume":
      return resumeHooksRolloutStage();
    case "cancel":
      return cancelHooksRolloutStage(action.reason);
  }
}

function isActive(stage: AdminHooksRolloutStage | undefined): boolean {
  return stage?.state === "running" || stage?.state === "paused";
}

function fmtDateTime(iso?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleString(undefined, {
    timeZone: "UTC",
    dateStyle: "medium",
    timeStyle: "short",
  });
}

/**
 * A staged rollout moves a new hooks version out over two days instead of all
 * at once: canary organizations first, then a quarter of the organizations on
 * the default pin, then the default pin itself. Pause holds it where it is.
 */
export function StagedRollout({
  rollout,
  stale,
}: {
  rollout: AdminHooksRollout;
  stale: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const [reason, setReason] = useState("");
  const stage = rollout.stage;
  const active = isActive(stage);

  const mutation = useMutation({
    mutationFn: runStageAction,
    onSuccess: (updated) => {
      setReason("");
      queryClient.setQueryData(hooksRolloutQuery.queryKey, updated);
      invalidateHooksRollout(queryClient);
    },
  });
  const disabled = mutation.isPending || stale;

  const pause = async () => {
    const confirmed = await confirm({
      title: "Pause the staged rollout?",
      description:
        "It stays at its current share until resumed. Organizations it already cleared keep the new version.",
      confirmLabel: "Pause rollout",
    });
    if (confirmed) mutation.mutate({ kind: "pause", reason: reason.trim() });
  };

  const cancel = async () => {
    const confirmed = await confirm({
      title: "Cancel the staged rollout?",
      description:
        "Organizations it cleared go back to the default pin. Hooks plugins already published are not downgraded.",
      confirmLabel: "Cancel rollout",
    });
    if (confirmed) mutation.mutate({ kind: "cancel", reason: reason.trim() });
  };

  return (
    <section className="border-border overflow-hidden rounded-md border">
      <div className="border-border bg-muted/50 space-y-1 border-b p-4">
        <h2 className="text-base font-semibold">Staged rollout</h2>
        <p className="text-muted-foreground text-sm">
          Rolls a new hooks version out to canary organizations, then 25% of
          organizations on the default pin a day later, then moves the default
          pin a day after that. Organization overrides are not affected.
        </p>
      </div>
      <div className="space-y-4 p-4">
        {stage && active ? (
          <ActiveStage stage={stage} />
        ) : (
          <>
            {stage && <LastStage stage={stage} />}
            <StartStage
              rollout={rollout}
              disabled={disabled}
              confirm={confirm}
              onStart={(version) => mutation.mutate({ kind: "start", version })}
            />
          </>
        )}
        {stage && active && (
          <div className="space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <Input
                className="w-80"
                aria-label="Reason for pausing or cancelling"
                placeholder="Reason, e.g. hooks error alert"
                maxLength={500}
                value={reason}
                disabled={disabled}
                onChange={(event) => setReason(event.target.value)}
              />
              {stage.state === "running" ? (
                <Button
                  size="sm"
                  disabled={disabled || reason.trim() === ""}
                  onClick={() => void pause()}
                >
                  Pause
                </Button>
              ) : (
                <Button
                  size="sm"
                  disabled={disabled}
                  onClick={() => mutation.mutate({ kind: "resume" })}
                >
                  Resume
                </Button>
              )}
              <Button
                size="sm"
                variant="destructive"
                disabled={disabled || reason.trim() === ""}
                onClick={() => void cancel()}
              >
                Cancel rollout
              </Button>
            </div>
            <p className="text-muted-foreground text-xs">
              A reason is required to pause or cancel.
            </p>
          </div>
        )}
        {mutation.isError && (
          <p role="alert" className="text-destructive text-sm">
            {errorMessage(mutation.error)}
          </p>
        )}
      </div>
      {confirmDialog}
    </section>
  );
}

function ActiveStage({
  stage,
}: {
  stage: AdminHooksRolloutStage;
}): JSX.Element {
  return (
    <>
      <div className="flex items-center gap-2 text-sm">
        <span className="font-medium">Hooks version {stage.version}</span>
        {stage.state === "running" ? (
          <Badge variant="outline">Running</Badge>
        ) : (
          <Badge variant="secondary">Paused</Badge>
        )}
      </div>
      <ol className="flex items-center gap-2 text-sm" aria-label="Stages">
        {STAGE_STEPS.map((step, index) => (
          <li key={step.percent} className="flex items-center gap-2">
            {index > 0 && <span className="text-muted-foreground">→</span>}
            <span
              aria-current={step.percent === stage.percent ? "step" : undefined}
              className={cn(
                "rounded-md border px-2 py-0.5",
                step.percent === stage.percent
                  ? "border-primary bg-primary text-primary-foreground"
                  : step.percent < stage.percent
                    ? "border-border text-foreground"
                    : "border-border text-muted-foreground border-dashed",
              )}
            >
              {step.label}
            </span>
          </li>
        ))}
      </ol>
      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
        <dt className="text-muted-foreground">Next stage</dt>
        <dd>
          {stage.state === "running"
            ? `${fmtDateTime(stage.next_advance_at)} UTC`
            : "Paused until resumed. Resuming restarts the day at this stage."}
        </dd>
        {stage.reason && (
          <>
            <dt className="text-muted-foreground">Paused because</dt>
            <dd>{stage.reason}</dd>
          </>
        )}
        <dt className="text-muted-foreground">Last change</dt>
        <dd>
          {stage.set_by} on {fmtDateTime(stage.set_at)} UTC
        </dd>
      </dl>
    </>
  );
}

function LastStage({ stage }: { stage: AdminHooksRolloutStage }): JSX.Element {
  const summary =
    stage.state === "completed"
      ? `The staged rollout of hooks version ${stage.version} completed and moved the default pin on ${fmtDateTime(stage.set_at)} UTC.`
      : `The staged rollout of hooks version ${stage.version} was cancelled by ${stage.set_by} on ${fmtDateTime(stage.set_at)} UTC${stage.reason ? `: ${stage.reason}` : "."}`;
  return <p className="text-muted-foreground text-sm">{summary}</p>;
}

function StartStage({
  rollout,
  disabled,
  confirm,
  onStart,
}: {
  rollout: AdminHooksRollout;
  disabled: boolean;
  confirm: ReturnType<typeof useConfirmDialog>[0];
  onStart: (version: number) => void;
}): JSX.Element {
  const current = rollout.current_version;
  const pinned = rollout.default_pin?.version;
  const [value, setValue] = useState(String(current));
  const parsed = validHooksRolloutVersion(value, current);
  const version =
    parsed !== undefined && (pinned === undefined || parsed > pinned)
      ? parsed
      : undefined;

  if (pinned !== undefined && pinned >= current) {
    return (
      <p className="text-sm" role="status">
        Nothing to roll out: the default pin already clears hooks version{" "}
        {current}.
      </p>
    );
  }

  const start = async () => {
    if (version === undefined) return;
    const confirmed = await confirm({
      title: `Start a staged rollout of hooks version ${version}?`,
      description:
        "Canary organizations get it now. 25% of organizations on the default pin get it in a day, and the default pin moves a day after that unless you pause or cancel.",
      confirmLabel: "Start rollout",
    });
    if (confirmed) onStart(version);
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Input
          className="w-28"
          type="number"
          min={(pinned ?? 0) + 1}
          max={current}
          step={1}
          aria-label="Hooks version to roll out"
          value={value}
          disabled={disabled}
          onChange={(event) => setValue(event.target.value)}
        />
        <Button
          size="sm"
          disabled={disabled || version === undefined}
          onClick={() => void start()}
        >
          Start staged rollout
        </Button>
      </div>
      {version === undefined && (
        <p className="text-destructive text-sm">
          The version must be a whole number above the default pin
          {pinned === undefined ? "" : ` (${pinned})`} and at most {current}.
        </p>
      )}
    </div>
  );
}
