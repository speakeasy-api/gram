import { useState, type JSX } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link, useLocation, useNavigate } from "@tanstack/react-router";

import { DataTable } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { aiProductsListQuery } from "@/lib/adminQueries";
import { badgeTone } from "@/lib/badgeTone";
import { errorMessage } from "@/lib/gramAdminApi";

import {
  aiProductsSearch,
  capQuery,
  MAX_QUERY_LENGTH,
  type AIProductsSearch,
} from "./aiProductsSearch";
import { categoryLabel } from "./aiProductLabels";

const COLUMNS = 6;

export function AIProductList(): JSX.Element {
  const location = useLocation();
  const search = aiProductsSearch(location.search);
  const navigate = useNavigate();
  // The box keeps its own text so a mid-string edit keeps the caret. Its own
  // edits replace the current history entry, so it keeps that text only when
  // the entry is still the one it edited; a new entry or a step back or
  // forward takes the address's term.
  const [draft, setDraft] = useState({
    text: search.q ?? "",
    key: location.state.__TSR_key,
    index: location.state.__TSR_index,
  });
  if (draft.key !== location.state.__TSR_key) {
    const ownEdit =
      "aiProductsFilter" in location.state &&
      location.state.__TSR_index === draft.index;
    setDraft({
      text: ownEdit ? draft.text : (search.q ?? ""),
      key: location.state.__TSR_key,
      index: location.state.__TSR_index,
    });
  }
  const filter = (next: AIProductsSearch): void => {
    void navigate({
      to: ".",
      search: (previous) => ({ ...previous, ...next }),
      state: (previous) => ({ ...previous, aiProductsFilter: true }),
      replace: true,
    });
  };

  const list = useQuery({
    ...aiProductsListQuery({
      query: search.q,
      includeDeleted: search.deleted ?? false,
    }),
    placeholderData: keepPreviousData,
  });
  const products = list.data?.products;
  // Rows from the previous filters stay on screen until the new ones land, so
  // they are dimmed and cannot be opened.
  const stale = list.isPlaceholderData;

  return (
    <div className="space-y-6 p-6">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold">AI clients</h1>
        <p className="text-muted-foreground text-sm">
          This page shows the catalog as stored in the database. Servers use a
          built-in list if it is missing or invalid at startup, and keep their
          last good copy if a later change is invalid. Shadow AI finds a product
          on devices by its signatures. Sign-in recognizes it by its documents.
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-4">
        <Input
          aria-label="Search products"
          placeholder="Search by name or ID"
          className="w-full sm:w-80"
          maxLength={MAX_QUERY_LENGTH}
          value={draft.text}
          onChange={(event) => {
            const text = capQuery(event.target.value);
            setDraft({ ...draft, text });
            filter({ q: text || undefined });
          }}
        />
        <label className="flex items-center gap-2 text-sm">
          <Switch
            checked={search.deleted ?? false}
            onCheckedChange={(checked) =>
              filter({ deleted: checked ? true : undefined })
            }
          />
          Show deleted products
        </label>
      </div>
      {list.error && (
        <div role="alert" className="space-y-2">
          <p className="text-destructive text-sm">
            Could not load products. {errorMessage(list.error)}
          </p>
          <Button
            variant="outline"
            size="sm"
            disabled={list.isFetching}
            onClick={() => void list.refetch()}
          >
            Try again
          </Button>
        </div>
      )}
      <div
        className={`overflow-x-auto rounded-lg border transition-opacity ${stale ? "opacity-60" : ""}`}
        aria-busy={stale}
        inert={stale}
      >
        <DataTable cellPadding="condensed">
          <TableHeader className="bg-muted">
            <TableRow>
              <TableHead scope="col">Product</TableHead>
              <TableHead scope="col">ID</TableHead>
              <TableHead scope="col">Category</TableHead>
              <TableHead scope="col">Vendor</TableHead>
              <TableHead scope="col" className="text-right">
                Documents
              </TableHead>
              <TableHead scope="col">Last changed</TableHead>
            </TableRow>
          </TableHeader>
          <DataTable.Body>
            {list.isPending ? (
              <DataTable.NoResultsMessage
                colSpan={COLUMNS}
                className="text-muted-foreground"
              >
                Loading products…
              </DataTable.NoResultsMessage>
            ) : list.error ? null : products?.length === 0 ? (
              <DataTable.NoResultsMessage
                colSpan={COLUMNS}
                className="text-muted-foreground"
              >
                {search.q
                  ? "No products match your search."
                  : search.deleted
                    ? "No products in the database. Servers use the built-in list until the catalog is seeded."
                    : "No active products. Turn on Show deleted products to see removed ones."}
              </DataTable.NoResultsMessage>
            ) : (
              products?.map((product) => (
                <TableRow key={product.id}>
                  <TableCell className="font-medium">
                    <div className="flex items-center gap-2">
                      <Link
                        to="/ai-clients/products/$productId"
                        params={{ productId: product.id }}
                        search={(previous) => previous}
                        className="hover:underline"
                      >
                        {product.displayName}
                      </Link>
                      {product.deleted && (
                        <Badge variant="outline" className={badgeTone.neutral}>
                          Deleted
                        </Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {product.id}
                  </TableCell>
                  <TableCell>{categoryLabel(product.category)}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {product.vendorKey}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {product.documentCount}
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {product.updatedAt.toLocaleDateString()}
                  </TableCell>
                </TableRow>
              ))
            )}
          </DataTable.Body>
        </DataTable>
      </div>
      {list.data?.truncated && (
        <p className="text-muted-foreground text-sm">
          Only the first {products?.length} products are shown. Search to find
          the rest.
        </p>
      )}
    </div>
  );
}
