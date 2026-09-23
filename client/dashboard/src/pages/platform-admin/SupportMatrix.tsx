import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import type { QueryResult } from "@gram/client/models/components/queryresult.js";
import { buildDeviceIntegrationCoverageQuery } from "@gram/client/react-query/deviceIntegrationCoverage.js";
import { unwrapAsync } from "@gram/client/types/fp.js";
import { InternalAdminBadge } from "@/components/internal-admin-badge";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { PlatformAdminGate } from "./PlatformAdminGate";
import {
  activeAgentCoverageLabel,
  capabilities,
  methods,
  surfaceForHookSource,
  surfaces,
  type CapabilityId,
  type SurfaceId,
} from "./support-matrix-model";

const WINDOW_DAYS = 30;

type SurfaceEvidence = {
  sessions: number;
  tokens: number;
  lastSeen: Date | null;
};

type EvidenceStatus = "loading" | "unavailable" | "ready";

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
            Review aggregate evidence and potential integration coverage for the
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
  const from = useMemo(
    () => new Date(Date.now() - WINDOW_DAYS * 86_400_000),
    [],
  );
  const to = useMemo(() => new Date(), []);
  const telemetry = useQuery({
    queryKey: [
      "support-coverage",
      organization.id,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from,
            to,
            groupBy: "hook_source",
            sortBy: "total_chats",
            topN: 1000,
            granularitySeconds: 86_400,
            includeDimensionValues: false,
          },
        }),
      ),
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
  const evidence = useMemo(
    () => buildEvidence(telemetry.data),
    [telemetry.data],
  );
  const telemetryStatus: EvidenceStatus = telemetry.isPending
    ? "loading"
    : telemetry.isError
      ? "unavailable"
      : "ready";
  const deviceStatus: EvidenceStatus = deviceCoverage.isPending
    ? "loading"
    : deviceCoverage.isError
      ? "unavailable"
      : "ready";
  const observedSurfaces = [...evidence.values()].filter(
    (item) => item.sessions > 0 || item.tokens > 0,
  ).length;
  const latestSeen = latestEvidence(evidence);

  return (
    <main className="mx-auto max-w-[1240px] space-y-8 py-3">
      <header className="border-border flex flex-wrap items-center justify-between gap-4 border-b pb-4">
        <p className="text-muted-foreground max-w-2xl text-sm">
          Aggregate activity for the current organization during the last{" "}
          {WINDOW_DAYS} days. Empty cells mean unknown—not unsupported.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <InternalAdminBadge />
          <Badge variant="neutral">
            <Badge.Text>{organization.name}</Badge.Text>
          </Badge>
          <EvidenceBadge status={telemetryStatus} />
        </div>
      </header>

      <section className="space-y-4">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <p className="text-muted-foreground font-mono text-[10px] tracking-[0.1em] uppercase">
              Feature / surface
            </p>
            <h3 className="mt-2 text-xl font-medium">
              Observed organization coverage
            </h3>
          </div>
          <div className="flex flex-wrap gap-x-5 gap-y-2 font-mono text-[10px] tracking-[0.06em] uppercase">
            <Legend color="bg-success-default" label="Observed" />
            <Legend color="bg-muted-foreground/25" label="No evidence" />
            <Legend color="border border-foreground/20" label="Unavailable" />
          </div>
        </div>
        {telemetryStatus === "unavailable" && (
          <div className="border-border bg-muted/30 border px-4 py-3 text-sm">
            Aggregate telemetry is unavailable. Cells remain unknown rather than
            being reported as zero coverage.
          </div>
        )}
        <CoverageTable evidence={evidence} telemetryStatus={telemetryStatus} />
      </section>

      <section className="grid border-y sm:grid-cols-2 lg:grid-cols-4">
        <SummaryMetric
          value={
            telemetryStatus === "ready"
              ? `${observedSurfaces}/${surfaces.length}`
              : "—"
          }
          label="surfaces with activity evidence"
        />
        <SummaryMetric
          value={latestSeen ? latestSeen.toLocaleDateString() : "—"}
          label="latest aggregate evidence"
        />
        <SummaryMetric
          value={
            deviceStatus === "loading"
              ? "Loading…"
              : deviceStatus === "unavailable"
                ? "Unavailable"
                : String(deviceCoverage.data?.agentActive ?? 0)
          }
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

      <IntegrationFootprints />
    </main>
  );
}

