import { useState, type JSX } from "react";
import {
  useQueries,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { Loader2Icon } from "lucide-react";
import { toast } from "sonner";

import type { AdminUpstreamOAuthMigrationOrganization } from "@gram/admin-client/models/components/adminupstreamoauthmigrationorganization";
import type { AdminUpstreamOAuthMigrationAuthClients } from "@gram/admin-client/models/components/adminupstreamoauthmigrationauthclients";
import type { AdminUpstreamOAuthMigrationStatus } from "@gram/admin-client/models/components/adminupstreamoauthmigrationstatus";
import type { AdminUpstreamOAuthMigrationUnresolvedAssociation } from "@gram/admin-client/models/components/adminupstreamoauthmigrationunresolvedassociation";

import { useConfirmDialog } from "@/components/ConfirmDialog";
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
  enableUpstreamAuthEnforcement,
  upstreamOAuthMigrationOrganizationsQuery,
  upstreamOAuthMigrationStatusQuery,
} from "@/lib/gramAdminClient";

// Limit concurrent typed readiness checks.
const STATUS_CONCURRENCY = 4;

export function UpstreamOAuthMigrationPage(): JSX.Element {
  const client = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const organizations = useQuery(upstreamOAuthMigrationOrganizationsQuery());
  const [filter, setFilter] = useState("");
  // What each busy organization is doing, by id.
  const [running, setRunning] = useState<Map<string, string>>(new Map());

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
  async function enforce(org: AdminUpstreamOAuthMigrationOrganization) {
    const ok = await confirm({
      title: `Enforce upstream auth for ${org.name}?`,
      description:
        "Its servers and assistant turns are served by the auth client each credential chose, and using it requires permission. Confirm that its mandatory authorization shadow shows no unexplained new denial, and that it was told valid credentials from another organization are refused on its public servers instead of served anonymously. Credentials with an ambiguous choice are asked to reconnect. Staff cannot turn enforcement off.",
      confirmLabel: "Enforce",
    });
    if (!ok) return;
    setRunning((current) =>
      new Map(current).set(org.id, "Updating enforcement"),
    );
    try {
      const status = await enableUpstreamAuthEnforcement(org.id);
      client.setQueryData(
        upstreamOAuthMigrationStatusQuery(org.id).queryKey,
        status,
      );
      toast.success(`Enforcing upstream auth for ${org.name}`);
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

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Upstream OAuth migration</h1>
          <p className="text-muted-foreground max-w-3xl">
            Shows upstream OAuth coverage, typed credential choices, and
            enforcement readiness. The one-time migration no longer runs.
          </p>
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
                        {status?.data?.enforcement?.ready &&
                        !status.data.enforcement.enabled ? (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => void enforce(org)}
                            disabled={busy}
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
  return <div className="space-y-1">{authClients}</div>;
}

// Report typed auth client readiness and the specific choices that need repair.
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
  const unexercisedVersions = p.unexercisedVersions ?? 0;
  const unexercised =
    unexercisedVersions > 0 ? (
      <p className="text-muted-foreground text-xs">
        {unexercisedVersions}{" "}
        {unexercisedVersions === 1 ? "profile" : "profiles"} with no version
        recorded yet, recorded on first use
      </p>
    ) : null;
  if (p.complete) {
    return (
      <div className="space-y-1">
        <p className="text-muted-foreground text-xs">
          Assistant profiles: recorded for {p.targets}{" "}
          {p.targets === 1
            ? "assistant and trigger pair"
            : "assistant and trigger pairs"}
        </p>
        {unexercised}
      </div>
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
      {unexercised}
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
