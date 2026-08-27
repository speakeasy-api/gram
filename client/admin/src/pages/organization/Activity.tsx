import { useMemo, useState, type JSX, type ReactNode } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import {
  organizationActivityQuery,
  organizationQuery,
} from "@/lib/adminQueries";
import type { AdminAuditLog, AdminOrganization } from "@/lib/gramAdminApi";

const UTC_DATE_TIME = new Intl.DateTimeFormat("en-US", {
  dateStyle: "full",
  timeStyle: "long",
  timeZone: "UTC",
});

function actorName(log: AdminAuditLog): string {
  return (
    log.actor_display_name ??
    (log.actor_type === "system" ? "System" : undefined) ??
    log.actor_slug ??
    log.actor_id
  );
}

function subjectName(log: AdminAuditLog): string {
  return log.subject_display_name ?? log.subject_slug ?? log.subject_id;
}

function Detail({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt className="text-muted-foreground text-xs font-medium">{label}</dt>
      <dd className="break-all text-sm">{children}</dd>
    </div>
  );
}

function JsonDetail({ label, value }: { label: string; value: unknown }) {
  if (value === undefined) return null;
  return (
    <Detail label={label}>
      <pre className="bg-muted mt-1 overflow-x-auto whitespace-pre-wrap break-words rounded-md p-2 font-mono text-xs">
        {JSON.stringify(value, null, 2)}
      </pre>
    </Detail>
  );
}

type ChangedField = {
  field: string;
  oldValue: unknown;
  newValue: unknown;
};

function computeChangedFields(before: unknown, after: unknown): ChangedField[] {
  const beforeObject =
    before != null && typeof before === "object"
      ? (before as Record<string, unknown>)
      : {};
  const afterObject =
    after != null && typeof after === "object"
      ? (after as Record<string, unknown>)
      : {};
  const fields = new Set([
    ...Object.keys(beforeObject),
    ...Object.keys(afterObject),
  ]);

  return [...fields]
    .filter(
      (field) =>
        JSON.stringify(beforeObject[field]) !==
        JSON.stringify(afterObject[field]),
    )
    .map((field) => ({
      field,
      oldValue: beforeObject[field],
      newValue: afterObject[field],
    }))
    .sort((left, right) => left.field.localeCompare(right.field));
}

function formatChangedValue(value: unknown): string {
  if (value === undefined) return "(none)";
  return JSON.stringify(value);
}

function ChangedFieldRow({ change }: { change: ChangedField }) {
  return (
    <div
      className="border-border/50 flex items-start gap-3 border-b px-3 py-2 last:border-b-0"
      data-field={change.field}
      role="listitem"
    >
      <span className="text-muted-foreground w-[140px] shrink-0 pt-0.5 font-mono text-xs font-medium">
        {change.field}
      </span>
      <div className="flex min-w-0 flex-1 flex-wrap items-start gap-2">
        <span className="max-w-full bg-red-50 px-2 py-0.5 font-mono text-xs break-all text-red-700 line-through dark:bg-red-950 dark:text-red-400">
          {formatChangedValue(change.oldValue)}
        </span>
        <span className="text-muted-foreground pt-0.5 text-xs">→</span>
        <span className="max-w-full bg-emerald-50 px-2 py-0.5 font-mono text-xs break-all text-emerald-700 dark:bg-emerald-950 dark:text-emerald-400">
          {formatChangedValue(change.newValue)}
        </span>
      </div>
    </div>
  );
}

function ActivityDiff({
  before,
  after,
  changes,
}: {
  before: unknown;
  after: unknown;
  changes: ChangedField[];
}) {
  const [showRawDiff, setShowRawDiff] = useState(false);

  if (showRawDiff) {
    return (
      <section aria-label="Raw diff">
        <button
          type="button"
          className="mb-2 text-xs text-blue-500 hover:underline"
          onClick={() => setShowRawDiff(false)}
        >
          View structured diff
        </button>
        <dl className="grid gap-2 sm:grid-cols-2">
          <JsonDetail label="Before snapshot" value={before} />
          <JsonDetail label="After snapshot" value={after} />
        </dl>
      </section>
    );
  }

  return (
    <section aria-label="Changed fields">
      <div className="flex items-center gap-2 py-1">
        <h3 className="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
          Changed fields
        </h3>
        <div className="bg-border h-px flex-1" />
        <span className="text-muted-foreground text-[11px]">
          {changes.length} field{changes.length === 1 ? "" : "s"} changed
        </span>
      </div>
      <div className="bg-background border" role="list">
        {changes.map((change) => (
          <ChangedFieldRow key={change.field} change={change} />
        ))}
      </div>
      <button
        type="button"
        className="mt-2 text-xs text-blue-500 hover:underline"
        onClick={() => setShowRawDiff(true)}
      >
        View raw diff
      </button>
    </section>
  );
}

