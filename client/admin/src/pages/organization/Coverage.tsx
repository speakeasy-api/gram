import { useMemo, useState, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import { Badge } from "@/components/ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { organizationQuery } from "@/lib/adminQueries";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { supportMatrixQuery } from "@/pages/coverage/api";
import { cn } from "@/lib/utils";

import {
  supportCoverageQuery,
  type CapabilityId,
  type CoverageCell,
  type SurfaceId,
} from "./coverageApi";
import {
  capabilities,
  cellKey,
  indexCells,
  methodFootprints,
  surfaces,
  type MethodFootprint,
} from "./coverageModel";

export function CoverageRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <Coverage key={data.id} org={data} />;
}

export function Coverage({ org }: { org: AdminOrganization }): JSX.Element {
  const coverage = useQuery({
    ...supportCoverageQuery(org.id),
    throwOnError: false,
  });
  const catalog = useQuery({ ...supportMatrixQuery, throwOnError: false });

  const loaded = !coverage.isError && !coverage.isPending;
  const cells = useMemo(
    () => indexCells(coverage.isError ? undefined : coverage.data?.cells),
    [coverage.isError, coverage.data?.cells],
  );
  const methods = useMemo(
    () => methodFootprints(catalog.data, cells, loaded),
    [catalog.data, cells, loaded],
  );

  const [hovered, setHovered] = useState<MethodFootprint | null>(null);
  const unmapped = loaded ? (coverage.data?.unmapped ?? []) : [];

  return (
    <div className="space-y-8 p-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-medium">Observed coverage</h2>
          <p className="text-muted-foreground text-sm">
            Aggregate activity for this organization over the last{" "}
            {coverage.data?.window_days ?? 30} days. An empty cell means no
            evidence was found, not that the surface is unsupported.
          </p>
        </div>
        {coverage.isError ? (
          <Badge variant="destructive">Evidence unavailable</Badge>
        ) : (
          <Badge variant="secondary">
            {coverage.isPending
              ? "Loading…"
              : `Last ${coverage.data?.window_days ?? 30} days`}
          </Badge>
        )}
      </header>

      {unmapped.length > 0 && (
        <p className="border-border bg-muted/40 border p-3 text-sm">
          <span className="font-medium">
            {unmapped.length} unmapped source
            {unmapped.length === 1 ? "" : "s"}.
          </span>{" "}
          Activity under {unmapped.map((u) => u.hook_source).join(", ")} folds
          onto no surface below, so the matrix is not showing everything.
        </p>
      )}

      <CoverageTable
        cells={cells}
        isPending={coverage.isPending}
        isError={coverage.isError}
        highlighted={hovered?.footprint ?? null}
        gaps={hovered?.gaps ?? null}
      />

      <section className="space-y-3">
        <div>
          <h2 className="text-lg font-medium">Close the gaps</h2>
          <p className="text-muted-foreground text-sm">
            {loaded
              ? "Integrations from the support matrix, ranked by how much of this organization's missing coverage each would close."
              : "Integrations from the support matrix. Not ranked against this organization yet."}{" "}
            Hover one to highlight the cells it claims.
          </p>
        </div>
        {catalog.isError ? (
          <p className="text-muted-foreground text-sm">
            The support matrix could not be loaded.
          </p>
        ) : (
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {methods.map((method) => (
              <MethodCard
                key={method.id}
                method={method}
                ranked={loaded}
                isActive={hovered?.id === method.id}
                onHover={setHovered}
              />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function CoverageTable({
  cells,
  isPending,
  isError,
  highlighted,
  gaps,
}: {
  cells: Map<string, CoverageCell>;
  isPending: boolean;
  isError: boolean;
  highlighted: Set<string> | null;
  gaps: Set<string> | null;
}): JSX.Element {
  return (
    <div className="border-border overflow-x-auto border">
      <table className="w-full min-w-[900px] border-collapse text-sm">
        <thead>
          <tr>
            <th className="border-border text-muted-foreground w-48 border-r border-b p-3 text-left text-xs font-normal uppercase">
              Capability / surface
            </th>
            {surfaces.map((surface) => (
              <th
                key={surface.id}
                className="border-border border-r border-b p-3 text-left font-medium last:border-r-0"
              >
                {surface.name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {capabilities.map((capability) => (
            <tr key={capability.id}>
              <th className="border-border border-r border-b p-3 text-left font-normal">
                <span className="font-medium">{capability.name}</span>
                <span className="text-muted-foreground mt-1 block text-xs">
                  {capability.description}
                </span>
              </th>
              {surfaces.map((surface) => {
                const key = cellKey(capability.id, surface.id);
                return (
                  <td
                    key={surface.id}
                    className="border-border border-r border-b p-2 last:border-r-0"
                  >
                    <EvidenceCell
                      capability={capability.id}
                      surface={surface.id}
                      cell={cells.get(key)}
                      isPending={isPending}
                      isError={isError}
                      closesGap={gaps?.has(key) ?? false}
                      dimmed={highlighted != null && !highlighted.has(key)}
                    />
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function unitFor(capability: CapabilityId): string {
  return capabilities.find((item) => item.id === capability)?.unit ?? "event";
}

function cellLabel(
  capability: CapabilityId,
  cell: CoverageCell | undefined,
  isPending: boolean,
  isError: boolean,
): string {
  if (isPending) return "Loading…";
  if (isError || !cell) return "Evidence unavailable";
  switch (cell.status) {
    case "observed": {
      const unit = unitFor(capability);
      return `${cell.value.toLocaleString()} ${unit}${cell.value === 1 ? "" : "s"}`;
    }
    case "pending":
      return cell.detail || "Not yet reportable";
    case "none":
      return `No ${unitFor(capability)} evidence`;
  }
}

function EvidenceCell({
  capability,
  surface,
  cell,
  isPending,
  isError,
  closesGap,
  dimmed,
}: {
  capability: CapabilityId;
  surface: SurfaceId;
  cell: CoverageCell | undefined;
  isPending: boolean;
  isError: boolean;
  closesGap: boolean;
  dimmed: boolean;
}): JSX.Element {
  const observed = cell?.status === "observed";
  const label = cellLabel(capability, cell, isPending, isError);
  const capabilityName =
    capabilities.find((item) => item.id === capability)?.name ?? capability;
  const surfaceName =
    surfaces.find((item) => item.id === surface)?.name ?? surface;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div
          tabIndex={0}
          className={cn(
            "flex min-h-16 flex-col justify-center border px-3 py-2 transition-opacity",
            observed ? "border-emerald-600/40" : "border-border bg-muted/20",
            closesGap && "border-2 border-sky-600",
            dimmed && "opacity-40",
          )}
        >
          <span className="text-xs">{label}</span>
          {observed && cell?.last_seen && (
            <span className="text-muted-foreground mt-1 text-[10px] uppercase">
              Last seen {new Date(cell.last_seen).toLocaleDateString()}
            </span>
          )}
        </div>
      </TooltipTrigger>
      <TooltipContent>
        <p className="font-medium">
          {capabilityName} · {surfaceName}
        </p>
        <p className="text-xs">{label}</p>
        {cell?.detail && cell.status !== "pending" && (
          <p className="text-xs">{cell.detail}</p>
        )}
      </TooltipContent>
    </Tooltip>
  );
}

function MethodCard({
  method,
  ranked,
  isActive,
  onHover,
}: {
  method: MethodFootprint;
  ranked: boolean;
  isActive: boolean;
  onHover: (method: MethodFootprint | null) => void;
}): JSX.Element {
  const summary = !ranked
    ? `Claims ${method.footprint.size} cell${method.footprint.size === 1 ? "" : "s"}`
    : method.gaps.size === 0
      ? "Everything it claims is already reporting"
      : `Would close ${method.gaps.size} of ${method.footprint.size} cells it claims`;

  return (
    <article
      tabIndex={0}
      onMouseEnter={() => onHover(method)}
      onMouseLeave={() => onHover(null)}
      onFocus={() => onHover(method)}
      onBlur={() => onHover(null)}
      className={cn(
        "border-border border p-4 transition-colors",
        isActive && "border-sky-600",
        ranked && method.gaps.size === 0 && "opacity-70",
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="font-medium">{method.name}</p>
          <p className="text-muted-foreground text-xs uppercase">
            {method.vendor}
          </p>
        </div>
        <span className="font-mono text-xs tabular-nums">
          {ranked ? `+${method.gaps.size}` : "—"}
        </span>
      </div>
      <p className="text-muted-foreground mt-3 text-sm">{summary}</p>
    </article>
  );
}
