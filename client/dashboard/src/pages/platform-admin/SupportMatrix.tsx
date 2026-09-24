import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { SupportCoverageCell } from "@gram/client/models/components/supportcoveragecell.js";
import { buildDeviceIntegrationCoverageQuery } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { buildTelemetrySupportCoverageQuery } from "@gram/client/react-query/telemetrySupportCoverage.js";
import { InternalAdminBadge } from "@/components/internal-admin-badge";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { PlatformAdminGate } from "./PlatformAdminGate";
import {
  activeAgentCoverageLabel,
  capabilities,
  cellKey,
  footprintOf,
  gapsClosedBy,
  indexCells,
  methods,
  surfaces,
  type CapabilityId,
  type IntegrationMethod,
  type SurfaceId,
} from "./support-matrix-model";

const WINDOW_DAYS = 30;

export default function SupportMatrix(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title area="Platform Admin">
            Support coverage
          </Page.Section.Title>
          <Page.Section.Description>
            Review observed evidence and recommended integrations for the
            current organization.
          </Page.Section.Description>
          <Page.Section.Body>
            <PlatformAdminGate>
              <OrganizationSupportMatrix />
            </PlatformAdminGate>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

function OrganizationSupportMatrix(): JSX.Element {
  const organization = useOrganization();
  const client = useSdkClient();

  const coverageQuery = buildTelemetrySupportCoverageQuery(client, {
    windowDays: WINDOW_DAYS,
  });
  const coverage = useQuery({
    ...coverageQuery,
    queryKey: [...coverageQuery.queryKey, { organizationId: organization.id }],
    staleTime: 60_000,
    throwOnError: false,
  });

  const deviceCoverageQuery = buildDeviceIntegrationCoverageQuery(client);
  const deviceCoverage = useQuery({
    ...deviceCoverageQuery,
    queryKey: [
      ...deviceCoverageQuery.queryKey,
      { organizationId: organization.id },
    ],
    staleTime: 60_000,
    throwOnError: false,
  });

  // Dropped on error: react-query keeps the last successful payload, which
  // would render stale values under the "unavailable" banner.
  const cells = useMemo(
    () => indexCells(coverage.isError ? undefined : coverage.data?.cells),
    [coverage.isError, coverage.data?.cells],
  );
  const coverageLoaded = !coverage.isError && !coverage.isPending;

  // Hovering a card highlights the cells it would fill.
  const [hoveredMethod, setHoveredMethod] = useState<IntegrationMethod | null>(
    null,
  );
  const highlighted = useMemo(
    () => (hoveredMethod ? footprintOf(hoveredMethod) : null),
    [hoveredMethod],
  );
  const highlightedGaps = useMemo(
    () => (hoveredMethod ? gapsClosedBy(hoveredMethod, cells) : null),
    [hoveredMethod, cells],
  );

  const observedSurfaces = surfaces.filter((surface) =>
    capabilities.some(
      (capability) =>
        cells.get(cellKey(capability.id, surface.id))?.status === "observed",
    ),
  ).length;
  // Gated for the same reason as the cells above: an ungated read shows a
  // stale or zeroed number while the banner says evidence is unavailable.
  const latestSeen = coverageLoaded
    ? latestEvidence(coverage.data?.cells)
    : null;
  const unmapped = coverageLoaded ? (coverage.data?.unmapped ?? []) : [];

  return (
    <main className="mx-auto max-w-[1240px] space-y-8 py-3">
      <header className="border-border flex flex-wrap items-center justify-between gap-4 border-b pb-4">
        <p className="text-muted-foreground max-w-2xl text-sm">
          Aggregate activity for the current organization during the last{" "}
          {WINDOW_DAYS} days. An empty cell means no evidence was found, not
          that the surface is unsupported.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <InternalAdminBadge />
          <Badge variant="neutral">
            <Badge.Text>{organization.name}</Badge.Text>
          </Badge>
          <EvidenceBadge
            isPending={coverage.isPending}
            isError={coverage.isError}
          />
        </div>
      </header>

      <section className="space-y-4">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <p className="text-eyebrow">Feature / surface</p>
            <h3 className="mt-2 text-xl font-medium">
              Observed organization coverage
            </h3>
          </div>
          <div className="flex flex-wrap gap-x-5 gap-y-2 font-mono text-[10px] tracking-[0.06em] uppercase">
            <Legend color="bg-success-default" label="Observed" />
            <Legend color="bg-muted-foreground/25" label="No evidence" />
            <Legend color="bg-warning-default" label="Not yet reportable" />
          </div>
        </div>

        {coverage.isError && (
          <div className="border-border bg-muted/30 border px-4 py-3 text-sm">
            Coverage evidence is unavailable. Cells are shown as unknown rather
            than reported as zero coverage.
          </div>
        )}

        {unmapped.length > 0 && (
          <div className="border-warning-default bg-muted/30 border px-4 py-3 text-sm">
            <span className="font-medium">
              {unmapped.length} unmapped {pluralize("source", unmapped.length)}.
            </span>{" "}
            Activity was observed under{" "}
            {unmapped.map((item) => item.hookSource).join(", ")}, which does not
            fold onto any surface below. The matrix is not showing everything
            this organization did.
          </div>
        )}

        <CoverageTable
          cells={cells}
          isPending={coverage.isPending}
          isError={coverage.isError}
          highlighted={highlighted}
          highlightedGaps={highlightedGaps}
        />
      </section>

      <section className="grid border-y sm:grid-cols-2 lg:grid-cols-4">
        <SummaryMetric
          value={
            coverageLoaded ? `${observedSurfaces}/${surfaces.length}` : "—"
          }
          label="surfaces with activity evidence"
        />
        <SummaryMetric
          value={latestSeen ? latestSeen.toLocaleDateString() : "—"}
          label="latest aggregate evidence"
        />
        <SummaryMetric
          value={deviceMetricValue(deviceCoverage)}
          label={activeAgentCoverageLabel(
            deviceCoverage.data?.attestation,
            deviceCoverage.data?.activeWindowMinutes,
          )}
        />
        <SummaryMetric
          value={`${WINDOW_DAYS} days`}
          label="observation window"
          last
        />
      </section>

      <IntegrationRecommendations
        cells={cells}
        isRanked={coverageLoaded}
        hoveredMethodId={hoveredMethod?.id ?? null}
        onHover={setHoveredMethod}
      />
    </main>
  );
}

function deviceMetricValue(query: {
  isPending: boolean;
  isError: boolean;
  data?: { agentActive?: number };
}): string {
  if (query.isPending) return "Loading…";
  if (query.isError) return "Unavailable";
  return String(query.data?.agentActive ?? 0);
}

function pluralize(word: string, count: number): string {
  return count === 1 ? word : `${word}s`;
}

function EvidenceBadge({
  isPending,
  isError,
}: {
  isPending: boolean;
  isError: boolean;
}) {
  if (isPending) {
    return (
      <Badge variant="neutral">
        <Badge.Text>Loading evidence…</Badge.Text>
      </Badge>
    );
  }
  if (isError) {
    return (
      <Badge variant="warning">
        <Badge.Text>Evidence unavailable</Badge.Text>
      </Badge>
    );
  }
  return (
    <Badge variant="success">
      <Badge.Text>Last {WINDOW_DAYS} days</Badge.Text>
    </Badge>
  );
}

function SummaryMetric({
  value,
  label,
  last = false,
}: {
  value: string;
  label: string;
  last?: boolean;
}) {
  return (
    <div
      className={cn(
        "border-border py-5 sm:px-5 sm:first:pl-0 sm:[&:nth-child(odd)]:border-r lg:border-r lg:[&:nth-child(odd)]:border-r",
        last && "lg:border-r-0",
      )}
    >
      <p className="text-2xl font-medium tracking-tight">{value}</p>
      <p className="text-muted-foreground mt-2 max-w-48 font-mono text-[10px] leading-relaxed tracking-[0.06em] uppercase">
        {label}
      </p>
    </div>
  );
}

function Legend({ color, label }: { color: string; label: string }) {
  return (
    <span className="flex items-center gap-2">
      <span className={cn("size-2", color)} />
      {label}
    </span>
  );
}

function CoverageTable({
  cells,
  isPending,
  isError,
  highlighted,
  highlightedGaps,
}: {
  cells: Map<string, SupportCoverageCell>;
  isPending: boolean;
  isError: boolean;
  highlighted: Set<string> | null;
  highlightedGaps: Set<string> | null;
}) {
  return (
    <div className="border-border dark:bg-background overflow-x-auto border bg-card">
      <table className="w-full min-w-[1080px] border-collapse text-sm">
        <thead>
          <tr>
            <th className="border-border text-muted-foreground h-20 w-52 border-r border-b p-4 text-left font-mono text-[10px] font-normal tracking-[0.08em] uppercase">
              Capability / surface
            </th>
            {surfaces.map((surface) => (
              <th
                key={surface.id}
                className="border-border h-20 min-w-36 border-r border-b p-4 text-left font-normal last:border-r-0"
              >
                <span className="text-[15px] font-medium">{surface.name}</span>
                <span className="text-muted-foreground mt-1 block font-mono text-[9px] font-normal tracking-[0.05em] uppercase">
                  {surface.detail}
                </span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {capabilities.map((capability) => (
            <tr key={capability.id}>
              <th className="border-border h-24 border-r border-b p-4 text-left font-normal last:border-b-0">
                <span className="text-[15px] font-medium">
                  {capability.name}
                </span>
                <span className="text-muted-foreground mt-1 block text-xs leading-snug">
                  {capability.description}
                </span>
              </th>
              {surfaces.map((surface) => {
                const key = cellKey(capability.id, surface.id);
                return (
                  <td
                    key={surface.id}
                    className="border-border h-24 border-r border-b p-2 last:border-r-0"
                  >
                    <EvidenceCell
                      capability={capability.id}
                      surface={surface.id}
                      cell={cells.get(key)}
                      isPending={isPending}
                      isError={isError}
                      closesGap={highlightedGaps?.has(key) ?? false}
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

function capabilityUnit(capability: CapabilityId): string {
  return capabilities.find((item) => item.id === capability)?.unit ?? "event";
}

function cellLabel(
  capability: CapabilityId,
  cell: SupportCoverageCell | undefined,
  isPending: boolean,
  isError: boolean,
): string {
  if (isPending) return "Loading…";
  if (isError || !cell) return "Evidence unavailable";

  switch (cell.status) {
    case "observed": {
      const unit = capabilityUnit(capability);
      return `${cell.value.toLocaleString()} ${pluralize(unit, cell.value)}`;
    }
    case "pending":
      return cell.detail || "Not yet reportable";
    case "none":
      return `No ${capabilityUnit(capability)} evidence`;
  }
}

function statusDotClass(
  cell: SupportCoverageCell | undefined,
  isPending: boolean,
): string {
  if (isPending || !cell) return "bg-muted-foreground/25";
  switch (cell.status) {
    case "observed":
      return "bg-success-default";
    case "pending":
      return "bg-warning-default";
    case "none":
      return "bg-muted-foreground/25";
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
  cell: SupportCoverageCell | undefined;
  isPending: boolean;
  isError: boolean;
  closesGap: boolean;
  dimmed: boolean;
}) {
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
            "flex min-h-20 flex-col justify-center border px-3 py-2 transition-opacity",
            observed ? "border-success-default" : "border-border bg-muted/15",
            // An already-observed cell in the footprint is not a gap.
            closesGap && "border-information-default border-2",
            dimmed && "opacity-40",
          )}
        >
          <span className="flex items-center gap-2">
            <span
              className={cn("size-2 shrink-0", statusDotClass(cell, isPending))}
            />
            <span className="text-xs leading-snug">{label}</span>
          </span>
          {cell?.lastSeen && observed && (
            <span className="text-muted-foreground mt-1 pl-4 font-mono text-[9px] tracking-wide uppercase">
              Last seen {new Date(cell.lastSeen).toLocaleDateString()}
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

function IntegrationRecommendations({
  cells,
  isRanked,
  hoveredMethodId,
  onHover,
}: {
  cells: Map<string, SupportCoverageCell>;
  isRanked: boolean;
  hoveredMethodId: string | null;
  onHover: (method: IntegrationMethod | null) => void;
}) {
  // Ranked by missing coverage closed, so the list is advice for this org.
  const ranked = useMemo(() => {
    const entries = methods.map((method) => ({
      method,
      gaps: gapsClosedBy(method, cells),
      footprint: footprintOf(method),
    }));
    if (!isRanked) {
      // Against an empty map every method's gaps equal its footprint, so
      // sorting would rank by footprint while the copy says otherwise.
      return entries;
    }
    return entries.sort((a, b) => b.gaps.size - a.gaps.size);
  }, [cells, isRanked]);

  return (
    <section className="space-y-4">
      <div>
        <p className="text-eyebrow">Integration footprints</p>
        <h3 className="mt-2 text-xl font-medium">Close the gaps</h3>
        <p className="text-muted-foreground mt-1 max-w-3xl text-sm">
          {isRanked
            ? "Ranked by how much of this organization's missing coverage each integration would close."
            : "Coverage evidence has not loaded, so these are not ranked against this organization yet."}{" "}
          Hover a card to highlight the cells it reaches above. Static product
          capability only — a card does not mean the integration is configured
          here.
        </p>
      </div>
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {ranked.map(({ method, gaps, footprint }) => (
          <RecommendationCard
            key={method.id}
            method={method}
            gapCount={isRanked ? gaps.size : null}
            footprintCount={footprint.size}
            isActive={hoveredMethodId === method.id}
            onHover={onHover}
          />
        ))}
      </div>
    </section>
  );
}

function RecommendationCard({
  method,
  gapCount,
  footprintCount,
  isActive,
  onHover,
}: {
  method: IntegrationMethod;
  gapCount: number | null;
  footprintCount: number;
  isActive: boolean;
  onHover: (method: IntegrationMethod | null) => void;
}) {
  const summary = recommendationSummary(gapCount, footprintCount);

  return (
    <article
      className={cn(
        "border-border bg-card border p-5 transition-colors",
        isActive && "border-information-default",
        gapCount === 0 && "opacity-70",
      )}
      onMouseEnter={() => onHover(method)}
      onMouseLeave={() => onHover(null)}
      onFocus={() => onHover(method)}
      onBlur={() => onHover(null)}
      tabIndex={0}
    >
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="font-medium">{method.name}</p>
          <p className="text-muted-foreground mt-1 font-mono text-[10px] tracking-wide uppercase">
            {method.setup}
          </p>
        </div>
        <span className="font-mono text-xs tabular-nums">
          {gapCount === null ? "—" : `+${gapCount}`}
        </span>
      </div>
      <p className="text-muted-foreground mt-3 text-sm leading-relaxed">
        {method.description}
      </p>
      <p className="text-muted-foreground mt-3 text-xs">{summary}</p>
    </article>
  );
}

function recommendationSummary(
  gapCount: number | null,
  footprintCount: number,
): string {
  if (gapCount === null) {
    return `Reaches ${footprintCount} ${pluralize("cell", footprintCount)}`;
  }
  if (gapCount === 0) {
    return "Everything it reaches is already reporting";
  }
  return `Would close ${gapCount} of ${footprintCount} ${pluralize("cell", footprintCount)} it reaches`;
}

function latestEvidence(cells: SupportCoverageCell[] | undefined): Date | null {
  let latest: Date | null = null;
  for (const cell of cells ?? []) {
    if (!cell.lastSeen) continue;
    const seen = new Date(cell.lastSeen);
    if (!latest || seen > latest) latest = seen;
  }
  return latest;
}
