import { useState, type JSX, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { toast } from "sonner";
import type { AdminOrganizationEncryption } from "@gram/admin-client/models/components/adminorganizationencryption";
import type { AdminOrganizationEncryptionUseCase } from "@gram/admin-client/models/components/adminorganizationencryptionusecase";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { organizationQuery } from "@/lib/adminQueries";
import { errorMessage, type AdminOrganization } from "@/lib/gramAdminApi";
import {
  enrollOrganizationEncryption,
  finalizeOrganizationEncryption,
  organizationEncryptionQuery,
  organizationEncryptionTransitionsKeyPrefix,
  rollbackOrganizationEncryption,
  runOrganizationEncryption,
} from "@/lib/gramAdminClient";
import {
  encryptionRefreshInterval,
  formatDuration,
  formatTimestamp,
  pluralize,
  TRANSITION_PHASE_LABELS,
  TRANSITION_PHASE_TONES,
  transitionPhase,
  labelForUseCase,
} from "@/lib/organizationEncryption";
import { FinalizeEncryption } from "@/pages/organization/FinalizeEncryption";

function Row({
  label,
  children,
}: {
  label: ReactNode;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="grid grid-cols-1 gap-1 py-1 sm:grid-cols-[12rem_minmax(0,1fr)] sm:items-center sm:gap-3">
      <span className="text-muted-foreground text-sm">{label}</span>
      <div className="min-w-0 text-sm">{children}</div>
    </div>
  );
}

export function EncryptionRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <Encryption key={data.id} org={data} />;
}

export function Encryption({ org }: { org: AdminOrganization }): JSX.Element {
  // Set when a run or finalize is accepted, so polling starts before a worker
  // records that the run began.
  const [requestedAt, setRequestedAt] = useState<number | null>(null);
  const query = useQuery({
    ...organizationEncryptionQuery(org.id),
    // Runs and clears happen in the background, so poll while one may be
    // under way and stop once nothing is.
    refetchInterval: (current) =>
      encryptionRefreshInterval(current.state.data, requestedAt, Date.now()),
  });

  if (query.isPending) {
    return (
      <p role="status" className="text-muted-foreground text-sm">
        Loading...
      </p>
    );
  }
  if (query.isError) {
    return (
      <p role="alert" className="text-sm">
        Unable to load organization encryption. {errorMessage(query.error)}
        <Button variant="ghost" onClick={() => void query.refetch()}>
          Retry
        </Button>
      </p>
    );
  }

  return (
    <EncryptionView
      org={org}
      encryption={query.data}
      onRunRequested={() => setRequestedAt(Date.now())}
    />
  );
}

function EncryptionView({
  org,
  encryption,
  onRunRequested,
}: {
  org: AdminOrganization;
  encryption: AdminOrganizationEncryption;
  onRunRequested: () => void;
}): JSX.Element {
  const config = encryption.config;

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold">Encryption</h1>
        <p className="text-muted-foreground max-w-3xl text-sm">
          Moves this organization&apos;s stored values from the platform key to
          its own key, one use case at a time. Enroll writes both copies, the
          background run converts older values and verifies them, and finalize
          destroys the platform-key copies after{" "}
          {formatDuration(encryption.minimumDualWriteSeconds)} in dual write.
        </p>
      </div>

      <section className="space-y-2 rounded-lg border p-4">
        <h2 className="text-base font-semibold">Key</h2>
        {config ? (
          <div>
            <Row label="Mode">
              {config.mode === "customer_managed"
                ? "Customer managed"
                : "Speakeasy managed"}
            </Row>
            <Row label="State">{config.state}</Row>
            {config.suspendedAt && (
              <Row label="Suspended">
                {formatTimestamp(config.suspendedAt)}
                {config.suspendedReason ? `: ${config.suspendedReason}` : ""}
              </Row>
            )}
            <Row label="Last health check">
              {formatTimestamp(config.lastCheckAt)}
              {config.lastCheckError ? `: ${config.lastCheckError}` : ""}
            </Row>
          </div>
        ) : (
          <p className="text-muted-foreground text-sm">
            No key is provisioned yet. Enrolling a use case provisions one.
          </p>
        )}
      </section>

      {encryption.useCases.map((useCase) => (
        <UseCaseSection
          key={useCase.useCase}
          org={org}
          encryption={encryption}
          useCase={useCase}
          onRunRequested={onRunRequested}
        />
      ))}
    </div>
  );
}

