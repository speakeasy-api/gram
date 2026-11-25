import type { JSX, ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import type { AdminAIProduct } from "@gram/admin-client/models/components/adminaiproduct";
import { ServiceError } from "@gram/admin-client/models/errors/serviceerror";

import { DataTable } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { aiProductQuery } from "@/lib/adminQueries";
import { badgeTone } from "@/lib/badgeTone";
import { errorMessage } from "@/lib/gramAdminApi";

import {
  categoryLabel,
  sourceLabel,
  staffLabel,
  verificationState,
} from "./aiProductLabels";

const DOCUMENT_COLUMNS = 5;

function Field({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="grid grid-cols-[12rem_1fr] gap-4 py-2 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  );
}

function Values({
  values,
  empty,
}: {
  values: string[];
  empty: string;
}): JSX.Element {
  if (values.length === 0) {
    return <span className="text-muted-foreground">{empty}</span>;
  }
  return (
    <ul className="flex flex-wrap gap-1">
      {values.map((value) => (
        <li key={value}>
          <code className="bg-muted rounded px-1.5 py-0.5 text-xs">
            {value}
          </code>
        </li>
      ))}
    </ul>
  );
}

function Overview({ product }: { product: AdminAIProduct }): JSX.Element {
  return (
    <section className="grid gap-2">
      <h2 className="text-base font-semibold">Overview</h2>
      <dl className="divide-y">
        <Field label="ID">
          <code className="text-xs">{product.id}</code>
        </Field>
        <Field label="Category">{categoryLabel(product.category)}</Field>
        <Field label="Vendor">
          <code className="text-xs">{product.vendorKey}</code>
        </Field>
        <Blocks product={product} />
        <Field label="Created">
          {product.createdAt.toLocaleString()}
          <div className="text-muted-foreground">
            {sourceLabel(product.createdBy)}
          </div>
        </Field>
        <Field label="Last changed">
          {product.updatedAt.toLocaleString()}
          {product.updatedBy && (
            <div className="text-muted-foreground">
              Changed by {staffLabel(product.updatedBy)}
            </div>
          )}
        </Field>
      </dl>
    </section>
  );
}

function Blocks({ product }: { product: AdminAIProduct }): JSX.Element {
  const count = product.blockedOrganizationCount;
  if (product.deleted && count > 0) {
    return (
      <Field label="Organizations with a block">
        {count}
        <div className="text-muted-foreground">
          These blocks stop applying once servers load the change (about a
          minute).
        </div>
      </Field>
    );
  }
  return (
    <Field label="Organizations blocking it">
      {count === 0 ? "None" : count}
    </Field>
  );
}

function Signatures({ product }: { product: AdminAIProduct }): JSX.Element {
  return (
    <section className="grid gap-2">
      <div>
        <h2 className="text-base font-semibold">Device signatures</h2>
        <p className="text-muted-foreground text-sm">
          What Shadow AI looks for to find this product on a device.
        </p>
      </div>
      <dl className="divide-y">
        <Field label="App bundle IDs">
          <Values values={product.bundleIds} empty="None" />
        </Field>
        <Field label="Commands">
          <Values values={product.binaries} empty="None" />
        </Field>
        <Field label="Config folders">
          <Values values={product.configDirs} empty="None" />
        </Field>
        <Field label="Process names">
          <Values values={product.processNames} empty="None" />
        </Field>
        <Field label="Version key">
          {product.versionPlistKey ? (
            <code className="text-xs">{product.versionPlistKey}</code>
          ) : (
            <span className="text-muted-foreground">
              Default (CFBundleShortVersionString)
            </span>
          )}
        </Field>
      </dl>
    </section>
  );
}

function ClientNames({ product }: { product: AdminAIProduct }): JSX.Element {
  return (
    <section className="grid gap-2">
      <div>
        <h2 className="text-base font-semibold">Reported names</h2>
        <p className="text-muted-foreground text-sm">
          Names the product sends when it connects to an MCP server. Any app can
          send any name, so these only label traffic. They never allow or block
          it.
        </p>
      </div>
      <Values values={product.clientInfoNames} empty="None" />
    </section>
  );
}

function Documents({ product }: { product: AdminAIProduct }): JSX.Element {
  return (
    <section className="grid gap-2">
      <div>
        <h2 className="text-base font-semibold">Documents</h2>
        <p className="text-muted-foreground text-sm">
          The sign-in documents this product publishes. A pattern matches many
          addresses: each * stands for one part of the path. Off documents are
          kept but no longer admit the app from the catalog. An issuer that
          accepts any client, or lists the address itself, can still admit it.
          Check shows whether Speakeasy fetched the live document.
        </p>
      </div>
      <div className="overflow-x-auto rounded-lg border">
        <DataTable cellPadding="condensed">
          <TableHeader className="bg-muted">
            <TableRow>
              <TableHead scope="col">Document</TableHead>
              <TableHead scope="col">Address</TableHead>
              <TableHead scope="col">Status</TableHead>
              <TableHead scope="col">Check</TableHead>
              <TableHead scope="col">Last changed</TableHead>
            </TableRow>
          </TableHeader>
          <DataTable.Body>
            {product.documents.length === 0 ? (
              <DataTable.NoResultsMessage
                colSpan={DOCUMENT_COLUMNS}
                className="text-muted-foreground"
              >
                This product has no documents.
              </DataTable.NoResultsMessage>
            ) : (
              product.documents.map((document) => {
                const check = verificationState(document);
                return (
                  <TableRow key={document.id}>
                    <TableCell className="font-medium">
                      {document.displayName}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <code className="text-xs break-all">
                          {document.url}
                        </code>
                        {document.isPattern && (
                          <Badge
                            variant="outline"
                            className={badgeTone.neutral}
                          >
                            Pattern
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      {document.deleted ? (
                        <Badge variant="outline" className={badgeTone.neutral}>
                          Deleted
                        </Badge>
                      ) : document.enabled ? (
                        <Badge variant="outline" className={badgeTone.success}>
                          On
                        </Badge>
                      ) : (
                        <Badge variant="outline" className={badgeTone.warning}>
                          Off
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant="outline"
                        className={check.tone}
                        title={check.detail}
                      >
                        {check.label}
                      </Badge>
                      {check.detail && (
                        <p className="text-muted-foreground mt-1 text-xs">
                          {check.detail}
                        </p>
                      )}
                    </TableCell>
                    <TableCell className="text-muted-foreground text-xs">
                      {document.updatedAt.toLocaleString()}
                      <div>
                        {document.updatedBy
                          ? `Changed by ${staffLabel(document.updatedBy)}`
                          : sourceLabel(document.createdBy)}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </DataTable.Body>
        </DataTable>
      </div>
    </section>
  );
}

export function AIProductDetail(): JSX.Element {
  const { productId } = useParams({ from: "/ai-clients/products/$productId" });
  const product = useQuery(aiProductQuery(productId));

  if (product.isPending) {
    return (
      <div className="p-6">
        <p className="text-muted-foreground text-sm">Loading product…</p>
      </div>
    );
  }

  if (product.error) {
    // Only the server's own not-found answer means the ID is unknown; a 404
    // from anything in front of it is an ordinary failure.
    const missing =
      product.error instanceof ServiceError &&
      product.error.statusCode === 404 &&
      product.error.data$.name === "not_found";
    return (
      <div role="alert" className="space-y-3 p-6">
        <p className="text-sm">
          {missing
            ? "No product has this ID."
            : `Could not load this product. ${errorMessage(product.error)}`}
        </p>
        <div className="flex gap-2">
          {!missing && (
            <Button
              variant="outline"
              size="sm"
              disabled={product.isFetching}
              onClick={() => void product.refetch()}
            >
              Try again
            </Button>
          )}
          <Button variant="ghost" size="sm" asChild>
            <Link to="/ai-clients/products" search={(previous) => previous}>
              Back to products
            </Link>
          </Button>
        </div>
      </div>
    );
  }

  const data = product.data;
  return (
    <div className="max-w-5xl space-y-8 p-6">
      <div className="space-y-1">
        <div className="flex items-center gap-2">
          <h1 className="text-2xl font-semibold">{data.displayName}</h1>
          {data.deleted && (
            <Badge variant="outline" className={badgeTone.neutral}>
              Deleted
            </Badge>
          )}
        </div>
        {data.deleted && (
          <p className="text-muted-foreground text-sm">
            This product is deleted. Once servers load the change (about a
            minute), Shadow AI stops finding it, sign-in stops recognizing it,
            and blocks on it stop applying.
          </p>
        )}
      </div>
      <Overview product={data} />
      <Signatures product={data} />
      <ClientNames product={data} />
      <Documents product={data} />
    </div>
  );
}
