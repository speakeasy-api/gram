import * as React from "react";

import { cn } from "@/lib/utils";

export interface InlineEmptyStateProps {
  /** Rendered inside a small muted square above the title. */
  icon?: React.ReactNode;
  title: React.ReactNode;
  description?: React.ReactNode;
  /** Slot for a retry/create button below the description. */
  action?: React.ReactNode;
  className?: string;
}

/**
 * Mid-weight empty state for embedding inside a card, panel, or table body —
 * lighter than a full-page empty state, heavier than a bare "No results"
 * string. Hairline dashed border, no shadow, squared corners.
 */
export function InlineEmptyState({
  icon,
  title,
  description,
  action,
  className,
}: InlineEmptyStateProps): React.JSX.Element {
  return (
    <div
      className={cn(
        "border-neutral-softest flex flex-col items-center gap-3 border border-dashed px-6 py-10 text-center",
        className,
      )}
    >
      {icon && (
        <div className="bg-muted text-muted-foreground flex size-10 shrink-0 items-center justify-center [&_svg]:size-4">
          {icon}
        </div>
      )}
      <div className="flex flex-col gap-1">
        <p className="font-sans text-sm font-medium">{title}</p>
        {description && (
          <p className="text-muted-foreground font-sans text-sm">
            {description}
          </p>
        )}
      </div>
      {action && <div className="mt-1">{action}</div>}
    </div>
  );
}
