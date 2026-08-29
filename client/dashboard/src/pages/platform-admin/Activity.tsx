import { Button } from "@/components/ui/Button";
import { useInfiniteQuery } from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { AdminSection } from "./AdminSection";

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

type ActivityLog = {
  id: string;
  action: string;
  actor_display_name?: string;
  actor_id: string;
  actor_type: string;
  acting_surface?: string;
  subject_display_name?: string;
  subject_type?: string;
  created_at: string;
  metadata?: Record<string, unknown>;
  before_snapshot?: unknown;
  after_snapshot?: unknown;
};

type ActivityPage = { logs: ActivityLog[]; next_cursor?: string };

type SafeSnapshot = {
  organization?: {
    account_type?: unknown;
    whitelisted?: unknown;
    disabled?: unknown;
  };
  trial?: {
    status?: unknown;
    tier?: unknown;
    ends_at?: unknown;
    converted_at?: unknown;
    demoted_at?: unknown;
  };
  keys?: Array<{
    key_type?: unknown;
    disable_causes?: unknown;
    stored_disabled?: unknown;
    effective_disabled?: unknown;
    key_access_changed?: unknown;
    monthly_credits?: unknown;
  }>;
  trial_ends_at?: unknown;
};

type DiffRow = {
  label: string;
  field: string;
  before: unknown;
  after: unknown;
};

export function OrganizationActivity({
  organizationId,
}: {
  organizationId: string;
}): JSX.Element {
  const query = useInfiniteQuery({
    queryKey: ["platform-admin", "organization-activity", organizationId],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }): Promise<ActivityPage> => {
      const params = new URLSearchParams({ organization_id: organizationId });
      if (pageParam) params.set("cursor", pageParam);
      const response = await fetch(`/admin/organization.activity?${params}`, {
        credentials: "same-origin",
      });
      if (!response.ok) throw new Error("organization activity request failed");
      return (await response.json()) as ActivityPage;
    },
    getNextPageParam: (page) => page.next_cursor,
    retry: false,
    throwOnError: false,
  });

  const fetchInFlight = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const fetchNextPage = query.fetchNextPage;
  const fetchOlder = useCallback(async () => {
    if (fetchInFlight.current) return;
    fetchInFlight.current = true;
    try {
      await fetchNextPage({ cancelRefetch: false });
    } catch {
      // React Query owns the incremental error state rendered below. Swallow the
      // expected rejected command promise so it cannot escape the click handler.
    } finally {
      if (mounted.current) fetchInFlight.current = false;
    }
  }, [fetchNextPage]);

  if (query.isLoading)
    return (
      <p
        role="status"
        aria-live="polite"
        className="px-4 py-3 text-sm text-muted-foreground"
      >
        Loading activity…
      </p>
    );
  if (query.isError && !query.data) {
    return (
      <div
        role="alert"
        className="flex flex-wrap items-center gap-3 px-4 py-3 text-sm"
      >
        <span>Activity could not be loaded.</span>
        <Button
          size="sm"
          variant="secondary"
          onClick={() => void query.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  }

  const byID = new Map<string, ActivityLog>();
  for (const page of query.data?.pages ?? []) {
    for (const log of page.logs) {
      if (!byID.has(log.id)) byID.set(log.id, log);
    }
  }
  const logs = [...byID.values()];

  return (
    <div className="divide-y">
      {logs.length === 0 && !query.hasNextPage && (
        <p className="px-4 py-3 text-sm text-muted-foreground">
          No activity yet.
        </p>
      )}
      {logs.map((log) => (
        <ActivityItem key={log.id} log={log} />
      ))}
      {query.isRefetchError && (
        <div
          role="alert"
          className="flex flex-wrap items-center gap-3 px-4 py-3 text-sm"
        >
          <span>Activity could not be refreshed.</span>
          <Button
            size="sm"
            variant="secondary"
            disabled={query.isRefetching}
            onClick={() => void query.refetch()}
          >
            {query.isRefetching ? "Retrying refresh…" : "Retry refresh"}
          </Button>
        </div>
      )}
      {query.isFetchNextPageError && (
        <div
          role="alert"
          aria-label="Older activity could not be loaded."
          className="flex flex-wrap items-center gap-3 px-4 py-3 text-sm"
        >
          <span>Older activity could not be loaded.</span>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void fetchOlder()}
          >
            Retry older activity
          </Button>
        </div>
      )}
      {query.hasNextPage && !query.isFetchNextPageError && (
        <div className="px-4 py-3">
          {query.isFetchingNextPage && (
            <span role="status" aria-live="polite" className="sr-only">
              Loading older activity…
            </span>
          )}
          <Button
            size="sm"
            variant="secondary"
            disabled={query.isFetchingNextPage}
            onClick={() => void fetchOlder()}
          >
            {query.isFetchingNextPage ? "Loading…" : "Load older activity"}
          </Button>
        </div>
      )}
    </div>
  );
}