function UseCaseSection({
  org,
  encryption,
  useCase,
  onRunRequested,
}: {
  org: AdminOrganization;
  encryption: AdminOrganizationEncryption;
  useCase: AdminOrganizationEncryptionUseCase;
  onRunRequested: () => void;
}): JSX.Element {
  const client = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const [busy, setBusy] = useState<string | null>(null);
  const label = labelForUseCase(useCase.useCase);
  const phase = transitionPhase(useCase.state, useCase.platformCopiesClearedAt);
  const backfill = useCase.lastBackfillStartedAt
    ? useCase.lastBackfillCompletedAt
      ? `${formatTimestamp(useCase.lastBackfillCompletedAt)}: ${pluralize(useCase.lastBackfillConverted ?? 0, "value")} converted`
      : `Running since ${formatTimestamp(useCase.lastBackfillStartedAt)}`
    : "Never";

  async function refresh() {
    await Promise.all([
      client.invalidateQueries({
        queryKey: organizationEncryptionQuery(org.id).queryKey,
      }),
      client.invalidateQueries({
        queryKey: organizationEncryptionTransitionsKeyPrefix,
      }),
    ]);
  }

  async function act(
    activity: string,
    run: () => Promise<unknown>,
    success: string,
  ) {
    setBusy(activity);
    try {
      await run();
      toast.success(success);
    } catch (e) {
      toast.error(`${label}: ${errorMessage(e)}`);
    } finally {
      // Refresh before re-enabling the buttons, so none acts on stale state.
      await refresh();
      setBusy(null);
    }
  }

  async function enroll() {
    const ok = await confirm({
      title: `Enroll ${org.name} for ${label}?`,
      description:
        "Provisions the organization's key, then writes both a platform-key and an organization-key copy of every new value. Nothing changes if provisioning fails. It can be rolled back until finalize.",
      confirmLabel: "Enroll",
    });
    if (!ok) return;
    await act(
      "Enrolling",
      () => enrollOrganizationEncryption(org.id, useCase.useCase),
      `Enrolled ${org.name} for ${label}`,
    );
  }

  async function rollback() {
    const ok = await confirm({
      title: `Roll back ${label} for ${org.name}?`,
      description:
        "Returns to writing and reading only the platform-key copy. Both copies still exist, so nothing is lost. The organization can be enrolled again later.",
      confirmLabel: "Roll back",
      destructive: true,
    });
    if (!ok) return;
    await act(
      "Rolling back",
      () => rollbackOrganizationEncryption(org.id, useCase.useCase),
      `Rolled back ${label} for ${org.name}`,
    );
  }

  async function run() {
    await act(
      phase === "clearing" ? "Resuming" : "Starting",
      async () => {
        await runOrganizationEncryption(org.id, useCase.useCase);
        onRunRequested();
      },
      phase === "clearing"
        ? `Requested clearing ${label} for ${org.name}`
        : `Requested a backfill of ${label} for ${org.name}; it waits for any run already in progress`,
    );
  }

  async function finalize(submission: {
    confirmation: string;
    acknowledgedUnreadableValues: number;
  }) {
    try {
      const result = await finalizeOrganizationEncryption({
        organizationID: org.id,
        useCase: useCase.useCase,
        ...submission,
      });
      onRunRequested();
      toast.success(
        result.clearingStarted
          ? `Finalized ${label} for ${org.name}; clearing platform-key copies`
          : `Finalized ${label} for ${org.name}; the hourly run clears the platform-key copies`,
      );
    } finally {
      await refresh();
    }
  }

  return (
    <section
      aria-labelledby={`encryption-${useCase.useCase}`}
      className="space-y-3 rounded-lg border p-4"
    >
      {confirmDialog}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <h2
            id={`encryption-${useCase.useCase}`}
            className="text-base font-semibold"
          >
            {label}
          </h2>
          <Badge variant="outline" className={TRANSITION_PHASE_TONES[phase]}>
            {TRANSITION_PHASE_LABELS[phase]}
          </Badge>
        </div>
        <div className="flex flex-wrap gap-2">
          {phase === "platform_key" && (
            <Button
              size="sm"
              disabled={busy !== null}
              onClick={() => void enroll()}
            >
              {busy ?? "Enroll"}
            </Button>
          )}
          {(phase === "dual_write" || phase === "clearing") && (
            <Button
              size="sm"
              variant="outline"
              disabled={busy !== null || !encryption.runsAvailable}
              title={
                encryption.runsAvailable
                  ? undefined
                  : "This server cannot start runs; the hourly background run continues."
              }
              onClick={() => void run()}
            >
              {busy === "Starting" || busy === "Resuming"
                ? busy
                : phase === "clearing"
                  ? "Resume clearing"
                  : "Run backfill now"}
            </Button>
          )}
          {phase === "dual_write" && (
            <>
              <Button
                size="sm"
                variant="outline"
                disabled={busy !== null}
                onClick={() => void rollback()}
              >
                {busy === "Rolling back" ? busy : "Roll back"}
              </Button>
              <FinalizeEncryption
                organizationName={org.name}
                useCase={useCase.useCase}
                unreadableValues={useCase.unreadableValues}
                platformCopies={useCase.platformCopies}
                confirmation={encryption.finalizeConfirmation}
                disabled={busy !== null || useCase.finalizeBlockers.length > 0}
                onSubmit={finalize}
              />
            </>
          )}
        </div>
      </div>

      {phase === "clearing" && (
        <p role="status" className="text-muted-foreground text-sm">
          Finalized. Clearing the platform-key copies is in progress or was
          interrupted; this page refreshes until it completes. An interrupted
          clear resumes on the hourly background run, or resume it now.
        </p>
      )}

      <div>
        <Row label="State changed">
          {formatTimestamp(useCase.stateChangedAt)}
        </Row>
        <Row label="Waiting for an organization-key copy">
          {useCase.waitingValues.toLocaleString()}
        </Row>
        <Row label="Unreadable">
          {useCase.unreadableValues.toLocaleString()}
        </Row>
        {useCase.unattributedValues > 0 && (
          <Row label="Without an organization">
            {useCase.unattributedValues.toLocaleString()}
          </Row>
        )}
        <Row label="Platform-key copies">
          {useCase.platformCopiesClearedAt
            ? `Cleared ${formatTimestamp(useCase.platformCopiesClearedAt)}`
            : useCase.platformCopies.toLocaleString()}
        </Row>
        <Row label="Last backfill">{backfill}</Row>
        {useCase.lastBackfillError && (
          <Row label="Last backfill error">
            <span className="text-destructive">
              {useCase.lastBackfillError}
            </span>
          </Row>
        )}
        <Row label="Last verification">
          {formatTimestamp(useCase.lastVerifiedAt)}
        </Row>
      </div>

      {phase === "dual_write" && useCase.finalizeBlockers.length > 0 && (
        <div className="text-sm">
          <p className="font-medium">Finalize is blocked:</p>
          <ul className="text-muted-foreground list-disc pl-5">
            {useCase.finalizeBlockers.map((blocker) => (
              <li key={blocker}>{blocker}</li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
