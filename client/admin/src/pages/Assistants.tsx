import { useRef, useState, type JSX } from "react";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Loader2Icon } from "lucide-react";
import { toast } from "sonner";

import { useOnUnmount } from "@/hooks/useOnUnmount";
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
import {
  assistantMigrationOrganizationsQuery,
  organizationAssistantsQuery,
} from "@/lib/adminQueries";
import {
  MIGRATION_ACTIONS,
  migrationCandidates,
  migrationCounts,
  type AssistantMigrationAction,
} from "@/lib/assistantMigrations";
import {
  errorMessage,
  migrateOrganizationAssistantIdentities,
  migrateOrganizationAssistantsToKit,
  repairAssistantAgentNames,
  type AdminAssistantMigrationOrganization,
  type AdminOrganizationAssistant,
} from "@/lib/gramAdminApi";

const EMPTY_ORGANIZATIONS: AdminAssistantMigrationOrganization[] = [];
const ACTIONS: AssistantMigrationAction[] = ["names", "identity", "kit"];
const STATUS_CONCURRENCY = 4;
type RunResult = {
  message: string;
  incomplete: boolean;
  results: {
    assistant_id: string;
    project_id: string;
    name: string;
    outcome: string;
  }[];
};

async function runMigration(
  action: AssistantMigrationAction,
  organizationID: string,
  assistants: AdminOrganizationAssistant[],
): Promise<RunResult> {
  if (action === "names") {
    const run = await repairAssistantAgentNames(organizationID);
    return {
      message: `Repaired ${run.repaired}, conflicts ${run.conflicts}, failed ${run.failed}`,
      incomplete: run.conflicts + run.failed > 0,
      results: run.results,
    };
  }
  if (action === "identity") {
    const run = await migrateOrganizationAssistantIdentities(organizationID);
    return {
      message: `Migrated ${run.migrated}, already migrated ${run.already_migrated}, needs owner ${run.needs_owner}, failed ${run.failed}`,
      incomplete: run.needs_owner + run.failed > 0,
      results: run.results,
    };
  }
  const run = await migrateOrganizationAssistantsToKit(
    organizationID,
    migrationCandidates(assistants, "kit").map((a) => a.id),
  );
  return {
    message: `Moved to Kit ${run.switched}, already on Kit ${run.already_kit}, needs identity ${run.needs_identity}, ineligible ${run.ineligible}, busy ${run.busy}, failed ${run.failed}`,
    incomplete: run.needs_identity + run.ineligible + run.busy + run.failed > 0,
    results: run.results,
  };
}

