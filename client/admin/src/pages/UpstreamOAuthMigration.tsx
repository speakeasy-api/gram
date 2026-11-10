import { useState, type JSX } from "react";
import {
  useQueries,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { CheckCircle2Icon, CircleDashedIcon, Loader2Icon } from "lucide-react";
import { toast } from "sonner";

import type { AdminUpstreamOAuthMigrationOrganization } from "@gram/admin-client/models/components/adminupstreamoauthmigrationorganization";
import type { AdminUpstreamOAuthMigrationAuthClients } from "@gram/admin-client/models/components/adminupstreamoauthmigrationauthclients";
import type { AdminUpstreamOAuthMigrationStatus } from "@gram/admin-client/models/components/adminupstreamoauthmigrationstatus";
import type { AdminUpstreamOAuthMigrationUnresolvedAssociation } from "@gram/admin-client/models/components/adminupstreamoauthmigrationunresolvedassociation";

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
import { needsMigrationRun } from "@/lib/upstreamOAuthMigration";
import {
  backfillMcpAuthGrants,
  disableUpstreamAuthEnforcement,
  enableUpstreamAuthEnforcement,
  runUpstreamOAuthMigration,
  upstreamOAuthMigrationOrganizationsQuery,
  upstreamOAuthMigrationStatusQuery,
} from "@/lib/gramAdminClient";

// Each status is a dry run of both backfills, so only a few load at once.
const STATUS_CONCURRENCY = 4;

export function UpstreamOAuthMigrationPage(): JSX.Element {
  const client = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const organizations = useQuery(upstreamOAuthMigrationOrganizationsQuery());
  const [filter, setFilter] = useState("");
  // What each busy organization is doing, by id.
  const [running, setRunning] = useState<Map<string, string>>(new Map());
  const [bulk, setBulk] = useState<{ done: number; total: number } | null>(
    null,
  );
  const [backfilling, setBackfilling] = useState(false);

  const all = organizations.data?.organizations ?? [];
  const term = filter.trim().toLowerCase();
  const visible = term
    ? all.filter(
        (org) =>
          org.name.toLowerCase().includes(term) ||
          org.slug.toLowerCase().includes(term) ||
          org.id === filter.trim(),
      )
    : all;

  // Settled statuses open the window for the next ones; every settled query
  // re-renders the page, which moves the window on. A cached status that is
  // refetching counts as unsettled, so a revisit refetches a few at a time.
  const settled = all.filter((org) => {
    const state = client.getQueryState(
      upstreamOAuthMigrationStatusQuery(org.id).queryKey,
    );
    return (
      state?.fetchStatus === "idle" &&
      (state.status === "success" || state.status === "error")
    );
  }).length;
  const statuses = useQueries({
    queries: all.map((org, index) => ({
      ...upstreamOAuthMigrationStatusQuery(org.id),
      enabled: index < settled + STATUS_CONCURRENCY,
    })),
  });
  const statusByID = new Map(all.map((org, i) => [org.id, statuses[i]]));
  const migrated = statuses.filter((s) => s.data?.migrated).length;
  const pendingCount = statuses.filter((s) => needsMigrationRun(s.data)).length;

  async function migrate(org: AdminUpstreamOAuthMigrationOrganization) {
    setRunning((current) => new Map(current).set(org.id, "Migrating"));
    try {
      const run = await runUpstreamOAuthMigration(org.id);
      client.setQueryData(
        upstreamOAuthMigrationStatusQuery(org.id).queryKey,
        run.status,
      );
      return run;
    } finally {
      setRunning((current) => {
        const next = new Map(current);
        next.delete(org.id);
        return next;
      });
    }
  }

  async function migrateOne(org: AdminUpstreamOAuthMigrationOrganization) {
    const ok = await confirm({
      title: `Migrate ${org.name}?`,
      description:
        "Records the upstream OAuth client associations its configuration proves, then each live credential's client choice, then a typed auth client for each upstream identity its servers use, and grants the default auth permissions for them. Safe to run again; it only adds permissions, and never restores one an admin removed.",
      confirmLabel: "Migrate",
    });
    if (!ok) return;
    try {
      const run = await migrate(org);
      if (run.authClientsError) {
        toast.warning(`${org.name}: ${run.authClientsError}`);
      }
      if (!run.status.migrated) {
        toast.warning(`${org.name} still needs attention`);
      } else if (!run.authClientsError) {
        toast.success(`${org.name} is migrated`);
      }
    } catch (e) {
      toast.error(`${org.name}: ${errorMessage(e)}`);
    }
  }

  async function toggleEnforcement(
    org: AdminUpstreamOAuthMigrationOrganization,
    enable: boolean,
  ) {
    const ok = await confirm(
      enable
        ? {
            title: `Enforce upstream auth for ${org.name}?`,
            description:
              "Its servers and assistant turns are served by the auth client each credential chose, and using it requires permission. Confirm that its mandatory authorization shadow shows no unexplained new denial, and that it was told valid credentials from another organization are refused on its public servers instead of served anonymously. Credentials with an ambiguous choice are asked to reconnect.",
            confirmLabel: "Enforce",
          }
        : {
            title: `Stop enforcing upstream auth for ${org.name}?`,
            description:
              "Requests go back to the upstream client choice within a minute. Choices keep being recorded, so enforcement can be turned on again later.",
            confirmLabel: "Stop enforcing",
          },
    );
    if (!ok) return;
    setRunning((current) =>
      new Map(current).set(org.id, "Updating enforcement"),
    );
    try {
      const status = enable
        ? await enableUpstreamAuthEnforcement(org.id)
        : await disableUpstreamAuthEnforcement(org.id);
      client.setQueryData(
        upstreamOAuthMigrationStatusQuery(org.id).queryKey,
        status,
      );
      toast.success(
        enable
          ? `Enforcing upstream auth for ${org.name}`
          : `Stopped enforcing upstream auth for ${org.name}`,
      );
    } catch (e) {
      toast.error(`${org.name}: ${errorMessage(e)}`);
    } finally {
      setRunning((current) => {
        const next = new Map(current);
        next.delete(org.id);
        return next;
      });
    }
  }

  async function migrateAllPending() {
    const pending = all.filter((org) =>
      needsMigrationRun(statusByID.get(org.id)?.data),
    );
    if (pending.length === 0) return;
    const ok = await confirm({
      title: `Migrate ${pending.length} ${pending.length === 1 ? "organization" : "organizations"}?`,
      description:
        "Runs the migration for every organization not yet migrated or with auth clients left to record, one at a time, and grants the default auth permissions for each. It only adds permissions, and never restores one an admin removed. Keep this tab open until it finishes; an interrupted run is safe to start again.",
      confirmLabel: "Migrate all",
    });
    if (!ok) return;
    setBulk({ done: 0, total: pending.length });
    let failed = 0;
    let incomplete = 0;
    let authClientsFailed = 0;
    for (const org of pending) {
      try {
        const run = await migrate(org);
        if (!run.status.migrated) incomplete++;
        if (run.authClientsError) authClientsFailed++;
      } catch (e) {
        failed++;
        toast.error(`${org.name}: ${errorMessage(e)}`);
      }
      setBulk((current) =>
        current ? { ...current, done: current.done + 1 } : current,
      );
    }
    setBulk(null);
    if (authClientsFailed > 0) {
      toast.warning(
        `Auth clients weren't recorded for ${authClientsFailed} ${authClientsFailed === 1 ? "organization" : "organizations"}. Run again; the migration itself is unaffected.`,
      );
    }
    if (incomplete > 0) {
      toast.warning(
        `${incomplete} ${incomplete === 1 ? "organization still needs" : "organizations still need"} attention`,
      );
    } else if (failed === 0 && authClientsFailed === 0) {
      toast.success(
        `Migrated ${pending.length} ${pending.length === 1 ? "organization" : "organizations"}`,
      );
    }
  }

  async function backfillAuthGrants() {
    const ok = await confirm({
      title: "Grant mcp:auth to everyone with mcp:connect?",
      description:
        "Every principal holding any mcp:connect grant, in every organization, gets unrestricted mcp:auth. It only adds grants and is safe to run again.",
      confirmLabel: "Grant",
    });
    if (!ok) return;
    setBackfilling(true);
    try {
      const result = await backfillMcpAuthGrants();
      toast.success(`Granted mcp:auth to ${result.granted} principals`);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBackfilling(false);
    }
  }

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Upstream OAuth migration</h1>
          <p className="text-muted-foreground max-w-3xl">
            Records which upstream OAuth client each MCP server uses, which
            client each signed-in credential chose, and a typed auth client for
            each associated client, for organizations whose servers sign in
            through a user session issuer. Auth clients never decide whether an
            organization is migrated. Migrate one organization to try it, then
            the rest.
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            onClick={() => void backfillAuthGrants()}
            disabled={backfilling}
          >
            {backfilling
              ? "Granting mcp:auth"
              : "Grant mcp:auth from mcp:connect"}
          </Button>
          <Button
            onClick={() => void migrateAllPending()}
            disabled={
              bulk !== null ||
              all.length === 0 ||
              settled < all.length ||
              pendingCount === 0
            }
          >
            {bulk
              ? `Migrating ${bulk.done} of ${bulk.total}`
              : "Migrate all pending"}
          </Button>
        </div>
      </div>

      <div className="flex items-center justify-between gap-4">
        <Input
          aria-label="Filter organizations"
          placeholder="Filter by name, slug or ID"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="max-w-sm"
        />
        <p className="text-muted-foreground text-sm">
          {migrated} of {all.length} migrated
          {settled < all.length
            ? `, ${all.length - settled} still checking`
            : ""}
        </p>
      </div>

      {organizations.isError ? (
        <p className="text-destructive">{errorMessage(organizations.error)}</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Organization</TableHead>
              <TableHead className="text-right">Servers</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="w-0" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {organizations.isPending ? (
              <TableRow>
                <TableCell colSpan={4} className="text-muted-foreground">
                  Loading organizations…
                </TableCell>
              </TableRow>
            ) : visible.length === 0 ? (
              <TableRow>
                <TableCell colSpan={4} className="text-muted-foreground">
                  {all.length === 0
                    ? "No organization has a server signed in through a user session issuer."
                    : "No organization matches."}
                </TableCell>
              </TableRow>
            ) : (
              visible.map((org) => {
                const status = statusByID.get(org.id);
                const activity = running.get(org.id);
                const busy = activity !== undefined;
                return (
                  <TableRow key={org.id}>
                    <TableCell>
                      <div className="font-medium">{org.name}</div>
                      <div className="text-muted-foreground font-mono text-xs">
                        {org.slug}
                      </div>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {org.servers}
                    </TableCell>
                    <TableCell>
                      <MigrationStatus status={status} activity={activity} />
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-2">
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => void migrateOne(org)}
                          disabled={busy || bulk !== null}
                        >
                          {status?.data?.migrated ? "Run again" : "Migrate"}
                        </Button>
                        {status?.data?.enforcement?.enabled ? (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => void toggleEnforcement(org, false)}
                            disabled={busy || bulk !== null}
                          >
                            Stop enforcing
                          </Button>
                        ) : status?.data?.enforcement?.ready ? (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => void toggleEnforcement(org, true)}
                            disabled={busy || bulk !== null}
                          >
                            Enforce
                          </Button>
                        ) : null}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      )}
      {confirmDialog}
    </div>
  );
}

function MigrationStatus({
  status,
  activity,
}: {
  status: UseQueryResult<AdminUpstreamOAuthMigrationStatus> | undefined;
  activity: string | undefined;
}): JSX.Element {
  if (activity !== undefined) {
    return (
      <span className="text-muted-foreground inline-flex items-center gap-1.5">
        <Loader2Icon aria-hidden="true" className="size-4 animate-spin" />
        {activity}
      </span>
    );
  }
  if (!status || status.isPending) {
    return <span className="text-muted-foreground">Checking…</span>;
  }
  if (status.isError) {
    return (
      <span className="text-destructive">{errorMessage(status.error)}</span>
    );
  }
  const s = status.data;
  const authClients = (
    <>
      <AuthClientStatus status={s} />
      <ExecutionProfileStatus status={s} />
      <EnforcementStatus status={s} />
    </>
  );
  const reconnect =
    s.reconnectCredentials > 0
      ? `${s.reconnectCredentials} ${s.reconnectCredentials === 1 ? "credential" : "credentials"} will be asked to reconnect`
      : null;
  if (s.migrated) {
    return (
      <div className="space-y-1">
        <span className="inline-flex items-center gap-1.5 text-emerald-600 dark:text-emerald-400">
          <CheckCircle2Icon aria-hidden="true" className="size-4" />
          Migrated
        </span>
        {reconnect ? (
          <p className="text-muted-foreground text-xs">{reconnect}</p>
        ) : null}
        {authClients}
      </div>
    );
  }
  return (
    <div className="space-y-1">
      <span className="text-muted-foreground inline-flex items-center gap-1.5">
        <CircleDashedIcon aria-hidden="true" className="size-4" />
        Not migrated
      </span>
      <div className="flex flex-wrap gap-1.5">
        {s.pendingAssociations > 0 ? (
          <Badge variant="secondary">
            {s.pendingAssociations}{" "}
            {s.pendingAssociations === 1 ? "association" : "associations"} to
            record
          </Badge>
        ) : null}
        {s.pendingSelections > 0 ? (
          <Badge variant="secondary">
            {s.pendingSelections} credential{" "}
            {s.pendingSelections === 1 ? "choice" : "choices"} to record
          </Badge>
        ) : null}
        {s.unresolvedServers.length > 0 ? (
          <Badge variant="destructive">
            {s.unresolvedServers.length}{" "}
            {s.unresolvedServers.length === 1 ? "server needs" : "servers need"}{" "}
            a client configured
          </Badge>
        ) : null}
      </div>
      {s.unresolvedServers.length > 0 ? (
        <ul className="text-muted-foreground font-mono text-xs">
          {s.unresolvedServers.map((server) => (
            <li key={server.mcpServerId}>
              server {server.mcpServerId} in project {server.projectId}
            </li>
          ))}
        </ul>
      ) : null}
      {reconnect ? (
        <p className="text-muted-foreground text-xs">{reconnect}</p>
      ) : null}
      {authClients}
    </div>
  );
}

// Typed auth clients are reported apart from the migration: they never decide
// whether an organization is migrated.
function AuthClientStatus({
  status,
}: {
  status: AdminUpstreamOAuthMigrationStatus;
}): JSX.Element {
  const a = status.authClients;
  if (!a) {
    return (
      <p className="text-muted-foreground text-xs">
        Auth clients: couldn't check
      </p>
    );
  }
  if (a.inProgress) {
    return (
      <p className="text-muted-foreground text-xs">
        Auth clients: run in progress
      </p>
    );
  }
  if (a.complete) {
    return (
      <div className="space-y-1">
        <p className="text-muted-foreground text-xs">Auth clients: recorded</p>
        <AuthClientItems items={a.warnings} label="Worth a look" />
        <SelectionCoverage authClients={a} />
      </div>
    );
  }
  const parts = [
    a.pendingClients > 0
      ? `${a.pendingClients} ${a.pendingClients === 1 ? "client" : "clients"} to record`
      : null,
    a.pendingLinks > 0
      ? `${a.pendingLinks} server ${a.pendingLinks === 1 ? "link" : "links"} to record`
      : null,
    a.pendingHeaders > 0
      ? `${a.pendingHeaders} ${a.pendingHeaders === 1 ? "header or scope set" : "headers or scope sets"} to update`
      : null,
    a.pendingRetirements > 0
      ? `${a.pendingRetirements} stale ${a.pendingRetirements === 1 ? "record" : "records"} to retire`
      : null,
    a.unresolved > 0 ? `${a.unresolved} need attention` : null,
    (a.attachmentsWaiting ?? 0) > 0
      ? `${a.attachmentsWaiting} agent ${a.attachmentsWaiting === 1 ? "attachment grant waits" : "attachment grants wait"} until upstream selector writes are allowed for this organization`
      : null,
  ].filter((part) => part !== null);
  const kinds = a.kinds.filter(
    (k) =>
      k.pendingClients +
        k.pendingLinks +
        k.pendingHeaders +
        k.pendingRetirements +
        k.unresolved >
      0,
  );
  return (
    <div className="space-y-1">
      <p className="text-muted-foreground text-xs">
        Auth clients: {parts.length > 0 ? parts.join(", ") : "run again"}
      </p>
      {a.failedPhases.length > 0 ? (
        <p className="text-destructive text-xs">
          Couldn't check: {a.failedPhases.join(", ")}
        </p>
      ) : null}
      {kinds.length > 0 ? (
        <ul className="text-muted-foreground text-xs">
          {kinds.map((k) => (
            <li key={k.kind}>
              {k.kind}: {k.pendingClients} clients, {k.pendingLinks} links,{" "}
              {k.pendingHeaders} headers to record; {k.pendingRetirements} to
              retire; {k.unresolved} need attention
            </li>
          ))}
        </ul>
      ) : null}
      <AuthClientItems items={a.unresolvedAssociations} />
      <AuthClientItems items={a.warnings} label="Worth a look" />
      <SelectionCoverage authClients={a} />
    </div>
  );
}

// Which typed auth client each live credential's choice names, by credential
// class. Gaps are never guessed: the server's configuration decides them, or
// the credential reconnects.
function SelectionCoverage({
  authClients: a,
}: {
  authClients: AdminUpstreamOAuthMigrationAuthClients;
}): JSX.Element | null {
  // A server older than credential choice coverage omits it while a deploy
  // rolls out: that is unknown, never complete.
  const classes = a.selectionClasses;
  if (classes === undefined || a.selectionsComplete === undefined) {
    return (
      <p className="text-muted-foreground text-xs">
        Credential choices: coverage unknown; this server did not report it.
        Check again once the deploy finishes.
      </p>
    );
  }
  if (classes.length === 0) {
    return null;
  }
  const gaps = a.selectionGaps ?? [];
  const gapCount = a.selectionGapCount ?? gaps.length;
  return (
    <div className="space-y-1">
      <p className="text-muted-foreground text-xs">
        Credential choices:{" "}
        {a.selectionsComplete
          ? "every choice is covered (auth client or explicit no-auth)"
          : `${a.pendingSelectionMappings ?? "unknown"} to name, ${gapCount} without a usable auth client`}
      </p>
      <ul className="text-muted-foreground text-xs">
        {classes.map((c) => (
          <li key={c.credentialClass}>
            {c.credentialClass}: {c.mapped} of {c.selections} named, {c.noAuth}{" "}
            no-auth, {c.pending} to name
            {c.ambiguous > 0 ? `, ${c.ambiguous} ambiguous` : ""}
            {c.noClient > 0 ? `, ${c.noClient} without a client` : ""}
            {c.staleNoAuth + c.unusable > 0
              ? `, ${c.staleNoAuth + c.unusable} to reconnect`
              : ""}
          </li>
        ))}
      </ul>
      {gaps.length > 0 ? (
        <ul className="text-muted-foreground font-mono text-xs">
          {gaps.map((gap) => (
            <li key={gap.selectionId}>
              {gap.credentialClass} {gap.outcome}: credential {gap.credentialId}
              , server {gap.mcpServerId} in project {gap.projectId}
            </li>
          ))}
          {gapCount > gaps.length ? (
            <li>and {gapCount - gaps.length} more</li>
          ) : null}
        </ul>
      ) : null}
    </div>
  );
}

function AuthClientItems({
  items,
  label,
}: {
  items: AdminUpstreamOAuthMigrationUnresolvedAssociation[];
  label?: string;
}): JSX.Element | null {
  if (items.length === 0) {
    return null;
  }
  return (
    <div className="space-y-1">
      {label ? <p className="text-muted-foreground text-xs">{label}:</p> : null}
      <ul className="text-muted-foreground font-mono text-xs">
        {items.map((item) => (
          <li
            key={`${item.kind}:${item.reason}:${item.subjectId}:${item.mcpServerId}`}
          >
            {item.kind} {item.reason}: {item.subjectId}
            {item.projectId !== NIL_UUID ? ` in project ${item.projectId}` : ""}
            {item.mcpServerId !== NIL_UUID
              ? `, server ${item.mcpServerId}`
              : ""}
            {item.remoteSessionClientId !== NIL_UUID &&
            item.remoteSessionClientId !== item.subjectId
              ? `, client ${item.remoteSessionClientId}`
              : ""}
            {item.bindingIds.length > 0
              ? `, bindings ${item.bindingIds.join(", ")}`
              : ""}
            {item.sourceIds.length > 0
              ? `, sources ${item.sourceIds.join(", ")}`
              : ""}
            {item.serverIds.length > 0
              ? `, servers ${item.serverIds.join(", ")}`
              : ""}
          </li>
        ))}
      </ul>
    </div>
  );
}

// The auth client choices assistant turns use, per assistant, trigger and
// execution context. Gaps are never guessed: the server's auth clients are
// configured instead.
function ExecutionProfileStatus({
  status,
}: {
  status: AdminUpstreamOAuthMigrationStatus;
}): JSX.Element | null {
  const p = status.executionProfiles;
  if (!p) {
    return (
      <p className="text-muted-foreground text-xs">
        Assistant profiles: couldn't check
      </p>
    );
  }
  if (p.targets === 0) {
    return null;
  }
  if (p.complete) {
    return (
      <p className="text-muted-foreground text-xs">
        Assistant profiles: recorded for {p.targets}{" "}
        {p.targets === 1
          ? "assistant and trigger pair"
          : "assistant and trigger pairs"}
      </p>
    );
  }
  return (
    <div className="space-y-1">
      <p className="text-muted-foreground text-xs">
        Assistant profiles:{" "}
        {p.pendingVersions > 0
          ? `${p.pendingVersions} to record`
          : "every profile recorded"}
      </p>
      {p.gaps.length > 0 ? (
        <ul className="text-muted-foreground text-xs">
          {p.gaps.map((gap) => (
            <li key={`${gap.executionContext}:${gap.reason}`}>
              {gap.executionContext} {gap.reason}: {gap.servers}{" "}
              {gap.servers === 1
                ? "profile and server pair"
                : "profile and server pairs"}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

// Whether upstream auth is enforced for the organization, and what keeps it
// from being enforced. Enforcing refuses valid credentials from another
// organization on its public servers instead of serving them anonymously.
function EnforcementStatus({
  status,
}: {
  status: AdminUpstreamOAuthMigrationStatus;
}): JSX.Element | null {
  const e = status.enforcement;
  if (!e) {
    return null;
  }
  if (e.enabled) {
    return (
      <p className="text-xs text-emerald-600 dark:text-emerald-400">
        Upstream auth enforced. Valid credentials from another organization are
        refused on its public servers.
      </p>
    );
  }
  if (e.ready) {
    return (
      <p className="text-muted-foreground text-xs">
        Ready to enforce upstream auth once its shadow evaluation agrees.
      </p>
    );
  }
  return (
    <p className="text-muted-foreground text-xs">
      Not ready to enforce: {e.blockers.join(" ")}
    </p>
  );
}

const NIL_UUID = "00000000-0000-0000-0000-000000000000";