function EvidenceBadge({ status }: { status: EvidenceStatus }) {
  if (status === "loading") {
    return (
      <Badge variant="neutral">
        <Badge.Text>Loading evidence…</Badge.Text>
      </Badge>
    );
  }
  if (status === "unavailable") {
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
  evidence,
  telemetryStatus,
}: {
  evidence: Map<SurfaceId, SurfaceEvidence>;
  telemetryStatus: EvidenceStatus;
}) {
  return (
    <div className="border-border overflow-x-auto border bg-white dark:bg-background">
      <table className="w-full min-w-[1080px] border-collapse text-sm">
        <thead>
          <tr>
            <th className="border-border h-20 w-52 border-r border-b p-4 text-left font-mono text-[10px] font-normal tracking-[0.08em] text-muted-foreground uppercase">
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
              {surfaces.map((surface) => (
                <td
                  key={surface.id}
                  className="border-border h-24 border-r border-b p-2 last:border-r-0"
                >
                  <EvidenceCell
                    capability={capability.id}
                    evidence={evidence.get(surface.id)}
                    telemetryStatus={telemetryStatus}
                  />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function EvidenceCell({
  capability,
  evidence,
  telemetryStatus,
}: {
  capability: CapabilityId;
  evidence?: SurfaceEvidence;
  telemetryStatus: EvidenceStatus;
}) {
  const unsupportedLabels: Partial<Record<CapabilityId, string>> = {
    blocking: "Policy data unavailable",
    identity: "Attribution unavailable",
    shadow: "Device detail unavailable",
  };
  const unsupportedLabel = unsupportedLabels[capability];
  const value =
    capability === "session"
      ? (evidence?.sessions ?? 0)
      : capability === "cost"
        ? (evidence?.tokens ?? 0)
        : null;
  const observed = telemetryStatus === "ready" && value !== null && value > 0;
  const label = unsupportedLabel
    ? unsupportedLabel
    : telemetryStatus === "loading"
      ? "Loading…"
      : telemetryStatus === "unavailable"
        ? "Evidence unavailable"
        : capability === "session"
          ? observed
            ? `${value.toLocaleString()} sessions`
            : "No activity evidence"
          : observed
            ? `${value.toLocaleString()} tokens`
            : "No token evidence";

  return (
    <div
      className={cn(
        "flex min-h-20 flex-col justify-center border px-3 py-2",
        observed
          ? "border-success-default/30 bg-success-default/10"
          : "border-border bg-muted/15",
      )}
    >
      <span className="flex items-center gap-2">
        <span
          className={cn(
            "size-2 shrink-0",
            observed ? "bg-success-default" : "bg-muted-foreground/25",
          )}
        />
        <span className="text-xs leading-snug">{label}</span>
      </span>
      {evidence?.lastSeen && observed && (
        <span className="text-muted-foreground mt-1 pl-4 font-mono text-[9px] tracking-wide uppercase">
          Last seen {evidence.lastSeen.toLocaleDateString()}
        </span>
      )}
    </div>
  );
}

function IntegrationFootprints() {
  const totalCells = surfaces.length * capabilities.length;
  return (
    <section className="space-y-4">
      <div>
        <p className="text-muted-foreground font-mono text-[10px] tracking-[0.1em] uppercase">
          Integration footprints
        </p>
        <h3 className="mt-2 text-xl font-medium">Potential coverage paths</h3>
        <p className="text-muted-foreground mt-1 max-w-3xl text-sm">
          Static product capability only. A filled cell indicates a potential
          path; it does not mean the integration is configured or reporting for
          this organization.
        </p>
      </div>
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {methods.map((method) => {
          const footprint = surfaces.flatMap((surface) =>
            capabilities.map(
              (capability) =>
                method.surfaces.includes(surface.id) &&
                method.capabilities.includes(capability.id),
            ),
          );
          const covered = footprint.filter(Boolean).length;
          return (
            <article
              key={method.id}
              className="border-border border bg-card p-5"
            >
              <div className="flex items-start justify-between gap-3">
                <div>
                  <p className="font-medium">{method.name}</p>
                  <p className="text-muted-foreground mt-1 font-mono text-[10px] tracking-wide uppercase">
                    {method.setup}
                  </p>
                </div>
                <span className="font-mono text-xs tabular-nums">
                  {covered}/{totalCells}
                </span>
              </div>
              <p className="text-muted-foreground mt-3 text-sm leading-relaxed">
                {method.description}
              </p>
              <div
                className="mt-5 grid grid-cols-6 gap-1"
                aria-label={`${covered} of ${totalCells} potential coverage cells`}
              >
                {footprint.map((isCovered, index) => (
                  <span
                    key={index}
                    aria-hidden="true"
                    className={cn(
                      "h-1.5",
                      isCovered ? "bg-[#2873D7]" : "bg-muted",
                    )}
                  />
                ))}
              </div>
            </article>
          );
        })}
      </div>
    </section>
  );
}

function latestEvidence(
  evidence: Map<SurfaceId, SurfaceEvidence>,
): Date | null {
  let latest: Date | null = null;
  for (const item of evidence.values()) {
    if (item.lastSeen && (!latest || item.lastSeen > latest))
      latest = item.lastSeen;
  }
  return latest;
}

function buildEvidence(
  data: QueryResult | undefined,
): Map<SurfaceId, SurfaceEvidence> {
  const result = new Map<SurfaceId, SurfaceEvidence>();
  for (const row of data?.table ?? []) {
    if (
      !row.groupValue ||
      row.groupValue === "Other" ||
      /^Other \(\d+\)$/.test(row.groupValue)
    )
      continue;
    const surface = surfaceForHookSource(row.groupValue);
    if (!surface) continue;
    const current = result.get(surface) ?? {
      sessions: 0,
      tokens: 0,
      lastSeen: null,
    };
    current.sessions += Number(row.measures.totalChats);
    current.tokens += Number(row.measures.totalTokens);
    const series = data?.timeseries.find(
      (item) => item.groupValue === row.groupValue,
    );
    const latest = [...(series?.points ?? [])]
      .reverse()
      .find(
        (point) =>
          Number(point.measures.totalChats) +
            Number(point.measures.totalToolCalls) +
            Number(point.measures.totalTokens) >
          0,
      );
    if (latest) {
      const latestDate = new Date(
        Number(BigInt(latest.bucketTimeUnixNano) / 1_000_000n),
      );
      if (!current.lastSeen || latestDate > current.lastSeen)
        current.lastSeen = latestDate;
    }
    result.set(surface, current);
  }
  return result;
}