function ActivityItem({ log }: { log: AdminAuditLog }): JSX.Element {
  const [diffExpanded, setDiffExpanded] = useState(false);
  const changes = useMemo(
    () => computeChangedFields(log.before_snapshot, log.after_snapshot),
    [log.before_snapshot, log.after_snapshot],
  );
  const showDiff = changes.length > 0;
  const diffId = `activity-diff-${log.id}`;

  return (
    <li className="rounded-lg border p-3">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1 text-sm">
        <time className="text-muted-foreground" dateTime={log.created_at}>
          {UTC_DATE_TIME.format(new Date(log.created_at))}
        </time>
        <strong className="font-medium">{log.action}</strong>
        <span>{actorName(log)}</span>
        <span className="text-muted-foreground">→</span>
        <span>{subjectName(log)}</span>
        <span className="text-muted-foreground">via {log.acting_surface}</span>
        {showDiff && (
          <button
            type="button"
            className="ml-2 text-xs text-blue-500 hover:underline"
            aria-controls={diffId}
            aria-expanded={diffExpanded}
            onClick={() => setDiffExpanded((expanded) => !expanded)}
          >
            {diffExpanded ? "Hide diff ▴" : "Show diff ▾"}
          </button>
        )}
      </div>
      {showDiff && diffExpanded && (
        <div id={diffId} className="pt-2">
          <ActivityDiff
            before={log.before_snapshot}
            after={log.after_snapshot}
            changes={changes}
          />
        </div>
      )}
      <details className="mt-2">
        <summary className="text-muted-foreground cursor-pointer text-sm">
          Event details for {log.action}
        </summary>
        <dl className="mt-2 grid gap-2 sm:grid-cols-2">
          {log.project_id && (
            <Detail label="Project ID">{log.project_id}</Detail>
          )}
          {log.project_slug && (
            <Detail label="Project slug">{log.project_slug}</Detail>
          )}
          <Detail label="Actor type">{log.actor_type}</Detail>
          <Detail label="Actor ID">{log.actor_id}</Detail>
          {log.actor_slug && (
            <Detail label="Actor slug">{log.actor_slug}</Detail>
          )}
          <Detail label="Subject type">{log.subject_type}</Detail>
          <Detail label="Subject ID">{log.subject_id}</Detail>
          {log.subject_slug && (
            <Detail label="Subject slug">{log.subject_slug}</Detail>
          )}
          {log.acting_client_id && (
            <Detail label="Acting client ID">{log.acting_client_id}</Detail>
          )}
          <JsonDetail label="Metadata" value={log.metadata} />
        </dl>
      </details>
    </li>
  );
}

export function ActivityRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <Activity org={data} />;
}

export function Activity({ org }: { org: AdminOrganization }): JSX.Element {
  const query = useInfiniteQuery(organizationActivityQuery(org.id));
  const logs = query.data?.pages.flatMap((page) => page.logs) ?? [];

  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-lg font-semibold">Activity</h2>
      {query.isPending ? (
        <p className="text-muted-foreground text-sm">Loading activity...</p>
      ) : query.isError && query.data === undefined ? (
        <div role="alert" className="flex items-center gap-2">
          <p className="text-destructive text-sm">Unable to load activity</p>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void query.refetch()}
          >
            Retry
          </Button>
        </div>
      ) : logs.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No activity for this organization
        </p>
      ) : (
        <ol className="flex flex-col gap-2">
          {logs.map((log) => (
            <ActivityItem key={log.id} log={log} />
          ))}
        </ol>
      )}
      {query.isRefetchError ? (
        <div role="alert" className="flex items-center gap-2">
          <p className="text-destructive text-sm">Unable to refresh activity</p>
          <Button
            variant="outline"
            size="sm"
            disabled={query.isRefetching}
            onClick={() => void query.refetch()}
          >
            {query.isRefetching ? "Retrying refresh..." : "Retry refresh"}
          </Button>
        </div>
      ) : null}
      {query.isFetchNextPageError ? (
        <div role="alert" className="flex items-center gap-2">
          <p className="text-destructive text-sm">
            Unable to load more activity
          </p>
          <Button
            variant="outline"
            size="sm"
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage()}
          >
            {query.isFetchingNextPage ? "Retrying..." : "Retry loading more"}
          </Button>
        </div>
      ) : query.hasNextPage ? (
        <Button
          className="self-start"
          variant="outline"
          size="sm"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          {query.isFetchingNextPage ? "Loading..." : "Load more"}
        </Button>
      ) : null}
    </section>
  );
}
