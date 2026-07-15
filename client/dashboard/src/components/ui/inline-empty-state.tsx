import * as React from "react";

import { cn } from "@/lib/utils";

export type InlineEmptyStateSize = "default" | "lg";

export interface InlineEmptyStateProps {
  /** Rendered inside a small muted square above the title. */
  icon?: React.ReactNode;
  title: React.ReactNode;
  description?: React.ReactNode;
  /** Slot for a retry/create button below the description. */
  action?: React.ReactNode;
  /**
   * `lg` fills a page or a tab body; `default` sits inside a card, panel, or
   * table body.
   */
  size?: InlineEmptyStateSize;
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
  size = "default",
  className,
}: InlineEmptyStateProps): React.JSX.Element {
  const large = size === "lg";

  return (
    <div
      className={cn(
        "border-neutral-softest flex flex-col items-center gap-3 border border-dashed text-center",
        large ? "px-8 py-16" : "px-6 py-10",
        className,
      )}
    >
      {icon && (
        <div
          className={cn(
            "bg-muted text-muted-foreground flex shrink-0 items-center justify-center",
            large ? "size-12 [&_svg]:size-5" : "size-10 [&_svg]:size-4",
          )}
        >
          {icon}
        </div>
      )}
      <div className="flex flex-col gap-1">
        <p
          className={cn(
            "font-sans font-medium",
            large ? "text-base" : "text-sm",
          )}
        >
          {title}
        </p>
        {description && (
          <p
            className={cn(
              "text-muted-foreground font-sans text-sm",
              large && "max-w-md",
            )}
          >
            {description}
          </p>
        )}
      </div>
      {action && <div className="mt-1">{action}</div>}
    </div>
  );
}
