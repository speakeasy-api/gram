import { toast } from "sonner";
import { invalidateIssuerQueries } from "./issuerQueries";
import type { JSX } from "react";
import { useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createColumnHelper, useTable } from "@tanstack/react-table";
import type { IssuerFieldMismatch } from "@gram/admin-client/models/components/issuerfieldmismatch";
import type { IssuerConvergenceCandidate } from "@gram/admin-client/models/components/issuerconvergencecandidate";
import { InfoIcon } from "lucide-react";
import { dataTableFeatures, DataTable } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  adminGetGlobalIssuerMigratePreflightQuery,
  adminListGlobalIssuerConvergenceCandidatesQuery,
  adminMigrateToGlobalIssuer,
} from "@/lib/gramAdminClient";
export function ConvergenceHelp(): JSX.Element | null {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label="About convergence"
        >
          <InfoIcon className="size-4" />
        </Button>
      </TooltipTrigger>
      <TooltipContent className="max-w-sm">
        Organization or project issuers that use the same upstream identity
        provider as this platform issuer. They may be migrated to the shared
        platform configuration after compatibility checks pass.
      </TooltipContent>
    </Tooltip>
  );
}
function fieldLabel(field: string): string {
  const labels: Record<string, string> = {
    authorization_endpoint: "Authorization endpoint",
    token_endpoint: "Token endpoint",
    registration_endpoint: "Registration endpoint",
    jwks_uri: "JWKS URI",
    scopes_supported: "Scopes",
    grant_types_supported: "Grant types",
    response_types_supported: "Response types",
    token_endpoint_auth_methods_supported:
      "Token endpoint authentication methods",
    code_challenge_methods_supported: "PKCE code challenge methods",
    client_id_metadata_document_supported: "Client ID Metadata Document",
    revocation_endpoint: "Revocation endpoint",
    scope_override: "Scope override",
  };
  return (
    labels[field] ??
    field.replaceAll("_", " ").replace(/^./, (c) => c.toUpperCase())
  );
}
function Differences({ items }: { items: IssuerFieldMismatch[] }) {
  return (
    <dl className="grid gap-2 text-sm">
      {items.map((m, i) => (
        <div key={`${m.field}-${i}`}>
          <dt className="font-medium">{fieldLabel(m.field)}</dt>
          <dd className="break-all text-muted-foreground">
            Source:{" "}
            {m.sourceValues
              ? m.sourceValues.join(", ") || "empty"
              : m.sourceValue === ""
                ? "empty"
                : (m.sourceValue ?? "not set")}
            <br />
            Platform:{" "}
            {m.targetValues
              ? m.targetValues.join(", ") || "empty"
              : m.targetValue === ""
                ? "empty"
                : (m.targetValue ?? "not set")}
          </dd>
          {(m.sourceValues || m.targetValues) && (
            <>
              <dd>
                Added:{" "}
                {(m.targetValues ?? [])
                  .filter((value) => !m.sourceValues?.includes(value))
                  .join(", ") || "None"}
              </dd>
              <dd>
                Dropped:{" "}
                {(m.sourceValues ?? [])
                  .filter((value) => !m.targetValues?.includes(value))
                  .join(", ") || "None"}
              </dd>
            </>
          )}
        </div>
      ))}
    </dl>
  );
}
export function MigrationReview({
  sourceId,
  targetId,
  sourceName,
  sourceOwner,
  targetName,
  onClose,
}: {
  sourceId: string;
  targetId: string;
  sourceName?: string;
  sourceOwner?: string;
  targetName?: string;
  onClose: () => void;
}): JSX.Element | null {
  const cache = useQueryClient();
  const preflight = useQuery({
    ...adminGetGlobalIssuerMigratePreflightQuery({ sourceId, targetId }),
    staleTime: 0,
  });
  const [pending, setPending] = useState(false);
  const busy = useRef(false);
  const [error, setError] = useState("");
  const data = preflight.data;
  const migrate = async () => {
    if (
      busy.current ||
      !data?.canMigrate ||
      preflight.isFetching ||
      preflight.error
    )
      return;
    busy.current = true;
    setPending(true);
    setError("");
    try {
      await adminMigrateToGlobalIssuer({ sourceId, targetId });
      await invalidateIssuerQueries(cache);
      toast.success("Issuer consolidated");
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Migration failed");
      await preflight.refetch();
    } finally {
      busy.current = false;
      setPending(false);
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
          <DialogTitle>Consolidate {sourceName || sourceId}</DialogTitle>
          <DialogDescription>
            Move clients from {sourceName || sourceId}
            {` (owned by ${sourceOwner || "Unknown organization"})`} onto
            platform issuer {targetName || targetId}, then remove the original.
            Existing sessions keep working.
          </DialogDescription>
        </DialogHeader>
        {preflight.isPending && <p role="status">Checking compatibility…</p>}
        {preflight.error && (
          <p role="alert">
            {preflight.error.message}
            <Button variant="ghost" onClick={() => void preflight.refetch()}>
              Retry
            </Button>
          </p>
        )}
        {error && <p role="alert">{error}</p>}
        {data && (
          <div className="grid gap-4">
            <p>
              {data.clientCount} clients will move.{" "}
              {data.targetTenantClientCount} tenant clients already use this
              platform issuer.
            </p>
            {data.mcpServerNames.length > 0 && (
              <div>
                <h3 className="font-medium">Affected MCP servers</h3>
                <ul>
                  {data.mcpServerNames.map((n, i) => (
                    <li key={i}>{n}</li>
                  ))}
                </ul>
              </div>
            )}
            {data.endpointMismatches.length > 0 && (
              <section>
                <h3 className="font-medium">
                  Different authorization server — migration blocked
                </h3>
                <Differences items={data.endpointMismatches} />
              </section>
            )}
            {data.conflictingMcpServerNames.length > 0 && (
              <section>
                <h3 className="font-medium">
                  Conflicting MCP server bindings — migration blocked
                </h3>
                <ul>
                  {data.conflictingMcpServerNames.map((n, i) => (
                    <li key={i}>{n}</li>
                  ))}
                </ul>
              </section>
            )}
            {data.warnings.length > 0 && (
              <section>
                <h3 className="font-medium">
                  Metadata differs; platform values become authoritative
                </h3>
                <Differences items={data.warnings} />
              </section>
            )}
          </div>
        )}
        <DialogFooter>
          <Button variant="ghost" disabled={pending} onClick={onClose}>
            Cancel
          </Button>
          <Button
            disabled={
              pending ||
              preflight.isFetching ||
              !!preflight.error ||
              data?.canMigrate !== true
            }
            onClick={() => void migrate()}
          >
            {pending ? "Consolidating…" : "Consolidate"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
const column = createColumnHelper<
  typeof dataTableFeatures,
  IssuerConvergenceCandidate
>();
const empty: IssuerConvergenceCandidate[] = [];
export function Convergence({
  issuerId,
  targetName,
}: {
  issuerId: string;
  targetName?: string;
}): JSX.Element | null {
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [sourceId, setSourceId] = useState<string>();
  const query = useQuery(
    adminListGlobalIssuerConvergenceCandidatesQuery({
      targetId: issuerId,
      cursor: cursors.at(-1),
      limit: 50,
    }),
  );
  const columns = column.columns([
    column.display({
      id: "issuer",
      header: "Issuer",
      cell: ({ row }) => (
        <div>
          {row.original.issuer.name || row.original.issuer.issuer}
          <p className="text-muted-foreground text-xs">
            {row.original.issuer.issuer}
          </p>
        </div>
      ),
    }),
    column.display({
      id: "scope",
      header: "Scope",
      cell: ({ row }) => (
        <div>
          {row.original.organizationName ||
            row.original.organizationId ||
            "Unknown organization"}
          <p className="text-muted-foreground text-xs">
            {row.original.issuer.projectId
              ? `Project: ${row.original.issuer.projectId}`
              : "Organization"}
          </p>
        </div>
      ),
    }),
    column.accessor("clientCount", { header: "Clients" }),
    column.display({
      id: "status",
      header: "Compatibility",
      cell: ({ row }) =>
        row.original.endpointMismatches.length
          ? `Different authorization server (${row.original.endpointMismatches.map((m) => fieldLabel(m.field)).join(", ")} differ)`
          : row.original.warnings.length
            ? `${row.original.warnings.map((m) => fieldLabel(m.field)).join(", ")} differ; platform values become authoritative`
            : "No blockers found",
    }),
    column.display({
      id: "review",
      header: "",
      cell: ({ row }) => (
        <Button
          variant="outline"
          size="sm"
          onClick={() => setSourceId(row.original.issuer.id)}
        >
          Review
        </Button>
      ),
    }),
  ]);
  const table = useTable({
    features: dataTableFeatures,
    columns,
    data: query.data?.result.items ?? empty,
    getRowId: (r) => r.issuer.id,
  });
  return (
    <div className="grid gap-4">
      <div className="flex items-center gap-1">
        <h2 className="text-lg font-semibold">Convergence</h2>
        <ConvergenceHelp />
      </div>
      <p className="text-muted-foreground text-sm">
        Candidate status is advisory. Review runs the authoritative
        compatibility preflight.
      </p>
      {query.isPending && <p role="status">Loading candidates…</p>}
      {query.error && (
        <p role="alert">
          {query.error.message}
          <Button variant="ghost" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </p>
      )}
      <DataTable>
        <DataTable.Header table={table} />
        <DataTable.Body>
          {table.getRowModel().rows.map((row) => (
            <DataTable.Row key={row.id} row={row} />
          ))}
        </DataTable.Body>
      </DataTable>
      {query.data?.result.items.length === 0 && (
        <p>No matching organization or project issuers.</p>
      )}
      <div className="flex justify-end gap-2">
        <Button
          variant="outline"
          disabled={cursors.length === 1 || query.isFetching}
          onClick={() => setCursors((c) => c.slice(0, -1))}
        >
          Previous
        </Button>
        <Button
          variant="outline"
          disabled={!query.data?.result.nextCursor || query.isFetching}
          onClick={() =>
            setCursors((c) => [...c, query.data?.result.nextCursor])
          }
        >
          Next
        </Button>
      </div>
      {sourceId && (
        <MigrationReview
          sourceId={sourceId}
          targetId={issuerId}
          targetName={targetName}
          sourceName={
            query.data?.result.items
              .find((c) => c.issuer.id === sourceId)
              ?.issuer.name?.trim() ||
            query.data?.result.items
              .find((c) => c.issuer.id === sourceId)
              ?.issuer.issuer?.trim()
          }
          sourceOwner={
            query.data?.result.items.find((c) => c.issuer.id === sourceId)
              ?.organizationName ||
            query.data?.result.items.find((c) => c.issuer.id === sourceId)
              ?.organizationId ||
            "Unknown organization"
          }
          onClose={() => setSourceId(undefined)}
        />
      )}
    </div>
  );
}