export function AssistantsPage(): JSX.Element {
  const client = useQueryClient();
  const organizations = useQuery(assistantMigrationOrganizationsQuery());
  const all = organizations.data?.organizations ?? EMPTY_ORGANIZATIONS;
  const { organization } = useSearch({ from: "/assistants" });
  const navigate = useNavigate({ from: "/assistants" });
  const filter = organization ?? "";
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const mounted = useRef(true);
  const stop = useRef(false);
  const [stopping, setStopping] = useState(false);
  const [progress, setProgress] = useState<{
    action: AssistantMigrationAction;
    done: number;
    total: number;
    organizationID: string;
  } | null>(null);
  const [runs, setRuns] = useState<Map<string, RunResult>>(new Map());
  const [confirm, dialog] = useConfirmDialog();

  useOnUnmount(() => {
    mounted.current = false;
    stop.current = true;
  });

  // Disabled query observers subscribe to the cache; this worker pool alone
  // starts status reads, including refreshes, so cached rows cannot fan out.
  const statuses = useQueries({
    queries: all.map((org) => ({
      ...organizationAssistantsQuery(org.id),
      enabled: false,
    })),
  });
  const discovery = useQuery({
    queryKey: ["admin", "assistant-status-discovery", all.map((org) => org.id)],
    enabled: organizations.isSuccess,
    retry: false,
    queryFn: async ({ signal }) => {
      let next = 0;
      async function worker() {
        while (!signal.aborted && next < all.length) {
          const org = all[next++];
          if (!org) break;
          try {
            await client.fetchQuery({
              ...organizationAssistantsQuery(org.id),
              staleTime: 0,
              retry: false,
            });
          } catch {
            // Each query observer shows its organization's read error.
          }
        }
      }
      await Promise.all(Array.from({ length: STATUS_CONCURRENCY }, worker));
      return true;
    },
    refetchOnWindowFocus: false,
  });
  const discovering = organizations.isSuccess && discovery.isFetching;

  const byID = new Map(all.map((org, index) => [org.id, statuses[index]]));
  const term = filter.trim().toLowerCase();
  const visible = all.filter(
    (org) =>
      !term ||
      org.id.toLowerCase() === term ||
      org.name.toLowerCase().includes(term) ||
      org.slug.toLowerCase().includes(term),
  );
  const allLoaded =
    !organizations.isPending &&
    !organizations.isError &&
    !organizations.isFetching &&
    !discovering &&
    statuses.every((status) => status.isSuccess && !status.isFetching);
  const totals = statuses.reduce(
    (sum, status) => {
      const counts = migrationCounts(status.data?.assistants ?? []);
      return {
        names: sum.names + counts.names,
        identity: sum.identity + counts.identity,
        kit: sum.kit + counts.kit,
      };
    },
    { names: 0, identity: 0, kit: 0 },
  );

  async function migrate(
    action: AssistantMigrationAction,
    target?: AdminAssistantMigrationOrganization,
  ) {
    if (busyRef.current) return;
    if (!target && !allLoaded) return;
    const targets = (target ? [target] : all).flatMap((org) => {
      const status = byID.get(org.id);
      if (!status?.isSuccess || status.isFetching) return [];
      const assistants = status.data.assistants;
      const count = migrationCandidates(assistants, action).length;
      return count > 0 ? [{ org, assistants, count }] : [];
    });
    if (targets.length === 0) return;
    busyRef.current = true;
    setBusy(true);
    try {
      const count = targets.reduce((sum, item) => sum + item.count, 0);
      const confirmed = await confirm({
        title: `${MIGRATION_ACTIONS[action].label} for ${target ? target.name : `${targets.length} organizations`}?`,
        description: `${MIGRATION_ACTIONS[action].description} ${count} assistant(s) selected. ${target ? "" : "This covers all eligible organizations, regardless of the search filter. "}Organizations run one at a time. Keep this tab open; interrupted work is safe to retry.`,
        details: (
          <ul className="max-h-40 overflow-auto text-sm">
            {targets.map(({ org, count: selected }) => (
              <li key={org.id}>
                {org.name} ({org.slug}): {selected}
              </li>
            ))}
          </ul>
        ),
        confirmLabel: MIGRATION_ACTIONS[action].label,
      });
      if (!confirmed || !mounted.current) return;
      stop.current = false;
      setStopping(false);
      let incomplete = 0;
      for (const [index, { org, assistants }] of targets.entries()) {
        if (stop.current || !mounted.current) break;
        setProgress({
          action,
          done: index,
          total: targets.length,
          organizationID: org.id,
        });
        let result: RunResult;
        try {
          result = await runMigration(action, org.id, assistants);
          try {
            await client.fetchQuery({
              ...organizationAssistantsQuery(org.id),
              staleTime: 0,
              retry: false,
            });
          } catch {
            result = {
              ...result,
              incomplete: true,
              message: `${result.message}. Status refresh failed; refresh before continuing.`,
            };
          }
        } catch (error) {
          result = {
            message: errorMessage(error),
            incomplete: true,
            results: [],
          };
        }
        if (result.incomplete) incomplete++;
        if (mounted.current) {
          setRuns((current) => new Map(current).set(org.id, result));
          setProgress({
            action,
            done: index + 1,
            total: targets.length,
            organizationID: "",
          });
        }
      }
      if (mounted.current) {
        if (stop.current)
          toast.info(
            "Stopped after the current organization. Completed changes are kept.",
          );
        else if (incomplete > 0)
          toast.warning(
            `${incomplete} organization(s) need attention. Review the results and retry incomplete work.`,
          );
        else toast.success(`${MIGRATION_ACTIONS[action].label} completed`);
      }
    } finally {
      busyRef.current = false;
      if (mounted.current) {
        setBusy(false);
        setProgress(null);
        setStopping(false);
      }
    }
  }

  async function refreshStatus() {
    if (busyRef.current) return;
    await client.invalidateQueries({
      queryKey: assistantMigrationOrganizationsQuery().queryKey,
    });
    await client.invalidateQueries({
      queryKey: ["admin", "assistant-status-discovery"],
    });
  }

  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold tracking-tight">Assistants</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Manage assistant identities, readable agent names, and Kit adoption
          across every organization.
        </p>
      </header>
      <div className="grid gap-3 sm:grid-cols-4">
        <Summary
          label="Assistants"
          value={all.reduce((sum, org) => sum + org.assistants, 0)}
        />
        <Summary label="Names to repair" value={totals.names} />
        <Summary label="Need agent identity" value={totals.identity} />
        <Summary label="Ready to try Kit" value={totals.kit} />
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {ACTIONS.map((action) => (
          <Button
            key={action}
            variant="outline"
            disabled={busy || !allLoaded || totals[action] === 0}
            onClick={() => void migrate(action)}
          >
            {MIGRATION_ACTIONS[action].label} · all eligible
          </Button>
        ))}
        <Button
          variant="ghost"
          disabled={busy || discovering || organizations.isFetching}
          onClick={() => void refreshStatus()}
        >
          Refresh status
        </Button>
      </div>
      <p className="text-muted-foreground -mt-3 text-sm">
        Bulk actions include all eligible organizations, even when the table is
        filtered. Kit eligibility is rechecked during migration; busy assistants
        or projects without a shared user-session issuer are skipped.
      </p>
      {(discovering || organizations.isPending) && (
        <p role="status" className="flex items-center gap-2 text-sm">
          <Loader2Icon className="size-4 animate-spin" />
          Loading organization status…{" "}
          {
            statuses.filter((status) => status.isSuccess || status.isError)
              .length
          }
          /{all.length}
        </p>
      )}
      {!discovering && statuses.some((status) => status.isError) && (
        <p role="alert" className="text-destructive text-sm">
          Some statuses could not be loaded. Refresh them before running a bulk
          action.
        </p>
      )}
      {progress && (
        <div
          role="status"
          className="flex items-center justify-between rounded-lg border p-3 text-sm"
        >
          <span>
            {MIGRATION_ACTIONS[progress.action].label}: {progress.done}/
            {progress.total} organizations complete
          </span>
          <Button
            size="sm"
            variant="outline"
            disabled={stopping}
            onClick={() => {
              stop.current = true;
              setStopping(true);
            }}
          >
            {stopping ? "Stopping…" : "Stop after current"}
          </Button>
        </div>
      )}
      <Input
        aria-label="Filter organizations"
        placeholder="Filter by organization name, slug, or ID…"
        value={filter}
        onChange={(event) =>
          void navigate({
            search: { organization: event.target.value || undefined },
            replace: true,
          })
        }
        className="max-w-md"
      />
      {organizations.isError ? (
        <p role="alert">
          Unable to load organizations: {errorMessage(organizations.error)}
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Organization</TableHead>
              <TableHead>Assistants</TableHead>
              <TableHead>Agent names</TableHead>
              <TableHead>Agent identity</TableHead>
              <TableHead>Kit runtime</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {visible.map((org) => {
              const status = byID.get(org.id);
              const assistants = status?.data?.assistants ?? [];
              const counts = migrationCounts(assistants);
              const result = runs.get(org.id);
              const loaded = status?.isSuccess && !status.isFetching;
              return (
                <TableRow key={org.id}>
                  <TableCell className="max-w-sm whitespace-normal">
                    <p className="font-medium">{org.name}</p>
                    <p className="text-muted-foreground text-xs">{org.slug}</p>
                    {status?.isError && (
                      <p role="alert" className="text-destructive mt-2 text-xs">
                        Unable to load status: {errorMessage(status.error)}
                      </p>
                    )}
                    {result && (
                      <details
                        className="mt-2 text-xs"
                        open={result.incomplete}
                      >
                        <summary className="cursor-pointer">
                          Last run: {result.message}
                        </summary>
                        <ul className="mt-1 space-y-1">
                          {result.results.map((item) => (
                            <li key={item.assistant_id}>
                              {item.name}: {item.outcome.replaceAll("_", " ")}
                            </li>
                          ))}
                        </ul>
                      </details>
                    )}
                    {loaded && (
                      <details className="mt-2 text-xs">
                        <summary className="cursor-pointer">
                          View assistants
                        </summary>
                        <ul className="mt-2 space-y-2">
                          {assistants.map((assistant) => (
                            <li key={assistant.id}>
                              <span className="font-medium">
                                {assistant.name}
                              </span>{" "}
                              · {assistant.project_name}
                              <br />
                              <span className="text-muted-foreground">
                                {assistant.identity_state === "NEVER_CONFIGURED"
                                  ? "No identity"
                                  : assistant.identity_state === "ACTIVE"
                                    ? "Active identity"
                                    : "Identity unavailable"}{" "}
                                ·{" "}
                                {assistant.runtime_version === 3
                                  ? "Kit"
                                  : "Default runtime"}
                                {!assistant.creator_active &&
                                assistant.identity_state === "NEVER_CONFIGURED"
                                  ? " · Needs active owner"
                                  : ""}
                                {assistant.agent_name
                                  ? ` · Agent: ${assistant.agent_name}`
                                  : ""}
                                {assistant.agent_name_repair_state ===
                                "conflict"
                                  ? " · Name conflict"
                                  : ""}
                              </span>
                            </li>
                          ))}
                        </ul>
                      </details>
                    )}
                  </TableCell>
                  <TableCell>{org.assistants}</TableCell>
                  <TableCell>
                    {loaded ? (
                      <>
                        <Badge variant="outline">{counts.names} pending</Badge>
                        {counts.conflicts > 0 && (
                          <p className="text-destructive mt-1 text-xs">
                            {counts.conflicts} conflicts
                          </p>
                        )}
                      </>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell>
                    {loaded ? (
                      <>
                        <Badge variant="outline">
                          {counts.identity} pending
                        </Badge>
                        {counts.needsOwner > 0 && (
                          <p className="text-muted-foreground mt-1 text-xs">
                            {counts.needsOwner} need owner
                          </p>
                        )}
                        {counts.unavailable > 0 && (
                          <p className="text-destructive mt-1 text-xs">
                            {counts.unavailable} unavailable
                          </p>
                        )}
                      </>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell>
                    {loaded ? (
                      <>
                        <Badge variant="outline">
                          {counts.kit} ready to try
                        </Badge>
                        <p className="text-muted-foreground mt-1 text-xs">
                          {
                            assistants.filter(
                              (assistant) => assistant.runtime_version === 3,
                            ).length
                          }{" "}
                          on Kit
                        </p>
                      </>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap justify-end gap-2">
                      {ACTIONS.map((action) => (
                        <Button
                          key={action}
                          size="sm"
                          variant="outline"
                          disabled={
                            busy ||
                            discovering ||
                            organizations.isFetching ||
                            !loaded ||
                            counts[action] === 0
                          }
                          onClick={() => void migrate(action, org)}
                        >
                          {progress?.organizationID === org.id &&
                          progress.action === action ? (
                            <Loader2Icon className="size-4 animate-spin" />
                          ) : null}
                          {MIGRATION_ACTIONS[action].label}
                        </Button>
                      ))}
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
            {!organizations.isPending && visible.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={6}
                  className="text-muted-foreground py-8 text-center"
                >
                  {all.length === 0
                    ? "No organizations with live assistants"
                    : "No organizations match this filter"}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      )}
      {dialog}
    </div>
  );
}

function Summary({
  label,
  value,
}: {
  label: string;
  value: number;
}): JSX.Element {
  return (
    <div className="rounded-lg border p-4">
      <p className="text-muted-foreground text-sm">{label}</p>
      <p className="mt-1 text-2xl font-semibold tabular-nums">{value}</p>
    </div>
  );
}
