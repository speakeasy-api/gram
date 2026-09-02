import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type JSX,
  type ReactNode,
} from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import { organizationQuery } from "@/lib/adminQueries";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { organizationActivityQuery } from "@/lib/gramAdminClient";
import type { AuditLog } from "@gram/admin-client/models/components/auditlog";

const TRIAL_ACTIONS = new Set([
  "organization:enterprise_trial_armed",
  "organization:enterprise_trial_extended",
  "organization:enterprise_trial_rearmed",
  "organization:enterprise_trial_demoted",
  "organization:enterprise_trial_converted",
]);

const UTC_DATE_TIME = new Intl.DateTimeFormat("en-US", {
  dateStyle: "full",
  timeStyle: "long",
  timeZone: "UTC",
});

function conversionSource(log: AuditLog): string | undefined {
  return typeof log.metadata?.conversion_source === "string"
    ? log.metadata.conversion_source
    : undefined;
}

function actorName(log: AuditLog): string {
  if (
    log.action === "organization:enterprise_trial_converted" &&
    conversionSource(log) === "stripe_checkout"
  ) {
    return "Stripe";
  }
  return (
    log.actorDisplayName ??
    (log.actorType === "system" || log.actorId === "system"
      ? "System"
      : undefined) ??
    log.actorSlug ??
    log.actorId
  );
}

function activityAction(log: AuditLog): string {
  switch (log.action) {
    case "organization:enterprise_trial_armed":
      return "started enterprise trial";
    case "organization:enterprise_trial_extended":
      return "extended enterprise trial";
    case "organization:enterprise_trial_rearmed":
      return "rearmed enterprise trial";
    case "organization:enterprise_trial_demoted":
      return "demoted enterprise trial";
    case "organization:enterprise_trial_converted":
      return conversionSource(log) === "stripe_checkout"
        ? "converted enterprise trial through checkout"
        : "marked enterprise trial converted";
    default:
      return log.action;
  }
}

