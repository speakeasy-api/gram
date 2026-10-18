import type { JSX } from "react";
import { useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import {
  createColumnHelper,
  useTable,
  type RowSelectionState,
} from "@tanstack/react-table";
import { toast } from "sonner";
import { Building2Icon, ChevronDownIcon, FolderIcon } from "lucide-react";
import type { AdminExternalMcpMigrationCandidate } from "@gram/admin-client/models/components/adminexternalmcpmigrationcandidate";
import type { AdminExternalMcpMigrationOutcome } from "@gram/admin-client/models/components/adminexternalmcpmigrationoutcome";
import {
  dataTableFeatures,
  DataTable,
  selectColumn,
  type DataTableFeatures,
} from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  adminListExternalMcpMigrationCandidatesQuery,
  adminMigrateExternalMcpServers,
  adminPreviewExternalMcpMigration,
} from "@/lib/gramAdminClient";

type Candidate = AdminExternalMcpMigrationCandidate;
type Outcome = AdminExternalMcpMigrationOutcome;

// The filter value that shows every organization or project.
const ALL = "all";

// The admin API migrates at most this many servers per request.
const MAX_PER_RUN = 100;

const empty: Candidate[] = [];

const column = createColumnHelper<DataTableFeatures, Candidate>();

function isReady(candidate: Candidate): boolean {
  return candidate.blockers.length === 0;
}

function StatusBadge({
  candidate,
  outcome,
}: {
  candidate: Candidate;
  outcome?: Outcome;
}): JSX.Element {
  if (outcome?.migrationStatus === "failed") {
    return (
      <div className="grid gap-1">
        <Badge variant="destructive">Failed</Badge>
        <p className="text-xs text-muted-foreground">{outcome.reason}</p>
      </div>
    );
  }
  if (outcome?.migrationStatus === "not_attempted") {
    return <Badge variant="outline">Not attempted</Badge>;
  }
  if (candidate.action === "delete") {
    return (
      <div className="grid gap-1">
        <Badge
          variant="outline"
          className="border-destructive/40 text-destructive"
        >
          Delete
        </Badge>
        <ul className="text-xs text-muted-foreground">
          {candidate.deleteReasons.map((reason) => (
            <li key={reason}>{reason}</li>
          ))}
        </ul>
      </div>
    );
  }
  if (!isReady(candidate)) {
    return (
      <div className="grid gap-1">
        <Badge variant="destructive">Blocked</Badge>
        <ul className="text-xs text-muted-foreground">
          {candidate.blockers.map((blocker) => (
            <li key={blocker}>{blocker}</li>
          ))}
        </ul>
      </div>
    );
  }
  if (candidate.warnings.length > 0) {
    return (
      <div className="grid gap-1">
        <Badge
          variant="outline"
          className="border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400"
        >
          Ready with warnings
        </Badge>
        <ul className="text-xs text-muted-foreground">
          {candidate.warnings.map((warning) => (
            <li key={warning}>{warning}</li>
          ))}
        </ul>
      </div>
    );
  }
  return <Badge variant="secondary">Ready</Badge>;
}

