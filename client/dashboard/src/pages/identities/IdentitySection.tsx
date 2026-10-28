import * as React from "react";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";

/**
 * The window the identity panels read (useIdentityWindow), offered on the
 * sections whose content it scopes rather than in the page header.
 */
export function IdentityTimeRangePicker(): React.JSX.Element {
  const {
    dateRange,
    customRange,
    customRangeLabel,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  } = useDateRangeFilter();
  return (
    <TimeRangePicker
      preset={customRange ? null : dateRange}
      customRange={customRange}
      customRangeLabel={customRangeLabel}
      onPresetChange={setDateRangeParam}
      onCustomRangeChange={setCustomRangeParam}
      onClearCustomRange={clearCustomRange}
    />
  );
}

/**
 * A sub-page's own header: the section name, and one line saying what the
 * slice is. Lighter than the page title — the person named in the page header
 * above is the subject, and the rail already says which section is open.
 */
export function IdentitySection({
  title,
  meta,
  action,
  timeRange = false,
  children,
}: {
  title?: string;
  /** e.g. "7 findings · 3 challenges · 11 blocks". */
  meta?: React.ReactNode;
  /** A control scoping everything below it, e.g. the account-type filter. */
  action?: React.ReactNode;
  /** Offer the time range picker: set where the section's content is windowed. */
  timeRange?: boolean;
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <section className="flex flex-col gap-6">
      {(title || meta || action || timeRange) && (
        <div className="border-border flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b pb-3">
          {title && <h2 className="text-display-xs font-thin">{title}</h2>}
          <div className="ml-auto flex items-center gap-4">
            {meta && (
              <span className="text-muted-foreground font-mono text-xs">
                {meta}
              </span>
            )}
            {action}
            {timeRange && <IdentityTimeRangePicker />}
          </div>
        </div>
      )}
      {children}
    </section>
  );
}
