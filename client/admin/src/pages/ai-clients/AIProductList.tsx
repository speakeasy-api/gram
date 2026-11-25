import { useState, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
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

import { aiProductsSearch, type AIProductsSearch } from "./aiProductsSearch";
import { categoryLabel } from "./aiProductLabels";

const COLUMNS = 6;

export function AIProductList(): JSX.Element {
  const location = useLocation();
  const search = aiProductsSearch(location.search);
  const navigate = useNavigate();
  // The box keeps its own text so a mid-string edit keeps the caret; it takes
  // the address's term only after a navigation it did not make.
  const [draft, setDraft] = useState({
    text: search.q ?? "",
    key: location.state.__TSR_key,
  });
  if (draft.key !== location.state.__TSR_key) {
    setDraft({
      text:
        "aiProductsFilter" in location.state ? draft.text : (search.q ?? ""),
      key: location.state.__TSR_key,
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

  const list = useQuery(
    aiProductsListQuery({
      query: search.q,
      includeDeleted: search.deleted ?? false,
    }),
  );
  const products = list.data?.products;

  return (
    <div className="space-y-6 p-6">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold">AI clients</h1>
        <p className="text-muted-foreground text-sm">
          Every product Speakeasy knows. Shadow AI uses a product to find it on
          devices. Sign-in uses its documents to recognize it. This page is
          read-only.
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-4">
        <Input
          aria-label="Search products"
          placeholder="Search by name or ID"
          className="w-full sm:w-80"
          value={draft.text}
          onChange={(event) => {
            setDraft({ ...draft, text: event.target.value });
            filter({ q: event.target.value || undefined });
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
      <div className="overflow-x-auto rounded-lg border">
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
                  : "The catalog has no products yet."}
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