function OutcomeList({
  outcomes,
  names,
}: {
  outcomes: Outcome[];
  names: Map<string, string>;
}): JSX.Element {
  return (
    <ul className="grid gap-3">
      {outcomes.map((outcome) => (
        <li key={outcome.toolsetId} className="rounded-md border p-3 text-sm">
          <div className="flex items-center justify-between gap-2">
            <span className="font-medium">
              {names.get(outcome.toolsetId) ?? outcome.toolsetId}
              {outcome.mcpSlug && (
                <span className="ml-2 font-mono text-xs text-muted-foreground">
                  /mcp/{outcome.mcpSlug}
                </span>
              )}
            </span>
            <OutcomeBadge outcome={outcome} />
          </div>
          {outcome.reason && (
            <p className="mt-2 text-destructive" role="alert">
              {outcome.reason}
            </p>
          )}
          {outcome.actions.length > 0 && (
            <ul className="mt-2 list-disc pl-5 text-muted-foreground">
              {outcome.actions.map((action, i) => (
                <li key={`${outcome.toolsetId}-action-${i}`}>{action}</li>
              ))}
            </ul>
          )}
          {outcome.warnings.length > 0 && (
            <ul className="mt-2 list-disc pl-5">
              {outcome.warnings.map((warning, i) => (
                <li key={`${outcome.toolsetId}-warning-${i}`}>{warning}</li>
              ))}
            </ul>
          )}
          {outcome.remoteMcpServerId &&
            outcome.migrationStatus === "migrated" && (
              <p className="mt-2 font-mono text-xs text-muted-foreground">
                Server {outcome.mcpServerId} · remote source{" "}
                {outcome.remoteMcpServerId}
              </p>
            )}
        </li>
      ))}
    </ul>
  );
}

function OutcomeBadge({ outcome }: { outcome: Outcome }): JSX.Element {
  switch (outcome.migrationStatus) {
    case "migrated":
      return <Badge>Migrated</Badge>;
    case "deleted":
      return <Badge>Deleted</Badge>;
    case "previewed":
      return (
        <Badge variant="secondary">
          {outcome.action === "delete" ? "Will delete" : "Will migrate"}
        </Badge>
      );
    case "failed":
      return <Badge variant="destructive">Failed</Badge>;
    case "not_attempted":
      return <Badge variant="outline">Not attempted</Badge>;
  }
}

type Server = { toolsetId: string };

