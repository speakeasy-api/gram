import * as React from "react";

import { cn } from "@/lib/utils";

/** @public — part of the component's prop API. */
export type StatusDotTone =
  | "success"
  | "warning"
  | "destructive"
  | "information"
  | "neutral";

/**
 * A status → presentation pairing for a single domain status value. Define a
 * `Record<YourDomainStatus, StatusPresentation>` next to the domain logic
 * that computes the status (not here) — see
 * `src/lib/user-session-status.ts`'s `STATUS_PRESENTATION` for the
 * established pattern — then spread the looked-up entry into `<StatusDot />`
 * at the call site:
 *
 * ```tsx
 * const STATUS_PRESENTATION: Record<SessionStatus, StatusPresentation> = {
 *   active: { label: "Active", tone: "success" },
 *   expired: { label: "Expired", tone: "neutral" },
 *   revoked: { label: "Revoked", tone: "destructive" },
 * };
 *
 * <StatusDot {...STATUS_PRESENTATION[status]} />
 * ```
 *
 * Keeping the map colocated with the domain (rather than growing a shared
 * "all statuses" map here) keeps each domain's status vocabulary and tone
 * choice in exactly one file.
 */
/** @public — the per-domain status map convention. */
export interface StatusPresentation {
  label: string;
  tone: StatusDotTone;
}

const toneDotClass: Record<StatusDotTone, string> = {
  neutral: "bg-muted-foreground",
  success: "bg-success-default",
  warning: "bg-warning-default",
  destructive: "bg-destructive-default",
  information: "bg-information-default",
};

const sizeDotClass: Record<"sm" | "md", string> = {
  sm: "size-1.5",
  md: "size-2",
};

export interface StatusDotProps {
  tone: StatusDotTone;
  /** Animates the dot with `animate-pulse` — use for "in progress" states. */
  pulse?: boolean;
  label?: React.ReactNode;
  size?: "sm" | "md";
  className?: string;
}

/** Square status indicator dot (matches the Badge dot), with an optional label. */
export function StatusDot({
  tone,
  pulse = false,
  label,
  size = "md",
  className,
}: StatusDotProps): React.JSX.Element {
  return (
    <span className={cn("inline-flex items-center gap-1.5", className)}>
      <span
        aria-hidden="true"
        className={cn(
          "shrink-0",
          sizeDotClass[size],
          toneDotClass[tone],
          pulse && "animate-pulse",
        )}
      />
      {label !== undefined && (
        <span className="text-foreground font-sans text-sm">{label}</span>
      )}
    </span>
  );
}
