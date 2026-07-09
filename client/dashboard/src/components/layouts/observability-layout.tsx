import * as React from "react";

import { cn } from "@/lib/utils";
import { Layout } from "./layout";

/**
 * The layout for pages that answer "what is happening": insights, costs,
 * security overview, project dashboard. A header band whose actions hold
 * the time-range control, a row of stat tiles, an optional full-width
 * distribution strip, then charts and ranked lists.
 *
 *   <ObservabilityLayout>
 *     <ObservabilityLayout.Header
 *       title="Watchdog"
 *       subtitle="Your riskiest AI usage, clustered and ranked."
 *       actions={<><TimeRangePicker/><Button>Export report</Button></>}
 *     />
 *     <ObservabilityLayout.Stats>{tiles}</ObservabilityLayout.Stats>
 *     <ObservabilityLayout.Strip label="Exposure by data type">…</…>
 *     <ObservabilityLayout.Section title="Active signals" annotation="8 of 8">
 *       …
 *     </ObservabilityLayout.Section>
 *   </ObservabilityLayout>
 */
function ObservabilityLayoutRoot({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return <Layout className={className}>{children}</Layout>;
}

/**
 * The KPI row. Tiles sit in a hairline-divided band — one border around
 * the row, dividers between the cells, so the row reads as one object.
 */
function ObservabilityLayoutStats({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Layout.Body className={cn("pt-6", className)}>
      <div className="border-neutral-softest grid grid-cols-1 border sm:grid-cols-2 lg:grid-cols-4 [&>*]:p-6 [&>*:not(:first-child)]:border-t [&>*:not(:first-child)]:border-neutral-softest sm:[&>*:nth-child(-n+2)]:border-t-0 sm:[&>*:nth-child(even)]:border-l sm:[&>*:nth-child(even)]:border-neutral-softest lg:[&>*]:border-t-0 lg:[&>*:not(:first-child)]:border-l lg:[&>*:not(:first-child)]:border-neutral-softest">
        {children}
      </div>
    </Layout.Body>
  );
}

/**
 * A full-width labeled strip: a stacked distribution bar plus its mono
 * legend. The one place a saturated palette spans the page.
 */
function ObservabilityLayoutStrip({
  label,
  annotation,
  children,
  className,
}: {
  label?: React.ReactNode;
  annotation?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("mt-6 flex flex-col gap-3", className)}>
      {(label || annotation) && (
        <div className="flex items-baseline justify-between gap-4">
          {label ? <Layout.Eyebrow>{label}</Layout.Eyebrow> : null}
          {annotation ? (
            <span className="text-muted font-mono text-xs">{annotation}</span>
          ) : null}
        </div>
      )}
      {children}
    </div>
  );
}

/** A responsive grid for chart cards. */
function ObservabilityLayoutGrid({
  children,
  className,
  columns = 2,
}: {
  children: React.ReactNode;
  className?: string;
  columns?: 1 | 2 | 3;
}) {
  const columnClass = {
    1: "grid-cols-1",
    2: "grid-cols-1 lg:grid-cols-2",
    3: "grid-cols-1 md:grid-cols-2 xl:grid-cols-3",
  }[columns];

  return (
    <div className={cn("grid gap-6", columnClass, className)}>{children}</div>
  );
}

function ObservabilityLayoutSection({
  className,
  ...props
}: React.ComponentProps<typeof Layout.Section>) {
  return <Layout.Section className={cn("mt-10", className)} {...props} />;
}

ObservabilityLayoutRoot.Header = Layout.Header;
ObservabilityLayoutRoot.Stats = ObservabilityLayoutStats;
ObservabilityLayoutRoot.Strip = ObservabilityLayoutStrip;
ObservabilityLayoutRoot.Grid = ObservabilityLayoutGrid;
ObservabilityLayoutRoot.Section = ObservabilityLayoutSection;
ObservabilityLayoutRoot.Actions = Layout.Actions;

export { ObservabilityLayoutRoot as ObservabilityLayout };