export function OrganizationActivitySection({
  organizationId,
}: {
  organizationId: string;
}): JSX.Element {
  return (
    <AdminSection
      title="Activity"
      description="Enterprise trial lifecycle changes for this organization."
    >
      <OrganizationActivity organizationId={organizationId} />
    </AdminSection>
  );
}

function ActivityItem({ log }: { log: ActivityLog }): JSX.Element {
  const before = useMemo(
    () => safeSnapshot(log.before_snapshot),
    [log.before_snapshot],
  );
  const after = useMemo(
    () => safeSnapshot(log.after_snapshot),
    [log.after_snapshot],
  );
  const diffs = snapshotDiffs(before, after);
  const [showRaw, setShowRaw] = useState(false);
  return (
    <article
      className="space-y-2 px-4 py-3 text-sm"
      data-testid={`activity-${log.id}`}
    >
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <div className="min-w-0 break-words">
          <span className="font-medium">{actorLabel(log)}</span>{" "}
          <span>{activityPhrase(log)}</span>
        </div>
        <time
          className="text-xs text-muted-foreground"
          dateTime={log.created_at}
        >
          {formatDate(log.created_at)}
        </time>
      </div>
      {TRIAL_ACTIONS.has(log.action) && (
        <TrialFacts log={log} before={before} after={after} />
      )}
      <details className="text-xs text-muted-foreground">
        <summary className="cursor-pointer">Details</summary>
        <div className="mt-2 space-y-2">
          <code className="break-all">{log.action}</code>
          <dl className="grid gap-2 sm:grid-cols-3">
            <Detail label="Actor">{actorLabel(log)}</Detail>
            <Detail label="Subject">
              {nonempty(log.subject_display_name) ??
                log.subject_type ??
                "Organization"}
            </Detail>
            <Detail label="Acting surface">
              {nonempty(log.acting_surface) ?? "Not recorded"}
            </Detail>
          </dl>
          <SafeMetadata metadata={log.metadata} />
          {diffs.length > 0 &&
            (showRaw ? (
              <section aria-label="Raw diff" role="region">
                <button type="button" onClick={() => setShowRaw(false)}>
                  View structured diff
                </button>
                <div className="grid gap-2 sm:grid-cols-2">
                  <JsonDetail label="Before snapshot" value={before} />
                  <JsonDetail label="After snapshot" value={after} />
                </div>
              </section>
            ) : (
              <section aria-label="Changed fields">
                <dl className="grid gap-1 rounded-md bg-muted/40 px-3 py-2">
                  {diffs.map((diff) => (
                    <div
                      className="grid grid-cols-[minmax(8rem,auto)_1fr] gap-2"
                      key={diff.field}
                    >
                      <dt>{diff.label}</dt>
                      <dd className="break-all">
                        {displayValue(diff.before)} → {displayValue(diff.after)}
                      </dd>
                    </div>
                  ))}
                </dl>
                <button type="button" onClick={() => setShowRaw(true)}>
                  View raw diff
                </button>
              </section>
            ))}
        </div>
      </details>
    </article>
  );
}

