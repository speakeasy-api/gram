import { useState, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { createColumnHelper, useTable } from "@tanstack/react-table";
import type { ExternalCredentialSummary } from "@gram/admin-client/models/components/externalcredentialsummary";

import { DataTable, dataTableFeatures } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

import { CredentialEditor } from "./CredentialEditor";
import { platformCredentialsListQuery } from "./platformCredentialsClient";

const PROVIDER_LABELS: Record<ExternalCredentialSummary["provider"], string> = {
  gcp_iam: "GCP IAM",
  aws_iam: "AWS IAM",
};

const column = createColumnHelper<
  typeof dataTableFeatures,
  ExternalCredentialSummary
>();
const columns = column.columns([
  column.accessor("name", {
    header: "Name",
    cell: ({ row }) => <span className="font-medium">{row.original.name}</span>,
  }),
  column.accessor("provider", {
    header: "Provider",
    cell: ({ row }) => (
      <Badge variant="outline">{PROVIDER_LABELS[row.original.provider]}</Badge>
    ),
  }),
  column.accessor("updatedAt", {
    header: "Updated",
    cell: ({ row }) => row.original.updatedAt.toLocaleString(),
  }),
  column.display({
    id: "actions",
    header: "",
    // Only GCP credentials have a detail endpoint; AWS platform credentials
    // are listed so none goes unseen, but are not managed here.
    cell: ({ row }) =>
      row.original.provider === "gcp_iam" ? (
        <Button asChild variant="outline" size="sm">
          <Link
            to="/platform-credentials/$credentialId"
            params={{ credentialId: row.original.id }}
            aria-label={`View ${row.original.name}`}
          >
            View
          </Link>
        </Button>
      ) : null,
  }),
]);

const empty: ExternalCredentialSummary[] = [];

export function PlatformCredentialList(): JSX.Element {
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  const [pending, setPending] = useState(false);
  const query = useQuery(platformCredentialsListQuery());
  const table = useTable({
    features: dataTableFeatures,
    getRowId: (row) => row.id,
    data: query.data?.credentials ?? empty,
    columns,
  });

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">Platform credentials</h1>
          <p className="text-muted-foreground text-sm">
            How Speakeasy authenticates into a cloud provider to reach platform
            keys, such as the identity provider signing key. Shared by every
            organization.
          </p>
        </div>
        <Button onClick={() => setCreating(true)}>Create credential</Button>
      </div>
      {query.isPending && <p role="status">Loading credentials…</p>}
      {query.error && (
        <div role="alert">
          {query.error.message}
          <Button variant="ghost" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </div>
      )}
      {!query.error && query.data && query.data.credentials.length > 0 && (
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
      )}
      {!query.error && query.data?.credentials.length === 0 && (
        <p className="text-muted-foreground text-sm">
          No platform credentials. Create one before provisioning platform
          signing keys.
        </p>
      )}
      <Dialog
        open={creating}
        onOpenChange={(open) => {
          if (!pending) setCreating(open);
        }}
      >
        <DialogContent onInteractOutside={(e) => e.preventDefault()}>
          <DialogHeader>
            <DialogTitle>Create platform credential</DialogTitle>
            <DialogDescription>
              A GCP identity Speakeasy uses on behalf of every organization.
            </DialogDescription>
          </DialogHeader>
          {creating && (
            <CredentialEditor
              onPendingChange={setPending}
              onCancel={() => setCreating(false)}
              onSaved={async (saved) => {
                setCreating(false);
                await navigate({
                  to: "/platform-credentials/$credentialId",
                  params: { credentialId: saved.id },
                });
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
