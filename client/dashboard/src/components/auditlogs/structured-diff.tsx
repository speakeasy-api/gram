import type { AuditLog } from "@gram/client/models/components";
import { computeChangedFields } from "@/lib/compute-changed-fields";
import { useMemo, useState, Suspense } from "react";
import React from "react";
import { Icon } from "@speakeasy-api/moonshine";
import { HighlightProvider } from "@/components/diffs/provider";

const StaticDiff = React.lazy(() =>
  import("@/components/auditlogs/diff").then((mod) => ({
    default: mod.StaticDiff,
  })),
);

function formatValue(value: unknown): string {
  if (value === undefined) return "(none)";
  if (value === null) return "null";
  if (typeof value === "string") return value;
  if (typeof value === "boolean") return String(value);
  if (typeof value === "number") return String(value);
  return JSON.stringify(value);
}

function ChangedFieldRow({
  field,
  oldValue,
  newValue,
}: {
  field: string;
  oldValue: unknown;
  newValue: unknown;
}) {
  return (
    <div className="flex items-center gap-3 border-b border-border/50 px-3 py-2 last:border-b-0">
      <span className="w-[140px] shrink-0 font-mono text-xs font-medium text-muted-foreground">
        {field}
      </span>
      <div className="flex items-center gap-2">
        <span className="rounded bg-red-50 px-2 py-0.5 font-mono text-xs text-red-700 line-through dark:bg-red-950 dark:text-red-400">
          {formatValue(oldValue)}
        </span>
        <span className="text-xs text-muted-foreground">→</span>
        <span className="rounded bg-emerald-50 px-2 py-0.5 font-mono text-xs text-emerald-700 dark:bg-emerald-950 dark:text-emerald-400">
          {formatValue(newValue)}
        </span>
      </div>
    </div>
  );
}

export function StructuredDiff({ log }: { log: AuditLog }) {
  const [showRawDiff, setShowRawDiff] = useState(false);

  const changes = useMemo(
    () => computeChangedFields(log.beforeSnapshot, log.afterSnapshot),
    [log.beforeSnapshot, log.afterSnapshot],
  );

  if (showRawDiff) {
    return (
      <div className="mt-2">
        <button
          type="button"
          onClick={() => setShowRawDiff(false)}
          className="mb-2 text-xs text-blue-500 hover:underline"
        >
          View structured diff
        </button>
        <HighlightProvider>
          <Suspense
            fallback={
              <div className="flex items-center gap-2 text-muted-foreground">
                <Icon name="loader-circle" className="size-4 animate-spin" />
                <span>Loading diff...</span>
              </div>
            }
          >
            <StaticDiff log={log} />
          </Suspense>
        </HighlightProvider>
      </div>
    );
  }

  return (
    <div className="mt-2">
      <div className="flex items-center gap-2 py-1">
        <span className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
          Changed fields
        </span>
        <div className="h-px flex-1 bg-border" />
        <span className="text-[11px] text-muted-foreground">
          {changes.length} field{changes.length === 1 ? "" : "s"} changed
        </span>
      </div>
      <div className="rounded-md border bg-background">
        {changes.map((change) => (
          <ChangedFieldRow
            key={change.field}
            field={change.field}
            oldValue={change.oldValue}
            newValue={change.newValue}
          />
        ))}
      </div>
      <button
        type="button"
        onClick={() => setShowRawDiff(true)}
        className="mt-2 text-xs text-blue-500 hover:underline"
      >
        View raw diff
      </button>
    </div>
  );
}
