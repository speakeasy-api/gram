"use client";

import * as React from "react";
import { Progress as ProgressPrimitive } from "radix-ui";

import { cn } from "@/lib/utils";

interface ProgressProps extends Omit<
  React.ComponentProps<typeof ProgressPrimitive.Root>,
  "max"
> {
  /** Label to display above the progress bar (matches LLM prompt) */
  label?: string;
  /** Maximum value (matches LLM prompt) */
  max?: number;
}

function Progress({
  className,
  value,
  label,
  max = 100,
  ...props
}: ProgressProps) {
  const percentage =
    value != null && max > 0
      ? Math.min(100, Math.max(0, (value / max) * 100))
      : 0;

  return (
    <div className={cn("w-full space-y-2", className)}>
      {label && (
        <div className="flex justify-between text-sm">
          <span>{label}</span>
          <span className="text-muted-foreground">
            {percentage.toFixed(0)}%
          </span>
        </div>
      )}
      <ProgressPrimitive.Root
        data-slot="progress"
        className="relative h-2 w-full overflow-hidden rounded-full bg-primary/20"
        value={value}
        max={max}
        {...props}
      >
        <ProgressPrimitive.Indicator
          data-slot="progress-indicator"
          className="h-full w-full flex-1 bg-primary transition-all"
          style={{ transform: `translateX(-${100 - percentage}%)` }}
        />
      </ProgressPrimitive.Root>
    </div>
  );
}

export { Progress };
