import { useMemo, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { createColumnHelper, useTable } from "@tanstack/react-table";

import {
  dataTableFeatures,
  DataTable as Table,
  type DataTableFeatures,
} from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { organizationQuery } from "@/lib/adminQueries";
import { badgeTone } from "@/lib/badgeTone";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { fmtDateShort } from "@/lib/utils";

import {
  hookFleetQuery,
  type HookFleetBucket,
  type HookFleetInstall,
  type HookVersionStatus,
} from "./hookFleetApi";

const STATUS_TONE: Record<HookVersionStatus, string> = {
  current: badgeTone.success,
  behind: badgeTone.warning,
  unsupported: badgeTone.destructive,
  unknown: badgeTone.neutral,
};

const GENERATION_LABELS: Record<string, string> = {
  legacy_unkeyed: "Legacy (no key)",
  legacy_keyed: "Legacy (keyed)",
  ingest_script: "Ingest (script)",
  ingest_binary: "Ingest (binary)",
};

function VersionBadge({ status }: { status: HookVersionStatus }): JSX.Element {
  return (
    <Badge variant="outline" className={STATUS_TONE[status]}>
      {status}
    </Badge>
  );
}

const installColumn = createColumnHelper<DataTableFeatures, HookFleetInstall>();

const INSTALL_COLUMNS = installColumn.columns([
  installColumn.accessor("user_email", {
    header: "User",
    meta: { cellClassName: "whitespace-normal break-all" },
    cell: ({ row }) => (
      <span className="text-sm">{row.original.user_email || "-"}</span>
    ),
  }),
  installColumn.accessor("hostname", {
    header: "Machine",
    meta: { cellClassName: "whitespace-normal break-all" },
    cell: ({ row }) => (
      <span className="text-muted-foreground text-sm">
        {row.original.hostname || "-"}
      </span>
    ),
  }),
  installColumn.accessor("harness", {
    header: "Harness",
    cell: ({ row }) => <span className="text-sm">{row.original.harness}</span>,
  }),
  installColumn.accessor("version", {
    header: "Version",
    cell: ({ row }) => (
      <span className="flex items-center gap-2 text-sm">
        {row.original.version || "-"}
        <VersionBadge status={row.original.version_status} />
      </span>
    ),
  }),
  installColumn.accessor("endpoint_generation", {
    header: "Endpoint",
    cell: ({ row }) => (
      <span className="text-sm">
        {generationLabel(row.original.endpoint_generation)}
      </span>
    ),
  }),
  installColumn.accessor("install_route", {
    header: "Route",
    cell: ({ row }) => (
      <span className="text-sm">{row.original.install_route || "-"}</span>
    ),
  }),
  installColumn.accessor("last_seen", {
    header: "Last seen",
    cell: ({ row }) => (
      <span className="flex items-center gap-2 text-sm">
        {fmtDateShort(row.original.last_seen)}
        {row.original.quiet && (
          <Badge variant="outline" className={badgeTone.warning}>
            quiet
          </Badge>
        )}
      </span>
    ),
  }),
  installColumn.accessor("events", {
    header: "Events",
    cell: ({ row }) => (
      <span className="text-sm tabular-nums">
        {row.original.events.toLocaleString()}
      </span>
    ),
  }),
  installColumn.accessor("errors", {
    header: "Errors",
    cell: ({ row }) => (
      <span className="text-sm tabular-nums">
        {row.original.errors.toLocaleString()}
      </span>
    ),
  }),
]);

function generationLabel(value: string): string {
  if (!value) return "-";
  return GENERATION_LABELS[value] ?? value;
}

export function HookFleetRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <HookFleetView key={data.id} org={data} />;
}

