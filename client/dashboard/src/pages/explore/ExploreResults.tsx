import { InlineEmptyState } from "@/components/inline-empty-state";
import type { JSX } from "react";

/**
 * The results panel under the builder. One generic empty state covers every
 * reason there is nothing to draw; the chart and summary table land here
 * once queries run.
 */
export function ExploreResults({ dataset }: { dataset: string }): JSX.Element {
  return (
    <section className="border-border bg-card flex flex-col gap-4 border p-5">
      <div className="flex items-center justify-between gap-4">
        <span className="text-eyebrow">Results</span>
        <span className="text-muted-foreground font-mono text-xs">
          {dataset}
        </span>
      </div>
      <InlineEmptyState
        icon="telescope"
        heading="No rows to show"
        description="Results appear here once the query runs."
      />
    </section>
  );
}
