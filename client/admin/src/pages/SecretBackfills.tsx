import { useState, type JSX, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Loader2Icon, RefreshCwIcon } from "lucide-react";
import { toast } from "sonner";

import type { AdminMarketplaceTokenBackfill } from "@gram/admin-client/models/components/adminmarketplacetokenbackfill";
import type { AdminRunnerKeyOrganizationBackfill } from "@gram/admin-client/models/components/adminrunnerkeyorganizationbackfill";
import type { AdminSecretBackfillOrganization } from "@gram/admin-client/models/components/adminsecretbackfillorganization";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  runMarketplaceTokenBackfill,
  runRunnerKeyOrganizationBackfill,
  secretBackfillsQuery,
} from "@/lib/gramAdminClient";

type Backfill = "marketplaceTokens" | "runnerKeyOrganizations";

// A run covers one organization, or every organization when organizationId is
// undefined.
type Running = {
  backfill: Backfill;
  organizationId: string | undefined;
};

function plural(n: number, one: string, many: string): string {
  return `${n.toLocaleString()} ${n === 1 ? one : many}`;
}

function marketplaceSummary(run: AdminMarketplaceTokenBackfill): string {
  return `Moved ${plural(run.moved, "token", "tokens")} and gave ${plural(run.attributed, "connection", "connections")} an organization.`;
}

function runnerKeySummary(run: AdminRunnerKeyOrganizationBackfill): string {
  return `Gave ${plural(run.filled, "runner key", "runner keys")} an organization.`;
}

// Prefixes a run's summary with the organization it covered.
function scoped(
  organization: AdminSecretBackfillOrganization | undefined,
  summary: string,
): string {
  return organization ? `${organization.name}: ${summary}` : summary;
}

