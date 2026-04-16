import { Scope } from "@/hooks/useRBAC";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import React from "react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "./ui/tooltip";

type RequireScopeProps = {
  scope: Scope | Scope[];
  /** When true, ALL scopes must be present. Default: false (any scope suffices). */
  all?: boolean;
  /** Optional resource ID to check scope against. */
  resourceId?: string;
  children: React.ReactNode;
} & (
  | {
      /**
       * "page" — renders the Unauthorized full-page fallback.
       * "section" — hides the children entirely.
       * "component" — disables children with a tooltip (wraps in a div with pointer-events-none and reduced opacity).
       */
      level: "page";
      fallback?: React.ReactNode;
    }
  | {
      level: "section";
      fallback?: React.ReactNode;
    }
  | {
      level: "component";
      /** Tooltip text shown on hover when disabled. */
      reason?: string;
      /** Extra classes applied to the disabled wrapper div (e.g. "w-full" for block-level children). */
      className?: string;
    }
);

export function RequireScope(props: RequireScopeProps) {
  const { scope, all = false, resourceId, children, level } = props;
  const { hasAllScopes, hasAnyScope, isLoading } = useRBAC();

  const scopes = Array.isArray(scope) ? scope : [scope];
  const allowed = all
    ? hasAllScopes(scopes, resourceId)
    : hasAnyScope(scopes, resourceId);

  // While grants are loading, render nothing to avoid flash of unauthorized
  if (isLoading) {
    if (level === "page") return null;
    if (level === "section") return null;
    // For component-level, show disabled state while loading
    return (
      <div className="pointer-events-none opacity-50 select-none">
        {children}
      </div>
    );
  }

  if (allowed) {
    return <>{children}</>;
  }

  switch (level) {
    case "page":
      return <>{props.fallback ?? <Unauthorized />}</>;

    case "section":
      return <>{props.fallback ?? null}</>;

    case "component":
      return (
        <ScopeDisabled reason={props.reason} className={props.className}>
          {children}
        </ScopeDisabled>
      );
  }
}

/**
 * Wraps children in a visually-disabled state with a tooltip explaining why.
 */
function ScopeDisabled({
  reason = "You don't have permission to perform this action.",
  className,
  children,
}: {
  reason?: string;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <div
            className={cn(
              "pointer-events-none inline-flex opacity-50 select-none",
              className,
            )}
          >
            {/* Wrapper div that re-enables pointer events for the tooltip to work */}
            <div
              className="pointer-events-auto w-full cursor-not-allowed [&_*]:cursor-not-allowed"
              onClickCapture={(e) => {
                e.preventDefault();
                e.stopPropagation();
              }}
            >
              {children}
            </div>
          </div>
        </TooltipTrigger>
        <TooltipContent>{reason}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

/**
 * Full-page unauthorized state. Used as the default fallback for page-level RequireScope.
 */
export function Unauthorized({
  title = "Access restricted",
  description = "You don't have permission to view this page. Contact your organization admin to request access.",
}: {
  title?: string;
  description?: string;
}) {
  return (
    <div className="flex h-full min-h-[400px] w-full items-center justify-center">
      <div className="flex max-w-sm flex-col items-center gap-3 text-center">
        <div className="bg-muted flex h-12 w-12 items-center justify-center rounded-full">
          <Icon name="lock" className="text-muted-foreground h-5 w-5" />
        </div>
        <h2 className="text-lg font-medium">{title}</h2>
        <p className="text-muted-foreground text-sm">{description}</p>
      </div>
    </div>
  );
}
