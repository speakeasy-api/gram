"use client";

import { cn } from "@/lib/utils";

export interface SwitchProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  "aria-label"?: string;
  "aria-labelledby"?: string;
  className?: string;
}

export function Switch({
  checked,
  onCheckedChange,
  disabled = false,
  "aria-label": ariaLabel,
  "aria-labelledby": ariaLabelledBy,
  className,
}: SwitchProps) {
  return (
    <button
      onClick={() => onCheckedChange(!checked)}
      disabled={disabled}
      aria-label={ariaLabel}
      aria-labelledby={ariaLabelledBy}
      role="switch"
      aria-checked={checked}
      className={cn(
        "relative inline-flex h-5 w-9 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50",
        checked ? "bg-primary" : "bg-input",
        className
      )}
    >
      <span
        className={cn(
          "pointer-events-none block h-4 w-4 rounded-full bg-background shadow-lg ring-0 transition-transform",
          checked ? "translate-x-4" : "translate-x-0.5"
        )}
      />
    </button>
  );
}