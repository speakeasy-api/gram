import { useMemo, useRef, useState, type JSX } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { createColumnHelper, useTable } from "@tanstack/react-table";
import { toast } from "sonner";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import {
  dataTableFeatures,
  DataTable as Table,
  type DataTableFeatures,
} from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  organizationAssistantsQuery,
  organizationQuery,
} from "@/lib/adminQueries";
import {
  errorMessage,
  migrateOrganizationAssistantIdentities,
  type AdminAssistantIdentityMigrationRun,
  type AdminAssistantIdentityState,
  type AdminOrganization,
  type AdminOrganizationAssistant,
} from "@/lib/gramAdminApi";
import { fmtDateShort } from "@/lib/utils";

const assistantColumn = createColumnHelper<
  DataTableFeatures,
  AdminOrganizationAssistant
>();

const IDENTITY_LABELS: Record<AdminAssistantIdentityState, string> = {
  NEVER_CONFIGURED: "None",
  ACTIVE: "Agent",
  UNAVAILABLE: "Unavailable",
};

const RUNTIME_LABELS: Record<number, string> = { 2: "Default", 3: "Kit" };

function assistantsMessage(isPending: boolean, isError: boolean): string {
  if (isPending) return "Loading...";
  if (isError) return "Unable to load assistants";
  return "No assistants in this organization";
}

function IdentityBadge({
  state,
}: {
  state: AdminAssistantIdentityState;
}): JSX.Element {
  return (
    <Badge
      variant={
        state === "ACTIVE"
          ? "secondary"
          : state === "UNAVAILABLE"
            ? "destructive"
            : "outline"
      }
    >
      {IDENTITY_LABELS[state]}
    </Badge>
  );
}

function assistantColumns() {
  return assistantColumn.columns([
    assistantColumn.accessor("name", {
      header: "Name",
      meta: { cellClassName: "whitespace-normal" },
      cell: ({ row }) => <span className="text-sm">{row.original.name}</span>,
    }),
    assistantColumn.accessor("project_name", {
      header: "Project",
      meta: { cellClassName: "whitespace-normal" },
      cell: ({ row }) => (
        <span className="text-sm">{row.original.project_name}</span>
      ),
    }),
    assistantColumn.accessor("identity_state", {
      header: "Identity",
      cell: ({ row }) => <IdentityBadge state={row.original.identity_state} />,
    }),
    assistantColumn.accessor("runtime_version", {
      header: "Runtime",
      cell: ({ row }) => (
        <span className="text-sm">
          {RUNTIME_LABELS[row.original.runtime_version] ??
            String(row.original.runtime_version)}
        </span>
      ),
    }),
    assistantColumn.accessor("created_by_user_email", {
      header: "Creator",
      meta: { cellClassName: "whitespace-normal break-all" },
      cell: ({ row }) => (
        <span className="text-sm">
          {row.original.created_by_user_email ?? "Unknown"}
          {!row.original.creator_active && (
            <span className="text-muted-foreground"> (not a member)</span>
          )}
        </span>
      ),
    }),
    assistantColumn.accessor("created_at", {
      header: "Created",
      cell: ({ row }) => (
        <span className="text-sm">{fmtDateShort(row.original.created_at)}</span>
      ),
    }),
  ]);
}