function Detail({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <div>
      <dt>{label}</dt>
      <dd className="break-all text-foreground">{children}</dd>
    </div>
  );
}

function JsonDetail({
  label,
  value,
}: {
  label: string;
  value: unknown;
}): JSX.Element {
  return (
    <div>
      <p>{label}</p>
      <pre className="whitespace-pre-wrap break-all">{safeJson(value)}</pre>
    </div>
  );
}

function SafeMetadata({
  metadata,
}: {
  metadata?: Record<string, unknown>;
}): JSX.Element | null {
  if (!metadata) return null;
  const safe = pickDefined(metadata, [
    "conversion_source",
    "tier",
    "account_type",
    "previous_account_type",
    "extended_by_days",
    "key_access_changed",
    "trial_starts_at",
    "trial_ends_at",
    "previous_trial_ends_at",
  ]);
  return Object.keys(safe).length === 0 ? null : (
    <JsonDetail label="Metadata" value={safe} />
  );
}

function TrialFacts({
  log,
  before,
  after,
}: {
  log: ActivityLog;
  before?: SafeSnapshot;
  after?: SafeSnapshot;
}): JSX.Element {
  const facts: Array<[string, ReactNode]> = [];
  const beforeEnd = firstRecorded(
    before?.trial?.ends_at,
    before?.trial_ends_at,
    log.metadata?.previous_trial_ends_at,
  );
  const afterEnd = firstRecorded(
    after?.trial?.ends_at,
    after?.trial_ends_at,
    log.metadata?.trial_ends_at,
  );
  const tier = firstRecorded(
    after?.trial?.tier,
    before?.trial?.tier,
    log.metadata?.tier,
    log.metadata?.account_type,
    log.metadata?.previous_account_type,
  );
  const keyAccessChanged = firstRecorded(
    log.metadata?.key_access_changed,
    snapshotKeyAccessChanged(after),
    snapshotKeyAccessChanged(before),
  );
  let title = "Enterprise trial updated";
  switch (log.action) {
    case "organization:enterprise_trial_armed":
      title = "Enterprise trial started";
      facts.push(
        ["Started", formatFactDate(log.created_at)],
        ["Trial end", formatFactDate(afterEnd)],
        ["Tier", recorded(tier)],
      );
      break;
    case "organization:enterprise_trial_extended":
      title = "Enterprise trial extended";
      facts.push(
        ["Previous trial end", formatFactDate(beforeEnd)],
        ["New trial end", formatFactDate(afterEnd)],
        ["Tier", recorded(tier)],
      );
      break;
    case "organization:enterprise_trial_rearmed":
      title = "Enterprise trial restarted";
      facts.push(
        ["Restarted", formatFactDate(log.created_at)],
        ["Trial end", formatFactDate(afterEnd)],
        ["Tier", recorded(tier)],
      );
      break;
    case "organization:enterprise_trial_demoted":
      title = "Enterprise trial ended";
      facts.push(
        ["Ended", formatFactDate(after?.trial?.demoted_at ?? log.created_at)],
        ["Trial end", formatFactDate(afterEnd)],
        ["Tier", recorded(tier)],
      );
      break;
    case "organization:enterprise_trial_converted":
      title = "Enterprise trial converted";
      facts.push(
        [
          "Converted",
          formatFactDate(after?.trial?.converted_at ?? log.created_at),
        ],
        ["Conversion method", conversionMethod(log)],
        ["Tier", recorded(tier)],
      );
      break;
  }
  if (
    log.action === "organization:enterprise_trial_rearmed" ||
    log.action === "organization:enterprise_trial_demoted" ||
    log.action === "organization:enterprise_trial_converted"
  ) {
    facts.push(["Key access changed", yesNo(keyAccessChanged)]);
  }
  for (const key of allKeyTypes(before, after)) {
    const oldAccess = keyAccess(before, key);
    const newAccess = keyAccess(after, key);
    facts.push([
      `Key access (${key})`,
      oldAccess === newAccess || oldAccess === "Not recorded"
        ? newAccess
        : `${oldAccess} → ${newAccess}`,
    ]);
  }
  return (
    <section
      className="rounded-md bg-muted/40 px-3 py-2 text-xs"
      aria-label={title}
    >
      <h3 className="mb-1 font-medium text-foreground">{title}</h3>
      <dl className="grid gap-x-4 gap-y-1 sm:grid-cols-2">
        {facts.map(([label, value]) => (
          <div className="contents" key={label}>
            <dt>{label}</dt>
            <dd className="break-words text-foreground">{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

function activityPhrase(log: ActivityLog): string {
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
      return "updated organization";
  }
}

function conversionSource(log: ActivityLog): string | undefined {
  return typeof log.metadata?.conversion_source === "string"
    ? log.metadata.conversion_source
    : undefined;
}

function actorLabel(log: ActivityLog): string {
  if (
    log.action === "organization:enterprise_trial_converted" &&
    conversionSource(log) === "stripe_checkout"
  )
    return "Stripe";
  if (log.actor_id === "system" || log.actor_type === "system") return "System";
  const displayName = nonempty(log.actor_display_name);
  if (displayName) return displayName;
  return log.action === "organization:enterprise_trial_armed"
    ? "Unknown user"
    : "Speakeasy Team";
}

function snapshotDiffs(
  before: SafeSnapshot | undefined,
  after: SafeSnapshot | undefined,
): DiffRow[] {
  if (!before && !after) return [];
  const rows: DiffRow[] = [];
  addDiff(
    rows,
    "Account type",
    before?.organization?.account_type,
    after?.organization?.account_type,
  );
  addDiff(
    rows,
    "Whitelisted",
    before?.organization?.whitelisted,
    after?.organization?.whitelisted,
  );
  addDiff(
    rows,
    "Disabled",
    before?.organization?.disabled,
    after?.organization?.disabled,
  );
  addDiff(rows, "Trial status", before?.trial?.status, after?.trial?.status);
  addDiff(rows, "Trial tier", before?.trial?.tier, after?.trial?.tier);
  addDiff(
    rows,
    "Trial end",
    before?.trial?.ends_at ?? before?.trial_ends_at,
    after?.trial?.ends_at ?? after?.trial_ends_at,
  );
  addDiff(
    rows,
    "Converted at",
    before?.trial?.converted_at,
    after?.trial?.converted_at,
  );
  addDiff(
    rows,
    "Demoted at",
    before?.trial?.demoted_at,
    after?.trial?.demoted_at,
  );

  const keyTypes = new Set([
    ...(before?.keys ?? []).map((key) => key.key_type),
    ...(after?.keys ?? []).map((key) => key.key_type),
  ]);
  for (const keyType of [...keyTypes]
    .filter((value): value is string => typeof value === "string")
    .sort()) {
    const beforeKey = before?.keys?.find((key) => key.key_type === keyType);
    const afterKey = after?.keys?.find((key) => key.key_type === keyType);
    addDiff(
      rows,
      `Stored disabled (${keyType})`,
      beforeKey?.stored_disabled,
      afterKey?.stored_disabled,
    );
    addDiff(
      rows,
      `Effective disabled (${keyType})`,
      beforeKey?.effective_disabled,
      afterKey?.effective_disabled,
    );
    addDiff(
      rows,
      `Key access changed (${keyType})`,
      beforeKey?.key_access_changed,
      afterKey?.key_access_changed,
    );
    addDiff(
      rows,
      `Disable causes (${keyType})`,
      beforeKey?.disable_causes,
      afterKey?.disable_causes,
    );
    addDiff(
      rows,
      `Monthly cap (${keyType})`,
      beforeKey?.monthly_credits,
      afterKey?.monthly_credits,
    );
  }
  return rows;
}

function safeSnapshot(value: unknown): SafeSnapshot | undefined {
  if (!isRecord(value)) return undefined;
  const organization = isRecord(value.organization)
    ? pickDefined(value.organization, [
        "account_type",
        "whitelisted",
        "disabled",
      ])
    : undefined;
  const trial = isRecord(value.trial)
    ? pickDefined(value.trial, [
        "status",
        "tier",
        "ends_at",
        "converted_at",
        "demoted_at",
      ])
    : undefined;
  const keys = Array.isArray(value.keys)
    ? value.keys
        .filter(isRecord)
        .map((key) =>
          pickDefined(key, [
            "key_type",
            "disable_causes",
            "stored_disabled",
            "effective_disabled",
            "key_access_changed",
            "monthly_credits",
          ]),
        )
    : undefined;
  return {
    organization,
    trial,
    keys,
    ...(value.trial_ends_at !== undefined
      ? { trial_ends_at: safeValue(value.trial_ends_at) }
      : {}),
  };
}

function addDiff(
  rows: DiffRow[],
  label: string,
  before: unknown,
  after: unknown,
): void {
  if (before === undefined && after === undefined) return;
  if (safeJson(before) !== safeJson(after))
    rows.push({
      label,
      field: label.toLowerCase().replaceAll(" ", "_"),
      before,
      after,
    });
}

function displayValue(value: unknown): string {
  if (value === undefined) return "(missing)";
  return safeJson(value);
}
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function safeValue(value: unknown): unknown {
  if (
    value === null ||
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  )
    return value;
  if (Array.isArray(value))
    return value.map(safeValue).filter((item) => item !== undefined);
  return undefined;
}

function pickDefined(
  source: Record<string, unknown>,
  fields: string[],
): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const field of fields) {
    if (source[field] !== undefined) result[field] = safeValue(source[field]);
  }
  return result;
}

function safeJson(value: unknown): string {
  if (value === undefined) return "(missing)";
  try {
    return JSON.stringify(value) ?? "[unserializable]";
  } catch {
    return "[unserializable]";
  }
}

function nonempty(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== ""
    ? value.trim()
    : undefined;
}

function firstRecorded(...values: unknown[]): unknown {
  return values.find(
    (value) => value !== undefined && value !== null && value !== "",
  );
}

function snapshotKeyAccessChanged(
  snapshot?: SafeSnapshot,
): boolean | undefined {
  const values = (snapshot?.keys ?? [])
    .map((key) => key.key_access_changed)
    .filter((value): value is boolean => typeof value === "boolean");
  return values.length === 0 ? undefined : values.some(Boolean);
}

function yesNo(value: unknown): string {
  return typeof value === "boolean" ? (value ? "Yes" : "No") : "Not recorded";
}

function recorded(value: unknown): string {
  if (value === undefined || value === null || value === "")
    return "Not recorded";
  return typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
    ? String(value)
    : safeJson(value);
}

function formatFactDate(value: unknown): string {
  return typeof value === "string" ? formatDate(value) : "Not recorded";
}

function conversionMethod(log: ActivityLog): string {
  const source = conversionSource(log);
  if (!source) return "Not recorded";
  if (source === "stripe_checkout") return "Stripe checkout";
  if (source === "platform_admin" || source === "admin") return "Manual";
  return source;
}

function allKeyTypes(before?: SafeSnapshot, after?: SafeSnapshot): string[] {
  const values = [...(before?.keys ?? []), ...(after?.keys ?? [])]
    .map((key) => key.key_type)
    .filter((value): value is string => typeof value === "string");
  return [...new Set(values)];
}

function keyAccess(
  snapshot: SafeSnapshot | undefined,
  keyType: string,
): string {
  const key = snapshot?.keys?.find((item) => item.key_type === keyType);
  return typeof key?.effective_disabled === "boolean"
    ? key.effective_disabled
      ? "Disabled"
      : "Enabled"
    : "Not recorded";
}

function formatDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.valueOf())
    ? "Unknown time"
    : UTC_DATE_TIME.format(date);
}