function subjectName(log: AuditLog): string {
  return log.subjectDisplayName ?? log.subjectSlug ?? log.subjectId;
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function nestedValue(snapshot: unknown, group: string, field: string): unknown {
  if (!isRecord(snapshot) || !isRecord(snapshot[group])) return undefined;
  return snapshot[group][field];
}

function trialEnd(snapshot: unknown): unknown {
  const nested = nestedValue(snapshot, "trial", "ends_at");
  return nested ?? (isRecord(snapshot) ? snapshot.trial_ends_at : undefined);
}

function keyValue(snapshot: unknown, keyType: string, field: string): unknown {
  if (!isRecord(snapshot) || !Array.isArray(snapshot.keys)) return undefined;
  const key = snapshot.keys.find(
    (candidate) => isRecord(candidate) && candidate.key_type === keyType,
  );
  return isRecord(key) ? key[field] : undefined;
}

function specializedChangedFields(
  before: unknown,
  after: unknown,
): ChangedField[] {
  const changes: ChangedField[] = [];
  const add = (field: string, oldValue: unknown, newValue: unknown) => {
    if (JSON.stringify(oldValue) !== JSON.stringify(newValue)) {
      changes.push({ field, oldValue, newValue });
    }
  };
  for (const [label, group, field] of [
    ["Account type", "organization", "account_type"],
    ["Whitelisted", "organization", "whitelisted"],
    ["Disabled", "organization", "disabled"],
    ["Trial status", "trial", "status"],
    ["Trial tier", "trial", "tier"],
    ["Converted at", "trial", "converted_at"],
    ["Demoted at", "trial", "demoted_at"],
  ] as const) {
    const oldValue = nestedValue(before, group, field);
    const newValue = nestedValue(after, group, field);
    if (oldValue !== undefined || newValue !== undefined)
      add(label, oldValue, newValue);
  }
  const oldTrialEnd = trialEnd(before);
  const newTrialEnd = trialEnd(after);
  if (oldTrialEnd !== undefined || newTrialEnd !== undefined) {
    add("Trial end", oldTrialEnd, newTrialEnd);
  }
  const keyTypes = new Set<string>();
  for (const snapshot of [before, after]) {
    if (!isRecord(snapshot) || !Array.isArray(snapshot.keys)) continue;
    for (const key of snapshot.keys) {
      if (isRecord(key) && typeof key.key_type === "string")
        keyTypes.add(key.key_type);
    }
  }
  for (const keyType of [...keyTypes].sort()) {
    for (const [label, field] of [
      ["OpenRouter stored disabled", "stored_disabled"],
      ["OpenRouter effective disabled", "effective_disabled"],
      ["OpenRouter key access changed", "key_access_changed"],
      ["OpenRouter monthly cap", "monthly_credits"],
    ] as const) {
      const oldValue = keyValue(before, keyType, field);
      const newValue = keyValue(after, keyType, field);
      if (oldValue !== undefined || newValue !== undefined) {
        add(`${label} (${keyType})`, oldValue, newValue);
      }
    }
  }
  return changes;
}

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

  const generic = [...fields]
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
  return [...specializedChangedFields(before, after), ...generic];
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

function firstRecorded(...values: unknown[]): unknown {
  return values.find(
    (value) => value !== undefined && value !== null && value !== "",
  );
}

function formatDate(value: Date): string {
  return Number.isNaN(value.valueOf())
    ? "Unknown time"
    : UTC_DATE_TIME.format(value);
}

function factDate(value: unknown): string {
  if (value instanceof Date) return formatDate(value);
  return typeof value === "string"
    ? formatDate(new Date(value))
    : "Not recorded";
}

function recorded(value: unknown): string {
  return value === undefined || value === null || value === ""
    ? "Not recorded"
    : String(value);
}

function snapshotKeys(
  snapshot: Record<string, unknown>,
): Record<string, unknown>[] {
  return Array.isArray(snapshot.keys) ? snapshot.keys.filter(isRecord) : [];
}

function keyTypes(
  before: Record<string, unknown>,
  after: Record<string, unknown>,
): string[] {
  return [
    ...new Set(
      [...snapshotKeys(before), ...snapshotKeys(after)]
        .map((key) => key.key_type)
        .filter((value): value is string => typeof value === "string"),
    ),
  ];
}

function keyAccess(snapshot: Record<string, unknown>, keyType: string): string {
  const key = snapshotKeys(snapshot).find((item) => item.key_type === keyType);
  return typeof key?.effective_disabled === "boolean"
    ? key.effective_disabled
      ? "Disabled"
      : "Enabled"
    : "Not recorded";
}

function snapshotKeyAccessChanged(
  snapshot: Record<string, unknown>,
): boolean | undefined {
  const values = snapshotKeys(snapshot)
    .map((key) => key.key_access_changed)
    .filter((value): value is boolean => typeof value === "boolean");
  return values.length === 0 ? undefined : values.some(Boolean);
}

function TrialFacts({ log }: { log: AuditLog }): JSX.Element {
  const before = isRecord(log.beforeSnapshot) ? log.beforeSnapshot : {};
  const after = isRecord(log.afterSnapshot) ? log.afterSnapshot : {};
  const beforeEnd = firstRecorded(
    nestedValue(before, "trial", "ends_at"),
    before.trial_ends_at,
    log.metadata?.previous_trial_ends_at,
  );
  const afterEnd = firstRecorded(
    nestedValue(after, "trial", "ends_at"),
    after.trial_ends_at,
    log.metadata?.trial_ends_at,
  );
  const tier = firstRecorded(
    nestedValue(after, "trial", "tier"),
    nestedValue(before, "trial", "tier"),
    log.metadata?.tier,
    log.metadata?.account_type,
    log.metadata?.previous_account_type,
  );
  const facts: Array<[string, string]> = [];
  let title = "Enterprise trial updated";
  switch (log.action) {
    case "organization:enterprise_trial_armed":
      title = "Enterprise trial started";
      facts.push(
        ["Started", factDate(log.createdAt)],
        ["Trial end", factDate(afterEnd)],
        ["Tier", recorded(tier)],
      );
      break;
    case "organization:enterprise_trial_extended":
      title = "Enterprise trial extended";
      facts.push(
        ["Previous trial end", factDate(beforeEnd)],
        ["New trial end", factDate(afterEnd)],
        ["Tier", recorded(tier)],
      );
      break;
    case "organization:enterprise_trial_rearmed":
      title = "Enterprise trial restarted";
      facts.push(
        ["Restarted", factDate(log.createdAt)],
        ["Trial end", factDate(afterEnd)],
        ["Tier", recorded(tier)],
      );
      break;
    case "organization:enterprise_trial_demoted":
      title = "Enterprise trial ended";
      facts.push(
        [
          "Ended",
          factDate(
            firstRecorded(
              nestedValue(after, "trial", "demoted_at"),
              log.createdAt,
            ),
          ),
        ],
        ["Trial end", factDate(afterEnd)],
        ["Tier", recorded(tier)],
      );
      break;
    case "organization:enterprise_trial_converted":
      title = "Enterprise trial converted";
      facts.push(
        [
          "Converted",
          factDate(
            firstRecorded(
              nestedValue(after, "trial", "converted_at"),
              log.createdAt,
            ),
          ),
        ],
        [
          "Conversion method",
          conversionSource(log) === "stripe_checkout"
            ? "Stripe checkout"
            : conversionSource(log) === "platform_admin" ||
                conversionSource(log) === "admin"
              ? "Manual"
              : recorded(conversionSource(log)),
        ],
        ["Tier", recorded(tier)],
      );
      break;
  }
  const keyAccessChanged = firstRecorded(
    log.metadata?.key_access_changed,
    snapshotKeyAccessChanged(after),
    snapshotKeyAccessChanged(before),
  );
  if (
    [
      "organization:enterprise_trial_rearmed",
      "organization:enterprise_trial_demoted",
      "organization:enterprise_trial_converted",
    ].includes(log.action)
  ) {
    facts.push([
      "OpenRouter key access changed",
      keyAccessChanged === undefined
        ? "Not recorded"
        : keyAccessChanged
          ? "Yes"
          : "No",
    ]);
  }
  for (const keyType of keyTypes(before, after)) {
    const oldAccess = keyAccess(before, keyType);
    const newAccess = keyAccess(after, keyType);
    facts.push([
      `OpenRouter ${keyType} key`,
      oldAccess === newAccess || oldAccess === "Not recorded"
        ? newAccess
        : `${oldAccess} → ${newAccess}`,
    ]);
  }
  return (
    <section
      className="bg-muted mt-2 rounded-md px-3 py-2 text-xs"
      aria-label={title}
      role="region"
    >
      <h3 className="mb-1 font-medium">{title}</h3>
      <dl className="grid gap-x-4 gap-y-1 sm:grid-cols-2">
        {facts.map(([label, value]) => (
          <div className="contents" key={label}>
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

function ActivityItem({ log }: { log: AuditLog }): JSX.Element {
  const [diffExpanded, setDiffExpanded] = useState(false);
  const changes = useMemo(
    () => computeChangedFields(log.beforeSnapshot, log.afterSnapshot),
    [log.beforeSnapshot, log.afterSnapshot],
  );
  const showDiff = changes.length > 0;
  const diffId = `activity-diff-${log.id}`;

  return (
    <li className="rounded-lg border p-3">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1 text-sm">
        <time
          className="text-muted-foreground"
          dateTime={
            Number.isNaN(log.createdAt.valueOf())
              ? undefined
              : log.createdAt.toISOString()
          }
        >
          {formatDate(log.createdAt)}
        </time>
        <strong className="font-medium">{activityAction(log)}</strong>
        <span>{actorName(log)}</span>
        <span className="text-muted-foreground">→</span>
        <span>{subjectName(log)}</span>
        <span className="text-muted-foreground">via {log.actingSurface}</span>
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
      {TRIAL_ACTIONS.has(log.action) && <TrialFacts log={log} />}
      {showDiff && diffExpanded && (
        <div id={diffId} className="pt-2">
          <ActivityDiff
            before={log.beforeSnapshot}
            after={log.afterSnapshot}
            changes={changes}
          />
        </div>
      )}
      <details className="mt-2">
        <summary className="text-muted-foreground cursor-pointer text-sm">
          Event details for {log.action}
        </summary>
        <dl className="mt-2 grid gap-2 sm:grid-cols-2">
          {log.projectId && <Detail label="Project ID">{log.projectId}</Detail>}
          {log.projectSlug && (
            <Detail label="Project slug">{log.projectSlug}</Detail>
          )}
          <Detail label="Actor type">{log.actorType}</Detail>
          <Detail label="Actor ID">{log.actorId}</Detail>
          {log.actorSlug && <Detail label="Actor slug">{log.actorSlug}</Detail>}
          <Detail label="Subject type">{log.subjectType}</Detail>
          <Detail label="Subject ID">{log.subjectId}</Detail>
          {log.subjectSlug && (
            <Detail label="Subject slug">{log.subjectSlug}</Detail>
          )}
          {log.actingClientId && (
            <Detail label="Acting client ID">{log.actingClientId}</Detail>
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
  const fetchInFlight = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const fetchNextPage = query.fetchNextPage;
  const fetchMore = useCallback(async () => {
    if (fetchInFlight.current) return;
    fetchInFlight.current = true;
    try {
      await fetchNextPage({ cancelRefetch: false });
    } catch {
      // React Query exposes the incremental error below.
    } finally {
      if (mounted.current) fetchInFlight.current = false;
    }
  }, [fetchNextPage]);
  const logs = useMemo(() => {
    const byID = new Map<string, AuditLog>();
    for (const page of query.data?.pages ?? []) {
      for (const log of page.result.logs) {
        if (!byID.has(log.id)) byID.set(log.id, log);
      }
    }
    return [...byID.values()];
  }, [query.data?.pages]);

  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-lg font-semibold">Activity</h2>
      {query.isPending ? (
        <p
          role="status"
          aria-live="polite"
          className="text-muted-foreground text-sm"
        >
          Loading activity...
        </p>
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
      ) : logs.length === 0 && !query.hasNextPage ? (
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
            onClick={() => void fetchMore()}
          >
            {query.isFetchingNextPage ? "Retrying..." : "Retry loading more"}
          </Button>
        </div>
      ) : query.hasNextPage ? (
        <>
          {query.isFetchingNextPage && (
            <span role="status" aria-live="polite" className="sr-only">
              Loading more activity
            </span>
          )}
          <Button
            className="self-start"
            variant="outline"
            size="sm"
            disabled={query.isFetchingNextPage}
            onClick={() => void fetchMore()}
          >
            {query.isFetchingNextPage ? "Loading..." : "Load more"}
          </Button>
        </>
      ) : null}
    </section>
  );
}