function MigrationSummary({
  run,
}: {
  run: AdminAssistantIdentityMigrationRun;
}): JSX.Element {
  const skipped = run.results.filter(
    (result) => result.outcome === "needs_owner" || result.outcome === "failed",
  );
  return (
    <div className="flex flex-col gap-2 rounded-lg border p-3 text-sm">
      <p className="font-medium">
        Migrated {run.migrated}, already migrated {run.already_migrated}, needs
        owner {run.needs_owner}, failed {run.failed}
      </p>
      {skipped.length > 0 && (
        <ul className="text-muted-foreground list-disc space-y-1 pl-5">
          {skipped.map((result) => (
            <li key={result.assistant_id}>
              {result.name}:{" "}
              {result.outcome === "needs_owner"
                ? "its creator is no longer a member, so nobody can own its agent"
                : "provisioning failed; run the migration again"}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function AssistantsRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <Assistants org={data} />;
}

export function Assistants({ org }: { org: AdminOrganization }): JSX.Element {
  const qc = useQueryClient();
  const query = organizationAssistantsQuery(org.id);
  const { data, isPending, isError } = useQuery({
    ...query,
    enabled: !!org.id,
  });
  const [confirm, confirmDialog] = useConfirmDialog();
  const buttonRef = useRef<HTMLButtonElement>(null);
  const [lastRun, setLastRun] =
    useState<AdminAssistantIdentityMigrationRun | null>(null);
  const migrate = useMutation({
    mutationFn: () => migrateOrganizationAssistantIdentities(org.id),
    onSuccess: async (run) => {
      setLastRun(run);
      await qc.invalidateQueries({ queryKey: query.queryKey });
    },
  });

  const assistants = useMemo(() => data?.assistants ?? [], [data]);
  const unmigrated = assistants.filter(
    (assistant) => assistant.identity_state === "NEVER_CONFIGURED",
  );
  const migratable = unmigrated.filter((assistant) => assistant.creator_active);

  const columns = useMemo(() => assistantColumns(), []);
  const table = useTable({
    features: dataTableFeatures,
    columns,
    data: assistants,
    getRowId: (assistant) => assistant.id,
  });
  const rows = table.getRowModel().rows;

  const run = async (): Promise<void> => {
    const confirmed = await confirm({
      title: `Migrate ${org.name} assistants to agent identity?`,
      description:
        "Each assistant without an agent identity gets its own agent, and its turns run as that agent instead of as its owner.",
      details: (
        <ul className="text-muted-foreground list-disc space-y-1 pl-5 text-sm">
          <li>
            {migratable.length} of {unmigrated.length} assistants without an
            agent identity can be migrated
          </li>
          <li>
            Each new agent is owned by the assistant's creator and can use every
            MCP server and skill in its project
          </li>
          <li>Assistants whose creator is no longer a member are skipped</li>
          <li>The customer sees this in their audit log</li>
        </ul>
      ),
      confirmLabel: "Migrate",
    });
    // The dialog has no opener to give the keyboard back to on its own.
    buttonRef.current?.focus();
    if (!confirmed) return;
    try {
      const result = await migrate.mutateAsync();
      toast.success("Assistant identity migration ran", {
        description: `${result.migrated} migrated`,
      });
    } catch (error) {
      toast.error(errorMessage(error));
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        {rows.length > 0 && (
          <p className="text-sm font-medium">
            {rows.length === 1 ? "1 assistant" : `${rows.length} assistants`},{" "}
            {unmigrated.length} without an agent identity
          </p>
        )}
        <Button
          ref={buttonRef}
          size="sm"
          className="ml-auto"
          disabled={migrate.isPending || migratable.length === 0}
          onClick={() => {
            void run();
          }}
        >
          {migrate.isPending ? "Migrating..." : "Migrate to agent identity"}
        </Button>
      </div>
      {lastRun?.organization_id === org.id && (
        <MigrationSummary run={lastRun} />
      )}
      <div className="overflow-clip rounded-lg border">
        <Table cellPadding="condensed">
          <Table.Header table={table} />
          <Table.Body>
            {isPending || rows.length === 0 ? (
              <Table.NoResultsMessage>
                <span className="text-muted-foreground text-sm">
                  {assistantsMessage(isPending, isError)}
                </span>
              </Table.NoResultsMessage>
            ) : (
              rows.map((row) => <Table.Row key={row.id} row={row} />)
            )}
          </Table.Body>
        </Table>
      </div>
      {confirmDialog}
    </div>
  );
}