export function SecretBackfillsPage(): JSX.Element {
  const client = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const backfills = useQuery(secretBackfillsQuery());
  const [running, setRunning] = useState<Running | null>(null);
  const [lastRun, setLastRun] = useState<Partial<Record<Backfill, string>>>({});
  const [filter, setFilter] = useState("");

  const data = backfills.data;
  const fingerprint = data?.platformKeyFingerprint;

  function refresh() {
    // A run changes the totals and which organizations are left, so read
    // them again rather than patch them, whether or not the run finished.
    void client.invalidateQueries({
      queryKey: secretBackfillsQuery().queryKey,
    });
  }

  function isRunning(backfill: Backfill, organizationId: string | undefined) {
    return (
      running?.backfill === backfill &&
      running.organizationId === organizationId
    );
  }

  async function applyMarketplaceTokens(
    checkedFingerprint: string,
    legacy: number,
    unattributed: number,
    organization?: AdminSecretBackfillOrganization,
  ) {
    const ok = await confirm({
      title: organization
        ? `Apply the marketplace token backfill to ${organization.name}?`
        : "Apply the marketplace token backfill to every organization?",
      description: `Moves ${plural(legacy, "plaintext token", "plaintext tokens")} to a hash and an encrypted copy, and gives ${plural(unattributed, "connection", "connections")} an organization${organization ? `, in ${organization.name} only` : ", across every organization"}. Install URLs don't change. Only apply if nobody will roll this environment back to an image without #9425: older images read only the plaintext column. Check that the fingerprint ${checkedFingerprint} matches the servers' first. If the admin server's key has changed since, the run is refused. Safe to run again.`,
      confirmLabel: "Apply",
    });
    if (!ok) return;
    setRunning({
      backfill: "marketplaceTokens",
      organizationId: organization?.organizationId,
    });
    try {
      const run = await runMarketplaceTokenBackfill(
        checkedFingerprint,
        organization?.organizationId,
      );
      const summary = scoped(organization, marketplaceSummary(run));
      setLastRun((current) => ({ ...current, marketplaceTokens: summary }));
      if (run.needsOrganizationKey > 0) {
        toast.warning(
          `${summary} ${plural(run.needsOrganizationKey, "token was", "tokens were")} left in plaintext because their organization needs its own key. Check the server logs.`,
        );
      } else if (run.skipped > 0) {
        toast.warning(
          `${summary} ${plural(run.skipped, "token", "tokens")} changed during the run. Apply again.`,
        );
      } else {
        toast.success(summary);
      }
    } catch (e) {
      // A failed run may still have moved some tokens.
      toast.error(errorMessage(e));
    } finally {
      setRunning(null);
      refresh();
    }
  }

  async function applyRunnerKeyOrganizations(
    pending: number,
    organization?: AdminSecretBackfillOrganization,
  ) {
    const ok = await confirm({
      title: organization
        ? `Apply the runner key organization backfill to ${organization.name}?`
        : "Apply the runner key organization backfill to every organization?",
      description: `Gives ${plural(pending, "runner key", "runner keys")} their project's organization${organization ? `, in ${organization.name} only` : ", across every organization"}. The keys and running Fly runners don't change. Safe to run again.`,
      confirmLabel: "Apply",
    });
    if (!ok) return;
    setRunning({
      backfill: "runnerKeyOrganizations",
      organizationId: organization?.organizationId,
    });
    try {
      const run = await runRunnerKeyOrganizationBackfill(
        organization?.organizationId,
      );
      const summary = scoped(organization, runnerKeySummary(run));
      setLastRun((current) => ({
        ...current,
        runnerKeyOrganizations: summary,
      }));
      if (run.pending > 0) {
        toast.warning(
          `${summary} ${plural(run.pending, "runner key was", "runner keys were")} added during the run. Apply again.`,
        );
      } else {
        toast.success(summary);
      }
    } catch (e) {
      // A failed run may still have filled some batches.
      toast.error(errorMessage(e));
    } finally {
      setRunning(null);
      refresh();
    }
  }

  const needle = filter.trim().toLowerCase();
  const organizations = (data?.organizations ?? []).filter(
    (organization) =>
      needle === "" ||
      organization.name.toLowerCase().includes(needle) ||
      organization.slug.toLowerCase().includes(needle) ||
      organization.organizationId.toLowerCase().includes(needle),
  );

  return (
    <div className="space-y-6 p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Secret backfills</h1>
          <p className="text-muted-foreground max-w-3xl">
            One-off fixes for secrets saved before their encryption code
            shipped. Each card shows what applying it would change right now.
            Apply one organization at a time from the table below, or to every
            organization from a card. Applying is safe to repeat. Temporary:
            this page goes away once both are done in every environment.
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
        <>
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
              busy={isRunning("marketplaceTokens", undefined)}
              disabled={running !== null || !fingerprint}
              onApply={() => {
                if (fingerprint) {
                  void applyMarketplaceTokens(
                    fingerprint,
                    data.marketplaceTokens.legacy,
                    data.marketplaceTokens.unattributed,
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
              busy={isRunning("runnerKeyOrganizations", undefined)}
              disabled={running !== null}
              onApply={() =>
                void applyRunnerKeyOrganizations(
                  data.runnerKeyOrganizations.pending,
                )
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

          <section className="space-y-3">
            <div className="flex flex-wrap items-end justify-between gap-4">
              <div className="space-y-1">
                <h2 className="text-lg font-semibold">By organization</h2>
                <p className="text-muted-foreground max-w-3xl text-sm">
                  Apply to one organization, check it, then go on to the next.
                  Lists only organizations with something left to apply.
                </p>
              </div>
              <Input
                aria-label="Filter organizations"
                placeholder="Filter by name, slug or ID"
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
                className="w-64"
              />
            </div>

            {data.organizations.length === 0 ? (
              <p className="text-muted-foreground text-sm">
                No organization has anything left to apply.
              </p>
            ) : organizations.length === 0 ? (
              <p className="text-muted-foreground text-sm">
                No organization matches the filter.
              </p>
            ) : (
              <div className="rounded-lg border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Organization</TableHead>
                      <TableHead className="text-right whitespace-normal">
                        Tokens in plaintext
                      </TableHead>
                      <TableHead className="text-right whitespace-normal">
                        Connections without an organization
                      </TableHead>
                      <TableHead className="text-right whitespace-normal">
                        Runner keys without an organization
                      </TableHead>
                      <TableHead>
                        <span className="sr-only">Actions</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {organizations.map((organization) => (
                      <OrganizationRow
                        key={organization.organizationId}
                        organization={organization}
                        running={running}
                        fingerprint={fingerprint}
                        onApplyTokens={(checkedFingerprint) =>
                          void applyMarketplaceTokens(
                            checkedFingerprint,
                            organization.legacyTokens,
                            organization.unattributedConnections,
                            organization,
                          )
                        }
                        onApplyRunnerKeys={() =>
                          void applyRunnerKeyOrganizations(
                            organization.pendingRunnerKeys,
                            organization,
                          )
                        }
                      />
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </section>
        </>
      ) : null}
      {confirmDialog}
    </div>
  );
}

function OrganizationRow({
  organization,
  running,
  fingerprint,
  onApplyTokens,
  onApplyRunnerKeys,
}: {
  organization: AdminSecretBackfillOrganization;
  running: Running | null;
  fingerprint: string | undefined;
  onApplyTokens: (checkedFingerprint: string) => void;
  onApplyRunnerKeys: () => void;
}): JSX.Element {
  const tokensLeft =
    organization.legacyTokens > 0 || organization.unattributedConnections > 0;
  const runningHere = running?.organizationId === organization.organizationId;
  return (
    <TableRow>
      <TableCell>
        <Link
          to="/organizations/$idOrSlug"
          params={{ idOrSlug: organization.organizationId }}
          className="font-medium underline-offset-4 hover:underline"
        >
          {organization.name}
        </Link>
        <div className="text-muted-foreground text-xs">{organization.slug}</div>
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {organization.legacyTokens.toLocaleString()}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {organization.unattributedConnections.toLocaleString()}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {organization.pendingRunnerKeys.toLocaleString()}
      </TableCell>
      <TableCell className="pr-3">
        <div className="flex justify-end gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={running !== null || !fingerprint || !tokensLeft}
            onClick={() => {
              if (fingerprint) onApplyTokens(fingerprint);
            }}
          >
            {runningHere && running.backfill === "marketplaceTokens" && (
              <Loader2Icon className="size-4 animate-spin" />
            )}
            Apply tokens
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={running !== null || organization.pendingRunnerKeys === 0}
            onClick={onApplyRunnerKeys}
          >
            {runningHere && running.backfill === "runnerKeyOrganizations" && (
              <Loader2Icon className="size-4 animate-spin" />
            )}
            Apply runner keys
          </Button>
        </div>
      </TableCell>
    </TableRow>
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
          variant="outline"
          size="sm"
          disabled={disabled || nothingToApply}
          onClick={onApply}
        >
          {busy && <Loader2Icon className="size-4 animate-spin" />}
          {nothingToApply ? "Nothing to apply" : "Apply to all organizations"}
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