export function HookFleetView({
  org,
}: {
  org: AdminOrganization;
}): JSX.Element {
  const fleet = useQuery({ ...hookFleetQuery(org.id), throwOnError: false });
  const data = fleet.isError ? undefined : fleet.data;
  const installs = useMemo(() => data?.installs ?? [], [data]);

  const table = useTable({
    features: dataTableFeatures,
    columns: INSTALL_COLUMNS,
    data: installs,
    getRowId: (i) =>
      `${i.project_id}/${i.user_email}/${i.hostname}/${i.harness}`,
  });
  const rows = table.getRowModel().rows;

  const behind = installs.filter(
    (i) => i.version_status === "behind" || i.version_status === "unsupported",
  ).length;

  return (
    <div className="space-y-8 p-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-lg font-medium">Hooks fleet</h2>
          <p className="text-muted-foreground text-sm">
            Every machine that sent hooks for this organization over the last{" "}
            {data?.window_days ?? 30} days.
            {data && (
              <>
                {" "}
                Speakeasy ships {data.latest_version}
                {data.minimum_version
                  ? `, and supports ${data.minimum_version} and newer`
                  : ""}
                . A machine is quiet after {data.quiet_after_days} days with no
                traffic.
              </>
            )}
          </p>
        </div>
        {fleet.isError ? (
          <Badge variant="destructive">Fleet unavailable</Badge>
        ) : (
          <Badge variant="secondary">
            {fleet.isPending ? "Loading…" : `Last ${data?.window_days} days`}
          </Badge>
        )}
      </header>

      {data?.truncated && (
        <p className="border-border bg-muted/40 border p-3 text-sm">
          This organization has more installs than shown. The counts below cover
          only the {data.total_installs.toLocaleString()} most recently seen.
        </p>
      )}

      {data && (
        <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Stat label="Installs" value={data.total_installs} />
          <Stat label="Behind or unsupported" value={behind} />
          <Stat label="Quiet" value={data.quiet_installs} />
          <Stat label="Legacy endpoint events" value={data.legacy_events} />
        </dl>
      )}

      {data && (
        <div className="grid gap-6 md:grid-cols-3">
          <section className="space-y-2">
            <h3 className="text-sm font-medium">Versions</h3>
            <ul className="divide-y rounded-lg border text-sm">
              {data.versions.map((v) => (
                <li
                  key={v.version}
                  className="flex items-center justify-between gap-2 px-3 py-2"
                >
                  <span className="flex items-center gap-2">
                    {v.version || "No version"}
                    <VersionBadge status={v.status} />
                  </span>
                  <span className="tabular-nums">{v.installs}</span>
                </li>
              ))}
              {data.versions.length === 0 && <EmptyItem />}
            </ul>
          </section>
          <BucketList title="Install routes" buckets={data.routes} />
          <BucketList
            title="Endpoints"
            buckets={data.generations}
            label={generationLabel}
            showEvents
          />
        </div>
      )}

      <section className="space-y-2">
        <h3 className="text-sm font-medium">Machines</h3>
        <div className="overflow-clip rounded-lg border">
          <Table cellPadding="condensed">
            <Table.Header table={table} />
            <Table.Body>
              {fleet.isPending || rows.length === 0 ? (
                <Table.NoResultsMessage>
                  <span className="text-muted-foreground text-sm">
                    {fleet.isPending
                      ? "Loading..."
                      : fleet.isError
                        ? "Unable to load hooks"
                        : "No hooks traffic in this window"}
                  </span>
                </Table.NoResultsMessage>
              ) : (
                rows.map((row) => <Table.Row key={row.id} row={row} />)
              )}
            </Table.Body>
          </Table>
        </div>
      </section>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }): JSX.Element {
  return (
    <div className="rounded-lg border p-3">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="text-lg font-medium tabular-nums">
        {value.toLocaleString()}
      </dd>
    </div>
  );
}

function EmptyItem(): JSX.Element {
  return <li className="text-muted-foreground px-3 py-2">None</li>;
}

function BucketList({
  title,
  buckets,
  label = (v) => v || "Unknown",
  showEvents = false,
}: {
  title: string;
  buckets: HookFleetBucket[];
  label?: (value: string) => string;
  showEvents?: boolean;
}): JSX.Element {
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-medium">{title}</h3>
      <ul className="divide-y rounded-lg border text-sm">
        {buckets.map((b) => (
          <li
            key={b.value}
            className="flex items-center justify-between gap-2 px-3 py-2"
          >
            <span>{label(b.value) === "-" ? "Unknown" : label(b.value)}</span>
            <span className="text-muted-foreground tabular-nums">
              <span className="text-foreground">{b.installs}</span>
              {showEvents && ` · ${b.events.toLocaleString()} events`}
            </span>
          </li>
        ))}
        {buckets.length === 0 && <EmptyItem />}
      </ul>
    </section>
  );
}
