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
  const [running, setRunning] = useState<Set<string>>(new Set());
  const [bulk, setBulk] = useState<{ done: number; total: number } | null>(
    null,
  );

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
    setRunning((current) => new Set(current).add(org.id));
    try {
      const run = await runUpstreamOAuthMigration(org.id);
      client.setQueryData(
        upstreamOAuthMigrationStatusQuery(org.id).queryKey,
        run.status,
      );
      return run;
    } finally {
      setRunning((current) => {
        const next = new Set(current);
        next.delete(org.id);
        return next;
      });
    }
  }

  async function migrateOne(org: AdminUpstreamOAuthMigrationOrganization) {
    const ok = await confirm({
      title: `Migrate ${org.name}?`,
      description:
        "Records the upstream OAuth client associations its configuration proves, then each live credential's client choice, then a typed auth client for each upstream identity its servers use. Safe to run again.",
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

  async function migrateAllPending() {
    const pending = all.filter((org) =>
      needsMigrationRun(statusByID.get(org.id)?.data),
    );
    if (pending.length === 0) return;
    const ok = await confirm({
      title: `Migrate ${pending.length} ${pending.length === 1 ? "organization" : "organizations"}?`,
      description:
        "Runs the migration for every organization not yet migrated or with auth clients left to record, one at a time. Keep this tab open until it finishes; an interrupted run is safe to start again.",
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
                const busy = running.has(org.id);
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
                      <MigrationStatus status={status} busy={busy} />
                    </TableCell>
                    <TableCell>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => void migrateOne(org)}
                        disabled={busy || bulk !== null}
                      >
                        {status?.data?.migrated ? "Run again" : "Migrate"}
                      </Button>
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
  busy,
}: {
  status: UseQueryResult<AdminUpstreamOAuthMigrationStatus> | undefined;
  busy: boolean;
}): JSX.Element {
  if (busy) {
    return (
      <span className="text-muted-foreground inline-flex items-center gap-1.5">
        <Loader2Icon aria-hidden="true" className="size-4 animate-spin" />
        Migrating
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
  const authClients = <AuthClientStatus status={s} />;
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
    a.pendingMarkedServers > 0
      ? `${a.pendingMarkedServers} ${a.pendingMarkedServers === 1 ? "server" : "servers"} to mark as needing upstream auth`
      : null,
    a.unresolved > 0 ? `${a.unresolved} need attention` : null,
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

const NIL_UUID = "00000000-0000-0000-0000-000000000000";
