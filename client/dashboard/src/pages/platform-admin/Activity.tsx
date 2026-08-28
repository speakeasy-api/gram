import { Button } from "@/components/ui/Button";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";
import { AdminSection } from "./AdminSection";

const TRIAL_ACTION_PREFIX = "organization:enterprise_trial_";

type ActivityLog = {
  id: string;
  action: string;
  actor_display_name?: string;
  actor_id: string;
  actor_type: string;
  created_at: string;
  metadata?: Record<string, unknown>;
  before_snapshot?: unknown;
  after_snapshot?: unknown;
};

type ActivityPage = { logs: ActivityLog[]; next_cursor?: string };

type SafeSnapshot = {
  organization?: {
    account_type?: string;
    whitelisted?: boolean;
    disabled?: boolean;
  };
  trial?: {
    tier?: string;
    ends_at?: string | null;
    converted_at?: string | null;
    demoted_at?: string | null;
  };
  keys?: Array<{
    key_type?: string;
    disable_causes?: string[];
    stored_disabled?: boolean;
    effective_disabled?: boolean;
    monthly_credits?: number;
  }>;
  trial_ends_at?: string;
};

type DiffRow = { label: string; before: string; after: string };

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
      if (log.action.startsWith(TRIAL_ACTION_PREFIX) && !byID.has(log.id))
        byID.set(log.id, log);
    }
  }
  const logs = [...byID.values()].sort((left, right) => {
    const byTime = Date.parse(right.created_at) - Date.parse(left.created_at);
    return byTime === 0 ? right.id.localeCompare(left.id) : byTime;
  });

  if (logs.length === 0 && !query.hasNextPage)
    return (
      <p className="px-4 py-3 text-sm text-muted-foreground">
        No activity yet.
      </p>
    );

  return (
    <div className="divide-y">
      {logs.map((log) => (
        <ActivityItem key={log.id} log={log} />
      ))}
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
  const diffs = snapshotDiffs(log.before_snapshot, log.after_snapshot);
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
      {diffs.length > 0 && (
        <dl className="grid grid-cols-1 gap-x-4 gap-y-1 rounded-md bg-muted/40 px-3 py-2 text-xs sm:grid-cols-[minmax(8rem,auto)_1fr]">
          {diffs.map((diff) => (
            <div className="contents" key={diff.label}>
              <dt className="text-muted-foreground">{diff.label}</dt>
              <dd className="min-w-0 break-words">
                {diff.before} → {diff.after}
              </dd>
            </div>
          ))}
        </dl>
      )}
      <details className="text-xs text-muted-foreground">
        <summary className="cursor-pointer">Details</summary>
        <code className="break-all">{log.action}</code>
      </details>
    </article>
  );
}

function activityPhrase(log: ActivityLog): string {
  switch (log.action) {
    case "organization:enterprise_trial_armed":
      return "armed enterprise trial";
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
      return "updated enterprise trial";
  }
}

function conversionSource(log: ActivityLog): string | undefined {
  return typeof log.metadata?.conversion_source === "string"
    ? log.metadata.conversion_source
    : undefined;
}

function actorLabel(log: ActivityLog): string {
  if (log.action === "organization:enterprise_trial_converted") {
    return conversionSource(log) === "stripe_checkout"
      ? "System"
      : "Platform administrator";
  }
  if (
    log.actor_id === "system" ||
    log.actor_type === "system" ||
    log.actor_display_name === "System"
  )
    return "System";
  return "Platform administrator";
}

function snapshotDiffs(beforeValue: unknown, afterValue: unknown): DiffRow[] {
  const before = safeSnapshot(beforeValue);
  const after = safeSnapshot(afterValue);
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
    ? {
        account_type: stringValue(value.organization.account_type),
        whitelisted: booleanValue(value.organization.whitelisted),
        disabled: booleanValue(value.organization.disabled),
      }
    : undefined;
  const trial = isRecord(value.trial)
    ? {
        tier: stringValue(value.trial.tier),
        ends_at: nullableString(value.trial.ends_at),
        converted_at: nullableString(value.trial.converted_at),
        demoted_at: nullableString(value.trial.demoted_at),
      }
    : undefined;
  const keys = Array.isArray(value.keys)
    ? value.keys.filter(isRecord).map((key) => ({
        key_type: stringValue(key.key_type),
        disable_causes: Array.isArray(key.disable_causes)
          ? key.disable_causes.filter(
              (cause): cause is string => typeof cause === "string",
            )
          : undefined,
        stored_disabled: booleanValue(key.stored_disabled),
        effective_disabled: booleanValue(key.effective_disabled),
        monthly_credits:
          typeof key.monthly_credits === "number"
            ? key.monthly_credits
            : undefined,
      }))
    : undefined;
  return {
    organization,
    trial,
    keys,
    trial_ends_at: stringValue(value.trial_ends_at),
  };
}

function addDiff(
  rows: DiffRow[],
  label: string,
  before: unknown,
  after: unknown,
): void {
  if (before === undefined && after === undefined) return;
  const beforeText = displayValue(before);
  const afterText = displayValue(after);
  if (beforeText !== afterText)
    rows.push({ label, before: beforeText, after: afterText });
}

function displayValue(value: unknown): string {
  if (value === undefined || value === null) return "—";
  if (Array.isArray(value))
    return value.length === 0 ? "none" : value.join(", ");
  if (
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  )
    return String(value);
  return "—";
}
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
function stringValue(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}
function nullableString(value: unknown): string | null | undefined {
  return value === null ? null : stringValue(value);
}
function booleanValue(value: unknown): boolean | undefined {
  return typeof value === "boolean" ? value : undefined;
}
function formatDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? "Unknown time" : date.toLocaleString();
}
