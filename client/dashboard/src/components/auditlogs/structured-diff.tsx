import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import { computeChangedFields } from "@/lib/compute-changed-fields";
import { useMemo, useState, Suspense } from "react";
import React from "react";
import { LoaderCircle } from "lucide-react";
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
    <div className="border-border/50 flex items-start gap-3 border-b px-3 py-2 last:border-b-0">
      <span className="text-muted-foreground w-[140px] shrink-0 pt-0.5 font-mono text-xs font-medium">
        {field}
      </span>
      <div className="flex min-w-0 flex-1 flex-wrap items-start gap-2">
        <span className="bg-destructive-softest text-destructive max-w-full px-2 py-0.5 font-mono text-xs break-all line-through">
          {formatValue(oldValue)}
        </span>
        <span className="text-muted-foreground pt-0.5 text-xs">→</span>
        <span className="bg-success-softest text-success max-w-full px-2 py-0.5 font-mono text-xs break-all">
          {formatValue(newValue)}
        </span>
      </div>
    </div>
  );
}

export function StructuredDiff({ log }: { log: AuditLog }): React.JSX.Element {
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
          className="text-link-primary mb-2 text-xs hover:underline"
        >
          View structured diff
        </button>
        <HighlightProvider>
          <Suspense
            fallback={
              <div className="text-muted-foreground flex items-center gap-2">
                <LoaderCircle className="size-4 animate-spin" />
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
        <span className="text-muted-foreground text-[11px] font-semibold tracking-wide uppercase">
          Changed fields
        </span>
        <div className="bg-border h-px flex-1" />
        <span className="text-muted-foreground text-[11px]">
          {changes.length} field{changes.length === 1 ? "" : "s"} changed
        </span>
      </div>
      <div className="bg-background border">
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
        className="text-link-primary mt-2 text-xs hover:underline"
      >
        View raw diff
      </button>
    </div>
  );
}