function countLabel(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

// runLabel names what a run does, such as "Migrate 3 servers and delete 2".
function runLabel(migrating: number, deleting: number): string {
  if (deleting === 0) return `Migrate ${countLabel(migrating, "server")}`;
  if (migrating === 0) return `Delete ${countLabel(deleting, "server")}`;
  return `Migrate ${countLabel(migrating, "server")} and delete ${countLabel(deleting, "server")}`;
}

function MigrateDialog({
  servers,
  deleting,
  names,
  onClose,
  onDone,
}: {
  servers: Server[];
  deleting: number;
  names: Map<string, string>;
  onClose: () => void;
  onDone: (outcomes: Outcome[]) => void;
}): JSX.Element {
  const cache = useQueryClient();
  // A preview runs every step and rolls back, so it is read like any query,
  // but never reused: the next dialog must see the database as it is then.
  const preview = useQuery({
    queryKey: ["externalMcpMigrationPreview", servers],
    queryFn: () => adminPreviewExternalMcpMigration({ servers }),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
  const [result, setResult] = useState<Outcome[]>();
  const [pending, setPending] = useState(false);
  const busy = useRef(false);
  const [error, setError] = useState("");

  const outcomes = result ?? preview.data?.outcomes ?? [];
  const failures = outcomes.filter((o) => o.migrationStatus === "failed");
  const done = result !== undefined;
  const canMigrate =
    !done &&
    !pending &&
    !preview.isFetching &&
    !preview.error &&
    outcomes.length > 0 &&
    failures.length === 0;

  const migrate = async () => {
    if (busy.current || !canMigrate) return;
    busy.current = true;
    setPending(true);
    setError("");
    try {
      const run = await adminMigrateExternalMcpServers({ servers });
      setResult(run.outcomes);
      onDone(run.outcomes);
      const completed = run.outcomes.filter(
        (o) =>
          o.migrationStatus === "migrated" || o.migrationStatus === "deleted",
      ).length;
      if (completed === run.outcomes.length) {
        toast.success(`Finished ${countLabel(completed, "server")}`);
      } else {
        toast.error(
          `Finished ${completed} of ${run.outcomes.length}; the run stopped at a failure`,
        );
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "The run failed");
    } finally {
      busy.current = false;
      setPending(false);
      await cache.invalidateQueries({
        queryKey: adminListExternalMcpMigrationCandidatesQuery().queryKey,
      });
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {done ? "Results" : runLabel(servers.length - deleting, deleting)}
          </DialogTitle>
          <DialogDescription>
            A migrated server switches in place to a remote MCP server on its
            own sign-in provider and keeps its ID and address. A deleted one
            loses its toolset and any server, addresses and plugin entries it
            still owns; a server already migrated stays intact. Servers run one
            at a time and the run stops at the first failure.
          </DialogDescription>
        </DialogHeader>
        {preview.isFetching && (
          <p role="status">Checking each server without changing anything…</p>
        )}
        {pending && <p role="status">Running…</p>}
        {preview.error && (
          <p role="alert" className="text-destructive">
            {preview.error.message}
          </p>
        )}
        {error && (
          <p role="alert" className="text-destructive">
            {error}
          </p>
        )}
        {failures.length > 0 && !done && (
          <p role="alert" className="text-sm text-destructive">
            {failures.length} server{failures.length === 1 ? "" : "s"} would
            fail. Deselect {failures.length === 1 ? "it" : "them"} to continue.
          </p>
        )}
        {outcomes.length > 0 && (
          <OutcomeList outcomes={outcomes} names={names} />
        )}
        <DialogFooter>
          {done ? (
            <Button onClick={onClose}>Close</Button>
          ) : (
            <>
              <Button variant="outline" disabled={pending} onClick={onClose}>
                Cancel
              </Button>
              {preview.error && (
                <Button
                  variant="outline"
                  onClick={() => void preview.refetch()}
                >
                  Retry preview
                </Button>
              )}
              <Button
                variant="destructive"
                disabled={!canMigrate}
                onClick={() => void migrate()}
              >
                {runLabel(servers.length - deleting, deleting)}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ExternalMcpMigration(): JSX.Element {
  const query = useQuery(adminListExternalMcpMigrationCandidatesQuery());
  const all = query.data?.candidates ?? empty;
  const windowDays = query.data?.toolCallWindowDays ?? 30;
  const navigate = useNavigate({ from: "/external-mcp-migration" });
  const { org, project } = useSearch({ from: "/external-mcp-migration" });

  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});
  const [outcomes, setOutcomes] = useState<Record<string, Outcome>>({});
  const [lastRun, setLastRun] = useState<Outcome[]>([]);
  const [dialogServers, setDialogServers] = useState<Server[]>();

  // Names outlive the rows: a migrated server drops out of the list, but its
  // result still needs a label.
  const [knownNames, setKnownNames] = useState(new Map<string, string>());
  const names = useMemo(() => {
    const merged = new Map(knownNames);
    for (const c of all) {
      merged.set(c.toolsetId, `${c.organizationName} / ${c.serverName}`);
    }
    return merged;
  }, [all, knownNames]);

  const organizations = useMemo(() => {
    const byId = new Map<string, string>();
    for (const c of all) byId.set(c.organizationId, c.organizationName);
    return [...byId.entries()].sort((a, b) => a[1].localeCompare(b[1]));
  }, [all]);

  // An id that names nothing in the list, such as one whose servers have all
  // migrated, filters nothing.
  const orgFilter = organizations.some(([id]) => id === org) ? org : undefined;
  const projects = useMemo(() => {
    const byId = new Map<string, string>();
    for (const c of all) {
      if (orgFilter && c.organizationId !== orgFilter) continue;
      byId.set(
        c.projectId,
        orgFilter ? c.projectName : `${c.organizationName} / ${c.projectName}`,
      );
    }
    return [...byId.entries()].sort((a, b) => a[1].localeCompare(b[1]));
  }, [all, orgFilter]);
  const projectFilter = projects.some(([id]) => id === project)
    ? project
    : undefined;

  const candidates = useMemo(
    () =>
      all.filter(
        (c) =>
          (!orgFilter || c.organizationId === orgFilter) &&
          (!projectFilter || c.projectId === projectFilter),
      ),
    [all, orgFilter, projectFilter],
  );

  const filterBy = (next: { org?: string; project?: string }) => {
    // A selection made under one filter is not carried into another, where
    // some of it would be hidden.
    setRowSelection({});
    void navigate({ search: next, replace: true });
  };

  const select = (pick: (c: Candidate) => boolean) => {
    const next: RowSelectionState = {};
    let count = 0;
    for (const c of candidates) {
      if (count === MAX_PER_RUN) break;
      if (isReady(c) && pick(c)) {
        next[c.toolsetId] = true;
        count++;
      }
    }
    setRowSelection(next);
  };

  const columns = column.columns([
    selectColumn<Candidate>({
      allLabel: "Select every ready server",
      rowLabel: (c) => `Select ${c.serverName} in ${c.organizationName}`,
    }),
    column.display({
      id: "organization",
      header: "Organization",
      cell: ({ row }) => (
        <div>
          {row.original.organizationName}
          <p className="text-xs text-muted-foreground">
            {row.original.organizationSlug}
          </p>
        </div>
      ),
    }),
    column.display({
      id: "project",
      header: "Project",
      cell: ({ row }) => row.original.projectName,
    }),
    column.display({
      id: "server",
      header: "Server",
      cell: ({ row }) => (
        <div>
          {row.original.serverName}
          {row.original.mcpSlug && (
            <p className="font-mono text-xs text-muted-foreground">
              /mcp/{row.original.mcpSlug}
            </p>
          )}
        </div>
      ),
    }),
    column.display({
      id: "upstream",
      header: "Upstream",
      meta: { cellClassName: "max-w-80" },
      cell: ({ row }) => {
        const { action, remoteUrl, replacementUrl } = row.original;
        if (!remoteUrl) {
          return <span>{action === "delete" ? "—" : "Several URLs"}</span>;
        }
        return (
          <div className="break-all">
            {remoteUrl}
            {replacementUrl && replacementUrl !== remoteUrl && (
              <p className="text-xs text-muted-foreground">
                Becomes {replacementUrl}
              </p>
            )}
          </div>
        );
      },
    }),
    column.display({
      id: "signIn",
      header: "Sign-in provider",
      cell: ({ row }) => row.original.signInProvider ?? "None",
    }),
    column.display({
      id: "toolCalls",
      header: `Calls (${windowDays}d)`,
      meta: { cellClassName: "text-right tabular-nums" },
      cell: ({ row }) =>
        row.original.toolCalls === undefined
          ? "—"
          : row.original.toolCalls.toLocaleString(),
    }),
    column.display({
      id: "status",
      header: "Status",
      meta: { cellClassName: "min-w-64 whitespace-normal" },
      cell: ({ row }) => (
        <StatusBadge
          candidate={row.original}
          outcome={outcomes[row.original.toolsetId]}
        />
      ),
    }),
  ]);

  const table = useTable({
    features: dataTableFeatures,
    columns,
    data: candidates,
    getRowId: (c) => c.toolsetId,
    enableRowSelection: (row) => isReady(row.original),
    state: { rowSelection },
    onRowSelectionChange: setRowSelection,
  });

  const selected = table.getSelectedRowModel().rows.map((row) => row.original);
  const selectedDeletes = selected.filter((c) => c.action === "delete").length;
  const ready = candidates.filter(isReady).length;
  const toMigrate = candidates.filter(
    (c) => isReady(c) && c.action === "migrate",
  ).length;
  const toDelete = candidates.filter((c) => c.action === "delete").length;

  const openMigrate = () => {
    setDialogServers(selected.map((c) => ({ toolsetId: c.toolsetId })));
  };

  const recordRun = (run: Outcome[]) => {
    setKnownNames(new Map(names));
    setLastRun(run);
    setOutcomes(Object.fromEntries(run.map((o) => [o.toolsetId, o])));
    setRowSelection({});
  };

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-semibold">External MCP migration</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Toolsets still backed by an external MCP attachment. Migrating one
          switches its server in place to a remote MCP server on its own sign-in
          provider, so it keeps its ID, address, plugins and access grants, and
          disables the old toolset. No sign-in provider or client is created. A
          toolset left with nothing to migrate, including one already migrated,
          is deleted instead.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={orgFilter ?? ALL}
          onValueChange={(value) =>
            filterBy({ org: value === ALL ? undefined : value })
          }
        >
          <SelectTrigger
            size="sm"
            aria-label="Organization"
            className="bg-card"
          >
            <Building2Icon />
            <SelectValue />
          </SelectTrigger>
          <SelectContent position="popper" align="start" className="min-w-64">
            <SelectItem value={ALL}>All organizations</SelectItem>
            {organizations.map(([id, name]) => (
              <SelectItem key={id} value={id}>
                {name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={projectFilter ?? ALL}
          onValueChange={(value) =>
            filterBy({
              org: orgFilter,
              project: value === ALL ? undefined : value,
            })
          }
        >
          <SelectTrigger size="sm" aria-label="Project" className="bg-card">
            <FolderIcon />
            <SelectValue />
          </SelectTrigger>
          <SelectContent position="popper" align="start" className="min-w-64">
            <SelectItem value={ALL}>All projects</SelectItem>
            {projects.map(([id, name]) => (
              <SelectItem key={id} value={id}>
                {name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline">
              Select <ChevronDownIcon className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            <DropdownMenuItem onSelect={() => select(() => true)}>
              All ready ({ready})
            </DropdownMenuItem>
            <DropdownMenuItem
              onSelect={() => select((c) => c.action === "migrate")}
            >
              Ready to migrate ({toMigrate})
            </DropdownMenuItem>
            <DropdownMenuItem
              onSelect={() => select((c) => c.action === "delete")}
            >
              To delete ({toDelete})
            </DropdownMenuItem>
            <DropdownMenuItem
              onSelect={() =>
                select((c) => c.action === "migrate" && c.toolCalls === 0)
              }
            >
              Ready with no recent calls
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => setRowSelection({})}>
              None
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <span className="text-sm text-muted-foreground" aria-live="polite">
          {selected.length} of {candidates.length} selected · {ready} ready ·{" "}
          <span
            className={
              selected.length > MAX_PER_RUN ? "text-destructive" : undefined
            }
          >
            up to {MAX_PER_RUN} servers per run
          </span>
        </span>
        <Button
          className="ml-auto"
          disabled={selected.length === 0 || selected.length > MAX_PER_RUN}
          onClick={openMigrate}
        >
          {selected.length > 0
            ? runLabel(selected.length - selectedDeletes, selectedDeletes)
            : "Migrate"}
        </Button>
      </div>

      {query.isPending && <p role="status">Loading servers…</p>}
      {query.error && (
        <p role="alert">
          {query.error.message}
          <Button variant="ghost" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </p>
      )}

      {lastRun.length > 0 && (
        <section className="grid gap-2">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold">Last run</h2>
            <Button variant="ghost" size="sm" onClick={() => setLastRun([])}>
              Dismiss
            </Button>
          </div>
          <OutcomeList outcomes={lastRun} names={names} />
        </section>
      )}

      <DataTable>
        <DataTable.Header table={table} />
        <DataTable.Body>
          {table.getRowModel().rows.map((row) => (
            <DataTable.Row key={row.id} row={row} />
          ))}
        </DataTable.Body>
      </DataTable>
      {!query.isPending && !query.error && all.length === 0 && (
        <p>No servers are backed by an external MCP attachment.</p>
      )}
      {all.length > 0 && candidates.length === 0 && (
        <p>No servers match these filters.</p>
      )}

      {dialogServers && (
        <MigrateDialog
          servers={dialogServers}
          deleting={
            dialogServers.filter(
              (s) =>
                all.find((c) => c.toolsetId === s.toolsetId)?.action ===
                "delete",
            ).length
          }
          names={names}
          onClose={() => setDialogServers(undefined)}
          onDone={recordRun}
        />
      )}
    </div>
  );
}
