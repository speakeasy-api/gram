import { useQuery } from "@tanstack/react-query";
import type { JSX } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { hooksReleaseProgressQuery } from "@/lib/adminQueries";
import {
  errorMessage,
  type AdminHooksReleaseInstallVersion,
  type AdminHooksReleaseProgress,
} from "@/lib/gramAdminApi";

/**
 * How far the current hooks release has reached: organizations the pins clear
 * for it, marketplace repositories republished with it, and installs running
 * its binary.
 */
export function ReleaseProgress(): JSX.Element {
  const query = useQuery({ ...hooksReleaseProgressQuery, throwOnError: false });

  return (
    <section className="border-border overflow-hidden rounded-md border">
      <div className="border-border bg-muted/50 space-y-1 border-b p-4">
        <h2 className="text-base font-semibold">Release progress</h2>
        <p className="text-muted-foreground text-sm">
          {query.data
            ? releaseLabel(query.data)
            : "How far the current hooks release has reached."}
        </p>
      </div>
      {query.isPending ? (
        <p role="status" className="p-4 text-sm">
          Loading release progress…
        </p>
      ) : !query.data ? (
        <div role="alert" className="flex items-center gap-3 p-4 text-sm">
          <span className="text-destructive">{errorMessage(query.error)}</span>
          <Button size="sm" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </div>
      ) : (
        <>
          {query.isError && (
            <div
              role="alert"
              className="flex items-center gap-3 px-4 pt-4 text-sm"
            >
              <span className="text-destructive">
                Unable to refresh release progress, so these counts may be out
                of date.
              </span>
              <Button size="sm" onClick={() => void query.refetch()}>
                Retry
              </Button>
            </div>
          )}
          <ProgressBody progress={query.data} />
        </>
      )}
    </section>
  );
}

function releaseLabel(progress: AdminHooksReleaseProgress): string {
  const binary = progress.current_binary_version
    ? `, binary ${progress.current_binary_version}`
    : "";
  return `Hooks version ${progress.current_version}${binary}.`;
}

function ProgressBody({
  progress,
}: {
  progress: AdminHooksReleaseProgress;
}): JSX.Element {
  const { fleet } = progress;
  return (
    <div className="space-y-6 p-4">
      <div className="space-y-4">
        <ProgressRow
          label="Organizations cleared"
          value={progress.organizations_cleared}
          total={progress.organizations}
          note={organizationsNote(progress)}
        />
        <ProgressRow
          label="Repositories republished"
          value={progress.repos_on_current}
          total={progress.repos}
          note="Marketplace repositories whose hooks plugin carries this version."
        />
        {fleet ? (
          <ProgressRow
            label="Installs running it"
            value={fleet.installs_on_current}
            total={fleet.installs}
            note={`Installs on exactly this binary that sent a hook in the last ${fleet.window_days} full days.`}
          />
        ) : (
          <p className="text-muted-foreground text-sm">
            Install counts are unavailable.
          </p>
        )}
      </div>
      <div className="grid gap-6 md:grid-cols-2">
        <VersionTable
          caption="Repositories by published version"
          unit="Repositories"
          rows={progress.repo_versions.map((row) => ({
            key: row.version,
            version: row.version || "Never published",
            current: row.version === String(progress.current_version),
            count: row.repos,
          }))}
        />
        {fleet && (
          <VersionTable
            caption="Installs by binary version"
            unit="Installs"
            rows={fleet.versions.map((row) => ({
              key: `${row.version}-${row.status}`,
              version: installVersionLabel(row),
              status: row.status === "current" ? undefined : row.status,
              current: row.status === "current",
              count: row.installs,
            }))}
          />
        )}
      </div>
    </div>
  );
}

function organizationsNote(progress: AdminHooksReleaseProgress): string {
  const parts = [
    "Organizations with a marketplace repository whose next publish moves to this version.",
  ];
  if (progress.organizations_held > 0) {
    parts.push(`${progress.organizations_held} held below it by a pin.`);
  }
  if (progress.organizations_on_legacy_flag > 0) {
    parts.push(
      `${progress.organizations_on_legacy_flag} follow the legacy PostHog flag, so they are not counted as cleared.`,
    );
  }
  return parts.join(" ");
}

function installVersionLabel(row: AdminHooksReleaseInstallVersion): string {
  if (row.version) return row.version;
  return row.status === "unsupported" ? "Legacy hooks" : "No version";
}

function ProgressRow({
  label,
  value,
  total,
  note,
}: {
  label: string;
  value: number;
  total: number;
  note: string;
}): JSX.Element {
  const percent = total > 0 ? Math.floor((value / total) * 100) : 0;
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-4 text-sm">
        <span className="font-medium">{label}</span>
        <span className="tabular-nums">
          {value.toLocaleString()} of {total.toLocaleString()}
          <span className="text-muted-foreground"> ({percent}%)</span>
        </span>
      </div>
      <div
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={total}
        aria-valuenow={value}
        className="bg-muted h-2 overflow-hidden rounded-full"
      >
        <div className="bg-primary h-full" style={{ width: `${percent}%` }} />
      </div>
      <p className="text-muted-foreground text-xs">{note}</p>
    </div>
  );
}

function VersionTable({
  caption,
  unit,
  rows,
}: {
  caption: string;
  unit: string;
  rows: {
    key: string;
    version: string;
    status?: string;
    current: boolean;
    count: number;
  }[];
}): JSX.Element {
  return (
    <div className="space-y-2">
      <h3 className="text-sm font-medium">{caption}</h3>
      {rows.length === 0 ? (
        <p className="text-muted-foreground text-sm">None yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Version</TableHead>
              <TableHead className="text-right">{unit}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <TableRow key={row.key}>
                <TableCell>
                  <span className="flex items-center gap-2">
                    {row.version}
                    {row.current && <Badge variant="outline">Current</Badge>}
                    {row.status && (
                      <span className="text-muted-foreground text-xs">
                        {row.status}
                      </span>
                    )}
                  </span>
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {row.count.toLocaleString()}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
