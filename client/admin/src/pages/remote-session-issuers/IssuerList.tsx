import type { JSX } from "react";
import { useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { createColumnHelper, useTable } from "@tanstack/react-table";
import type { GlobalRemoteSessionIssuer } from "@gram/admin-client/models/components/globalremotesessionissuer";
import { dataTableFeatures, DataTable } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { adminListGlobalIssuersQuery } from "@/lib/gramAdminClient";
import { IssuerLogo } from "./IssuerLogo";
import { IssuerActions } from "./IssuerActions";
import { IssuerEditor } from "./IssuerEditor";
const column = createColumnHelper<
  typeof dataTableFeatures,
  GlobalRemoteSessionIssuer
>();
const columns = column.columns([
  column.display({
    id: "provider",
    header: "Provider",
    cell: ({ row }) => (
      <div>
        {row.original.issuer.logoAssetId && (
          <IssuerLogo id={row.original.issuer.logoAssetId} />
        )}
        <div className="font-medium">
          {row.original.issuer.name || row.original.issuer.issuer}
        </div>
        <div className="text-muted-foreground text-xs">
          {row.original.issuer.issuer}
        </div>
      </div>
    ),
  }),
  column.accessor((r) => r.issuer.slug, { id: "slug", header: "Slug" }),
  column.accessor("globalClientCount", { header: "Platform clients" }),
  column.accessor("tenantClientCount", { header: "Tenant clients" }),
  column.display({
    id: "management",
    header: "Actions",
    cell: ({ row }) => <IssuerActions record={row.original} />,
  }),
  column.display({
    id: "actions",
    header: "",
    cell: ({ row }) => (
      <Button asChild variant="outline" size="sm">
        <Link
          to="/remote-session-issuers/$issuerId"
          params={{ issuerId: row.original.issuer.id }}
        >
          View
        </Link>
      </Button>
    ),
  }),
]);
const empty: GlobalRemoteSessionIssuer[] = [];
export function IssuerList(): JSX.Element | null {
  const navigate = useNavigate();
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [creating, setCreating] = useState(false);
  const [pending, setPending] = useState(false);
  const query = useQuery({
    ...adminListGlobalIssuersQuery({ cursor: cursors.at(-1), limit: 50 }),
    placeholderData: keepPreviousData,
  });
  const table = useTable({
    features: dataTableFeatures,
    getRowId: (row) => row.issuer.id,
    data: query.data?.result.items ?? empty,
    columns,
  });
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">Remote session issuers</h1>
          <p className="text-muted-foreground text-sm">
            Shared platform identity provider configuration.
          </p>
        </div>
        <Button onClick={() => setCreating(true)}>Create issuer</Button>
      </div>
      {query.isPending && <p role="status">Loading issuers…</p>}
      {query.error && (
        <div role="alert">
          {query.error.message}
          <Button variant="ghost" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </div>
      )}
      <div className="overflow-auto">
        <DataTable>
          <DataTable.Header table={table} />
          <DataTable.Body>
            {table.getRowModel().rows.map((row) => (
              <DataTable.Row key={row.id} row={row} />
            ))}
          </DataTable.Body>
        </DataTable>
      </div>
      {!query.error && query.data?.result.items.length === 0 && (
        <p>No issuers found</p>
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
      <Dialog
        open={creating}
        onOpenChange={(open) => {
          if (!pending) setCreating(open);
        }}
      >
        <DialogContent
          className="max-h-[90vh] overflow-y-auto sm:max-w-2xl"
          onInteractOutside={(e) => e.preventDefault()}
        >
          <DialogHeader>
            <DialogTitle>Create issuer</DialogTitle>
            <DialogDescription>
              Add a shared platform identity provider.
            </DialogDescription>
          </DialogHeader>
          {creating && (
            <IssuerEditor
              onCreated={async (id) => {
                await navigate({
                  to: "/remote-session-issuers/$issuerId",
                  params: { issuerId: id },
                });
              }}
              onPendingChange={setPending}
              onDone={() => setCreating(false)}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
