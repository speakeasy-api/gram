import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import {
  workloadAdmissionsLabel,
  workloadAgentLabel,
  workloadIssuerLabel,
} from "@/lib/workload-session";
import type { UserSessionWorkload } from "@gram/client/models/components/usersessionworkload.js";

/** Leads a workload row where a person's avatar would sit. */
export function WorkloadIcon({
  className,
}: {
  className?: string;
}): JSX.Element {
  return (
    <span
      className={cn(
        "bg-muted/50 flex size-6 shrink-0 items-center justify-center",
        className,
      )}
    >
      <Icon name="workflow" className="text-muted-foreground size-3.5" />
    </span>
  );
}

function WorkloadTooltip({
  workload,
}: {
  workload: UserSessionWorkload;
}): JSX.Element {
  return (
    <dl className="grid max-w-sm grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-left text-xs">
      <dt className="text-muted-foreground">Issuer</dt>
      <dd className="break-all">{workloadIssuerLabel(workload)}</dd>
      {workload.workloadIssuerUrl ? (
        <>
          <dt className="text-muted-foreground">Issuer URL</dt>
          <dd className="break-all">{workload.workloadIssuerUrl}</dd>
        </>
      ) : null}
      <dt className="text-muted-foreground">Subject</dt>
      <dd className="break-all">{workload.externalSubject}</dd>
      <dt className="text-muted-foreground">Authority</dt>
      <dd>{workloadAgentLabel(workload)}</dd>
      <dt className="text-muted-foreground">Admitted by</dt>
      <dd>{workloadAdmissionsLabel(workload)}</dd>
    </dl>
  );
}

/**
 * Marks a row as a machine rather than a person, with the issuer that vouched
 * for it beside the badge. The subject is the row's own label, so the badge
 * carries what scopes it: a subject means nothing without its issuer.
 */
export function WorkloadSessionBadge({
  workload,
}: {
  workload: UserSessionWorkload;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={<WorkloadTooltip workload={workload} />}>
      <span className="flex min-w-0 shrink items-center gap-2">
        <Badge size="sm" variant="information" background className="shrink-0">
          <Badge.Text>Workload</Badge.Text>
        </Badge>
        <span className="text-muted-foreground truncate text-xs">
          {workloadIssuerLabel(workload)} · {workloadAgentLabel(workload)}
        </span>
      </span>
    </SimpleTooltip>
  );
}

type LadderStep = {
  title: string;
  effect: string;
  stopsReconnect: boolean;
  available: boolean;
};

function withdrawAdmissionEffect(workload: UserSessionWorkload): string {
  switch (workload.admissions.length) {
    case 0:
      return "The kill switch. Nothing admits this workload any more, so it cannot exchange a new token.";
    case 1:
      return `The kill switch. Admitted only by ${workloadAdmissionsLabel(workload)}.`;
    default:
      return `The kill switch. Admitted by ${workload.admissions.length}: ${workloadAdmissionsLabel(workload)}. Withdraw every one, or the workload reconnects through the rest.`;
  }
}

/**
 * Revoking ends the tokens in hand. Whether the workload comes back depends on
 * whether anything still admits it: with no admission left there is nothing to
 * exchange a fresh platform token against.
 */
function revokeStep(
  workload: UserSessionWorkload,
  sessionCount: number,
): LadderStep {
  const subject =
    sessionCount > 1 ? `these ${sessionCount} sessions` : "the session";
  const tokens = sessionCount > 1 ? "those tokens" : "this token";
  const admitted = workload.admissions.length > 0;
  return {
    title: `Revoke ${subject}`,
    effect: admitted
      ? `Ends ${tokens} now. The workload has no refresh token, so it exchanges a fresh platform token and is back within minutes.`
      : `Ends ${tokens} now. Nothing admits this workload, so it has no way back until an admission is added.`,
    stopsReconnect: !admitted,
    available: true,
  };
}

function ladderSteps(
  workload: UserSessionWorkload,
  sessionCount: number,
): LadderStep[] {
  const steps = [revokeStep(workload, sessionCount)];

  // The agent steps describe authority this workload actually holds. With no
  // assignment there is nothing to unassign or suspend, and listing them would
  // send an operator after a control that changes nothing.
  if (workload.agentId) {
    const agent = workload.agentName ?? "the agent";
    steps.push(
      {
        title: `Unassign ${agent} from the workload`,
        effect:
          "The workload still authenticates, but every request it makes is refused.",
        stopsReconnect: false,
        available: false,
      },
      {
        title: `Suspend or revoke ${agent}`,
        effect:
          "Refuses requests from every workload assigned to that agent, not just this one.",
        stopsReconnect: false,
        available: false,
      },
    );
  }

  steps.push(
    {
      title: "Withdraw the admission",
      effect: withdrawAdmissionEffect(workload),
      stopsReconnect: workload.admissions.length > 0,
      available: false,
    },
    {
      title: `Delete the issuer ${workloadIssuerLabel(workload)}`,
      effect: "Stops every workload that issuer vouches for.",
      stopsReconnect: true,
      available: false,
    },
  );

  return steps;
}

/**
 * What an operator can do about a workload, narrowest first, and which of those
 * actually keeps it out. Revoking a session is the only step this page performs;
 * the rest are named so nobody mistakes a revoke for a kill switch.
 */
export function WorkloadRevocationLadder({
  workload,
  sessionCount = 1,
}: {
  workload: UserSessionWorkload;
  /** How many sessions the dialog is about, so the copy names what it ends. */
  sessionCount?: number;
}): JSX.Element {
  return (
    <div className="space-y-2">
      <p className="text-eyebrow">Stopping a workload, narrowest first</p>
      <ol className="border-border divide-border divide-y border">
        {ladderSteps(workload, sessionCount).map((step, index) => (
          <li key={step.title} className="flex gap-3 px-3 py-2 text-xs">
            <span className="text-muted-foreground tabular-nums">
              {index + 1}
            </span>
            <span className="min-w-0 flex-1 space-y-0.5">
              <span className="text-foreground flex flex-wrap items-center gap-2 font-medium">
                {step.title}
                <Badge
                  size="sm"
                  variant={step.stopsReconnect ? "success" : "neutral"}
                >
                  <Badge.Text>
                    {step.stopsReconnect
                      ? "Stops reconnecting"
                      : "Can reconnect"}
                  </Badge.Text>
                </Badge>
                {step.available ? null : (
                  <span className="text-muted-foreground font-normal">
                    Not yet available in the dashboard
                  </span>
                )}
              </span>
              <span className="text-muted-foreground block">{step.effect}</span>
            </span>
          </li>
        ))}
      </ol>
    </div>
  );
}
