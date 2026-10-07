import type { JSX } from "react";

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  formatSpendRate,
  formatSpendUsage,
  formatSpendUsd,
  type SpendProduct,
} from "./spendBreakdownUtils";

// Product, usage, list rate and estimated cost for each product. `compact`
// drops the list-rate column for narrow cards and moves the rate into the cost
// cell's tooltip, so the figure is still one hover away.
export function SpendProductsTable({
  products,
  compact = false,
}: {
  products: SpendProduct[];
  compact?: boolean;
}): JSX.Element {
  const columns = compact ? 3 : 4;
  return (
    <div className="bg-card overflow-hidden rounded-md border">
      <Table>
        <TableHeader className="bg-muted">
          <TableRow>
            <TableHead>Product</TableHead>
            <TableHead className="text-right">Usage</TableHead>
            {!compact && (
              <TableHead className="text-right">Current list rate</TableHead>
            )}
            <TableHead className="text-right">Estimated cost</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {products.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={columns}
                className="text-muted-foreground h-20 text-center"
              >
                Select at least one product to see its estimate.
              </TableCell>
            </TableRow>
          ) : (
            products.map((product) => (
              <TableRow key={product.id}>
                <TableCell className="font-medium">{product.label}</TableCell>
                <TableCell
                  className="text-right tabular-nums"
                  title={`${BigInt(product.quantity).toLocaleString("en-US")} ${product.unit === "bytes" ? "bytes" : "tokens"}`}
                >
                  {formatSpendUsage(product)}
                </TableCell>
                {!compact && (
                  <TableCell className="text-right tabular-nums">
                    {formatSpendRate(product)}
                  </TableCell>
                )}
                <TableCell
                  className="text-right tabular-nums"
                  title={
                    compact
                      ? `Current list rate: ${formatSpendRate(product)}`
                      : undefined
                  }
                >
                  {formatSpendUsd(product.costUsd)}
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}
