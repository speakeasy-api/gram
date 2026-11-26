import { useState, type JSX, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2Icon, RefreshCwIcon } from "lucide-react";
import { toast } from "sonner";

import type { AdminMarketplaceTokenBackfill } from "@gram/admin-client/models/components/adminmarketplacetokenbackfill";
import type { AdminRunnerKeyOrganizationBackfill } from "@gram/admin-client/models/components/adminrunnerkeyorganizationbackfill";
import type { AdminSecretBackfills } from "@gram/admin-client/models/components/adminsecretbackfills";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  runMarketplaceTokenBackfill,
  runRunnerKeyOrganizationBackfill,
  secretBackfillsQuery,
} from "@/lib/gramAdminClient";

type Backfill = "marketplaceTokens" | "runnerKeyOrganizations";

function plural(n: number, one: string, many: string): string {
  return `${n.toLocaleString()} ${n === 1 ? one : many}`;
}

function marketplaceSummary(run: AdminMarketplaceTokenBackfill): string {
  return `Moved ${plural(run.moved, "token", "tokens")} and gave ${plural(run.attributed, "connection", "connections")} an organization.`;
}

function runnerKeySummary(run: AdminRunnerKeyOrganizationBackfill): string {
  return `Gave ${plural(run.filled, "runner key", "runner keys")} an organization.`;
}

export function SecretBackfillsPage(): JSX.Element {
  const client = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const backfills = useQuery(secretBackfillsQuery());
  const [running, setRunning] = useState<Backfill | null>(null);
  const [lastRun, setLastRun] = useState<Partial<Record<Backfill, string>>>({});

  const data = backfills.data;
  const fingerprint = data?.platformKeyFingerprint;

  function updateCache(patch: Partial<AdminSecretBackfills>) {
    client.setQueryData<AdminSecretBackfills>(
      secretBackfillsQuery().queryKey,
      (current) => current && { ...current, ...patch },
    );
  }

  async function applyMarketplaceTokens(
    preview: AdminMarketplaceTokenBackfill,
    checkedFingerprint: string,
  ) {
    const ok = await confirm({
      title: "Apply the marketplace token backfill?",
      description: `Moves ${plural(preview.legacy, "plaintext token", "plaintext tokens")} to a hash and an encrypted copy, and gives ${plural(preview.unattributed, "connection", "connections")} an organization. Install URLs don't change. Only apply if nobody will roll this environment back to an image without #9425: older images read only the plaintext column. Check that the fingerprint ${checkedFingerprint} matches the servers' first. If the admin server's key has changed since, the run is refused. Safe to run again.`,
      confirmLabel: "Apply",
    });
    if (!ok) return;
    setRunning("marketplaceTokens");
    try {
      const run = await runMarketplaceTokenBackfill(checkedFingerprint);
      updateCache({ marketplaceTokens: run });
      setLastRun((current) => ({
        ...current,
        marketplaceTokens: marketplaceSummary(run),
      }));
      if (run.needsOrganizationKey > 0) {
        toast.warning(
          `${marketplaceSummary(run)} ${plural(run.needsOrganizationKey, "token was", "tokens were")} left in plaintext because their organization needs its own key. Check the server logs.`,
        );
      } else if (run.skipped > 0) {
        toast.warning(
          `${marketplaceSummary(run)} ${plural(run.skipped, "token", "tokens")} changed during the run. Apply again.`,
        );
      } else {
        toast.success(marketplaceSummary(run));
      }
    } catch (e) {
      toast.error(errorMessage(e));
      // A failed run may still have moved some tokens.
      void client.invalidateQueries({
        queryKey: secretBackfillsQuery().queryKey,
      });
    } finally {
      setRunning(null);
    }
  }

  async function applyRunnerKeyOrganizations(
    preview: AdminRunnerKeyOrganizationBackfill,
  ) {
    const ok = await confirm({
      title: "Apply the runner key organization backfill?",
      description: `Gives ${plural(preview.pending, "runner key", "runner keys")} their project's organization. The keys and running Fly runners don't change. Safe to run again.`,
      confirmLabel: "Apply",
    });
    if (!ok) return;
    setRunning("runnerKeyOrganizations");
    try {
      const run = await runRunnerKeyOrganizationBackfill();
      updateCache({ runnerKeyOrganizations: run });
      setLastRun((current) => ({
        ...current,
        runnerKeyOrganizations: runnerKeySummary(run),
      }));
      if (run.pending > 0) {
        toast.warning(
          `${runnerKeySummary(run)} ${plural(run.pending, "runner key was", "runner keys were")} added during the run. Apply again.`,
        );
      } else {
        toast.success(runnerKeySummary(run));
      }
    } catch (e) {
      toast.error(errorMessage(e));
      // A failed run may still have filled some batches.
      void client.invalidateQueries({
        queryKey: secretBackfillsQuery().queryKey,
      });
    } finally {
      setRunning(null);
    }
  }

  return (
    <div className="space-y-6 p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Secret backfills</h1>
          <p className="text-muted-foreground max-w-3xl">
            One-off fixes for secrets saved before their encryption code
            shipped. Each card shows what applying it would change right now.
            Applying is safe to repeat. Temporary: this page goes away once both
            are done in every environment.
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={backfills.isFetching || running !== null}
          onClick={() => void backfills.refetch()}
        >
          <RefreshCwIcon className="size-4" />
          Refresh
        </Button>
      </div>

      {backfills.isLoading ? (
        <div className="text-muted-foreground flex items-center gap-2">
          <Loader2Icon className="size-4 animate-spin" /> Loading
        </div>
      ) : backfills.error ? (
        <p className="text-destructive">{errorMessage(backfills.error)}</p>
      ) : data ? (
        <div className="grid gap-4 lg:grid-cols-2">
          <BackfillCard
            title="Marketplace tokens"
            description="Gives each GitHub connection its project's organization, then replaces each plaintext marketplace token with its hash and an encrypted copy."
            status={
              data.marketplaceTokens.legacy === 0 &&
              data.marketplaceTokens.unattributed === 0
                ? "done"
                : "todo"
            }
            counts={[
              ["Tokens still in plaintext", data.marketplaceTokens.legacy],
              [
                "Connections without an organization",
                data.marketplaceTokens.unattributed,
              ],
              ["Tokens already hashed", data.marketplaceTokens.hashed],
            ]}
            lastRun={lastRun.marketplaceTokens}
            busy={running === "marketplaceTokens"}
            disabled={running !== null || !fingerprint}
            onApply={() => {
              if (fingerprint) {
                void applyMarketplaceTokens(
                  data.marketplaceTokens,
                  fingerprint,
                );
              }
            }}
          >
            {fingerprint ? (
              <p className="text-muted-foreground text-sm">
                Encrypts with platform key{" "}
                <code className="text-foreground">{fingerprint}</code>. Before
                you apply, check it matches{" "}
                <code>gram.encryption.key_fingerprint</code> on the servers'{" "}
                <code>encryption key loaded</code> log.
              </p>
            ) : (
              <p className="text-destructive text-sm">
                This admin server has no platform key, so it can't encrypt
                tokens. Previews still work.
              </p>
            )}
          </BackfillCard>

          <BackfillCard
            title="Runner key organizations"
            description="Gives each function runner key its project's organization, so it can later move to the organization's own key."
            status={
              data.runnerKeyOrganizations.pending > 0
                ? "todo"
                : data.runnerKeyOrganizations.missingOrganization > 0
                  ? "attention"
                  : "done"
            }
            counts={[
              [
                "Runner keys without an organization",
                data.runnerKeyOrganizations.pending,
              ],
              [
                "Runner keys that can't get one",
                data.runnerKeyOrganizations.missingOrganization,
              ],
            ]}
            lastRun={lastRun.runnerKeyOrganizations}
            busy={running === "runnerKeyOrganizations"}
            disabled={running !== null}
            onApply={() =>
              void applyRunnerKeyOrganizations(data.runnerKeyOrganizations)
            }
          >
            {data.runnerKeyOrganizations.missingOrganization > 0 && (
              <p className="text-destructive text-sm">
                {plural(
                  data.runnerKeyOrganizations.missingOrganization,
                  "runner key belongs",
                  "runner keys belong",
                )}{" "}
                to a project whose organization has no metadata row. Look into
                them before any organization moves function runner keys to its
                own key.
              </p>
            )}
          </BackfillCard>
        </div>
      ) : null}
      {confirmDialog}
    </div>
  );
}

