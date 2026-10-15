import type { JSX } from "react";
import { useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createColumnHelper,
  useTable,
  type RowSelectionState,
} from "@tanstack/react-table";
import { toast } from "sonner";
import { ChevronDownIcon } from "lucide-react";
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
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
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

// The "Migrate into" value that creates a new remote server.
const NEW_SERVER = "new";

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
          {outcome.mcpServerId && outcome.migrationStatus === "migrated" && (
            <p className="mt-2 font-mono text-xs text-muted-foreground">
              New server {outcome.mcpServerId}
              {outcome.endpointIds.length > 0 &&
                ` · endpoint ${outcome.endpointIds.join(", ")}`}
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
    case "previewed":
      return <Badge variant="secondary">Ready</Badge>;
    case "failed":
      return <Badge variant="destructive">Failed</Badge>;
    case "not_attempted":
      return <Badge variant="outline">Not attempted</Badge>;
  }
}

type Server = { toolsetId: string; targetMcpServerId?: string };

function MigrateDialog({
  servers,
  names,
  onClose,
  onDone,
}: {
  servers: Server[];
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
      const migrated = run.outcomes.filter(
        (o) => o.migrationStatus === "migrated",
      ).length;
      if (migrated === run.outcomes.length) {
        toast.success(
          `Migrated ${migrated} server${migrated === 1 ? "" : "s"}`,
        );
      } else {
        toast.error(
          `Migrated ${migrated} of ${run.outcomes.length}; the run stopped at a failure`,
        );
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Migration failed");
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
            {done
              ? "Migration results"
              : `Migrate ${servers.length} server${servers.length === 1 ? "" : "s"}`}
          </DialogTitle>
          <DialogDescription>
            Each server becomes a remote MCP server on its own sign-in provider
            and keeps its address. Servers run one at a time and the run stops
            at the first failure.
          </DialogDescription>
        </DialogHeader>
        {preview.isFetching && (
          <p role="status">Checking each server without changing anything…</p>
        )}
        {pending && <p role="status">Migrating…</p>}
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
                Migrate {servers.length} server
                {servers.length === 1 ? "" : "s"}
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
  const candidates = query.data?.candidates ?? empty;
  const windowDays = query.data?.toolCallWindowDays ?? 30;

  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});
  const [targets, setTargets] = useState<Record<string, string>>({});
  const [outcomes, setOutcomes] = useState<Record<string, Outcome>>({});
  const [lastRun, setLastRun] = useState<Outcome[]>([]);
  const [dialogServers, setDialogServers] =
    useState<{ toolsetId: string; targetMcpServerId?: string }[]>();

  // Names outlive the rows: a migrated server drops out of the list, but its
  // result still needs a label.
  const [knownNames, setKnownNames] = useState(new Map<string, string>());
  const names = useMemo(() => {
    const merged = new Map(knownNames);
    for (const c of candidates) {
      merged.set(c.toolsetId, `${c.organizationName} / ${c.serverName}`);
    }
    return merged;
  }, [candidates, knownNames]);

  const organizations = useMemo(() => {
    const byId = new Map<string, string>();
    for (const c of candidates) byId.set(c.organizationId, c.organizationName);
    return [...byId.entries()].sort((a, b) => a[1].localeCompare(b[1]));
  }, [candidates]);

  const select = (pick: (c: Candidate) => boolean) => {
    const next: RowSelectionState = {};
    for (const c of candidates) {
      if (isReady(c) && pick(c)) next[c.toolsetId] = true;
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
        const { remoteUrl, cleanedUrl } = row.original;
        if (!remoteUrl) return <span>Several URLs</span>;
        return (
          <div className="break-all">
            {remoteUrl}
            {cleanedUrl && cleanedUrl !== remoteUrl && (
              <p className="text-xs text-muted-foreground">
                Becomes {cleanedUrl}
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
      id: "target",
      header: "Migrate into",
      cell: ({ row }) => {
        const c = row.original;
        if (c.targets.length === 0 || !isReady(c)) {
          return <span className="text-muted-foreground">New server</span>;
        }
        return (
          <Select
            value={targets[c.toolsetId] ?? NEW_SERVER}
            onValueChange={(value) =>
              setTargets((current) => ({ ...current, [c.toolsetId]: value }))
            }
          >
            <SelectTrigger
              aria-label={`Migrate ${c.serverName} into`}
              size="sm"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={NEW_SERVER}>New server</SelectItem>
              {c.targets.map((target) => (
                <SelectItem key={target.id} value={target.id}>
                  {target.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        );
      },
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
  const ready = candidates.filter(isReady).length;

  const openMigrate = () => {
    setDialogServers(
      selected.map((c) => {
        const target = targets[c.toolsetId];
        return target && target !== NEW_SERVER
          ? { toolsetId: c.toolsetId, targetMcpServerId: target }
          : { toolsetId: c.toolsetId };
      }),
    );
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
          Servers still backed by an external MCP attachment. Migrating one
          replaces it with a remote MCP server on its own sign-in provider,
          keeps its address, moves what references it, and deletes the old
          toolset. No sign-in provider or client is created.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
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
              onSelect={() => select((c) => (c.toolCalls ?? 0) === 0)}
            >
              Ready with no recent calls
            </DropdownMenuItem>
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>By organization</DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="max-h-80 overflow-y-auto">
                {organizations.map(([id, name]) => (
                  <DropdownMenuItem
                    key={id}
                    onSelect={() => select((c) => c.organizationId === id)}
                  >
                    {name}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => setRowSelection({})}>
              None
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <span className="text-sm text-muted-foreground" aria-live="polite">
          {selected.length} of {candidates.length} selected · {ready} ready
        </span>
        <Button
          className="ml-auto"
          disabled={selected.length === 0}
          onClick={openMigrate}
        >
          Migrate{selected.length > 0 ? ` ${selected.length}` : ""}
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
      {!query.isPending && !query.error && candidates.length === 0 && (
        <p>No servers are backed by an external MCP attachment.</p>
      )}

      {dialogServers && (
        <MigrateDialog
          servers={dialogServers}
          names={names}
          onClose={() => setDialogServers(undefined)}
          onDone={recordRun}
        />
      )}
    </div>
  );
}
