import type { JSX } from "react";
import { accountLabels, type AccountFilter } from "./accounts";
import {
  osSummary,
  type MatrixCell,
  type MethodContribution,
} from "./matrixCell";
import { statusLabels } from "./model";

function methodNames(entries: MethodContribution[]): string {
  return entries
    .map(
      ({ method, fact }) =>
        `${method.name}${fact.verify ? " (needs verification)" : ""}`,
    )
    .join(", ");
}

export function CoverageTooltip({
  account,
  cell,
}: {
  account: AccountFilter;
  cell: MatrixCell;
}): JSX.Element {
  const supported = cell.contributions.filter(
    ({ fact }) => fact.status === "supported",
  );
  const partial = cell.contributions.filter(
    ({ fact }) => fact.status === "partial",
  );
  const os = osSummary(cell);
  return (
    <div className="space-y-1">
      <p className="font-medium">
        {account === "all" ? "Coverage" : accountLabels[account]} ·{" "}
        {statusLabels[cell.fact.status]}
      </p>
      <p>
        {supported.length
          ? `Supported by: ${methodNames(supported)}`
          : "No confirmed supporting methods."}
      </p>
      {partial.length > 0 && <p>Partial coverage: {methodNames(partial)}</p>}
      {cell.method && cell.fact.note && (
        <p className="whitespace-pre-wrap opacity-75">{cell.fact.note}</p>
      )}
      {cell.support?.note && (
        <p className="whitespace-pre-wrap opacity-75">{cell.support.note}</p>
      )}
      {os && <p className="opacity-75">Operating systems: {os}</p>}
    </div>
  );
}