// "attention" means applying has nothing left to do, but some rows still lack
// what the backfill gives, so it isn't finished.
type BackfillStatus = "todo" | "attention" | "done";

const statusBadge: Record<BackfillStatus, JSX.Element> = {
  todo: <Badge variant="secondary">To do</Badge>,
  attention: <Badge variant="destructive">Needs a look</Badge>,
  done: <Badge>Done</Badge>,
};

function BackfillCard({
  title,
  description,
  status,
  counts,
  lastRun,
  busy,
  disabled,
  onApply,
  children,
}: {
  title: string;
  description: string;
  status: BackfillStatus;
  counts: Array<[string, number]>;
  lastRun: string | undefined;
  busy: boolean;
  disabled: boolean;
  onApply: () => void;
  children?: ReactNode;
}): JSX.Element {
  const nothingToApply = status !== "todo";
  return (
    <section className="flex flex-col gap-4 rounded-lg border p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-1">
          <h2 className="text-lg font-semibold">{title}</h2>
          <p className="text-muted-foreground text-sm">{description}</p>
        </div>
        {statusBadge[status]}
      </div>

      <dl className="grid grid-cols-[1fr_auto] gap-x-6 gap-y-1 text-sm">
        {counts.map(([label, value]) => (
          <div key={label} className="contents">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="text-right tabular-nums">
              {value.toLocaleString()}
            </dd>
          </div>
        ))}
      </dl>

      {children}

      <div className="mt-auto flex flex-wrap items-center gap-3">
        <Button
          size="sm"
          disabled={disabled || nothingToApply}
          onClick={onApply}
        >
          {busy && <Loader2Icon className="size-4 animate-spin" />}
          {nothingToApply ? "Nothing to apply" : "Apply"}
        </Button>
        {lastRun && (
          <span className="text-muted-foreground text-sm">
            Last run: {lastRun}
          </span>
        )}
      </div>
    </section>
  );
}
