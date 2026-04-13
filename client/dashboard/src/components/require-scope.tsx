import { Scope } from "@/hooks/useRBAC";
import { useRBAC } from "@/hooks/useRBAC";
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
      <div className="opacity-50 pointer-events-none select-none">
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
      return <ScopeDisabled reason={props.reason}>{children}</ScopeDisabled>;
  }
}

/**
 * Wraps children in a visually-disabled state with a tooltip explaining why.
 */
function ScopeDisabled({
  reason = "You don't have permission to perform this action.",
  children,
}: {
  reason?: string;
  children: React.ReactNode;
}) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="opacity-50 pointer-events-none select-none cursor-not-allowed inline-flex">
            {/* Wrapper div that re-enables pointer events for the tooltip to work */}
            <div
              className="pointer-events-auto"
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
    <div className="flex items-center justify-center h-full w-full min-h-[400px]">
      <div className="flex flex-col items-center gap-3 max-w-sm text-center">
        <div className="flex items-center justify-center w-12 h-12 rounded-full bg-muted">
          <Icon name="lock" className="h-5 w-5 text-muted-foreground" />
        </div>
        <h2 className="text-lg font-medium">{title}</h2>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
    </div>
  );
}
