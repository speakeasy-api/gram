import { useRBAC } from "@/hooks/useRBAC";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import React from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";

type RenderFn = (props: { disabled: boolean }) => React.ReactNode;

type RequireScopeProps = {
  scope: Scope | Scope[];
  /** When true, ALL scopes must be present. Default: false (any scope suffices). */
  all?: boolean;
  /** Optional resource ID to check scope against. */
  resourceId?: string;
  /**
   * Either a React node or a render function receiving `{ disabled }`.
   * Use the render function form when children contain portals (e.g. dropdowns,
   * dialogs) that escape CSS containment and need to receive disabled state directly.
   */
  children: React.ReactNode | RenderFn;
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

export function RequireScope(
  props: RequireScopeProps,
): React.JSX.Element | null {
  const { scope, all = false, resourceId, children, level } = props;
  const { hasAllScopes, hasAnyScope, isLoading } = useRBAC();

  const scopes = Array.isArray(scope) ? scope : [scope];
  const allowed = all
    ? hasAllScopes(scopes, resourceId)
    : hasAnyScope(scopes, resourceId);

  const resolveChildren = (disabled: boolean): React.ReactNode =>
    typeof children === "function" ? children({ disabled }) : children;

  // While grants are loading, render nothing to avoid flash of unauthorized
  if (isLoading) {
    if (level === "page") return null;
    if (level === "section") return null;
    // For component-level, show disabled state while loading
    return (
      <div className="pointer-events-none opacity-50 select-none">
        {resolveChildren(true)}
      </div>
    );
  }

  if (allowed) {
    return <>{resolveChildren(false)}</>;
  }

  switch (level) {
    case "page":
      return <>{props.fallback ?? <Unauthorized />}</>;

    case "section":
      return <>{props.fallback ?? null}</>;

    case "component":
      return (
        <ScopeDisabled
          reason={props.reason}
          className={props.className}
          scopes={scopes}
          all={all}
        >
          {resolveChildren(true)}
        </ScopeDisabled>
      );
  }
}

/**
 * Build the "required scope" hint shown alongside the tooltip reason so the
 * user knows exactly which grant they're missing (and can request it).
 */
function requiredScopeLabel(scopes: Scope[], all: boolean): string {
  if (scopes.length === 0) return "";
  if (scopes.length === 1) return `Requires the ${scopes[0]} scope.`;
  const joined = scopes.join(", ");
  return all
    ? `Requires all of these scopes: ${joined}.`
    : `Requires one of these scopes: ${joined}.`;
}

/**
 * Wraps children in a visually-disabled state with a tooltip explaining why.
 */
function ScopeDisabled({
  reason = "You don't have permission to perform this action.",
  className,
  scopes,
  all,
  children,
}: {
  reason?: string;
  className?: string;
  scopes: Scope[];
  all: boolean;
  children: React.ReactNode;
}) {
  const scopeLabel = requiredScopeLabel(scopes, all);
  return (
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
            className="pointer-events-auto w-full cursor-not-allowed **:cursor-not-allowed"
            onClickCapture={(e) => {
              e.preventDefault();
              e.stopPropagation();
            }}
          >
            {children}
          </div>
        </div>
      </TooltipTrigger>
      <TooltipContent>
        <span>{reason}</span>
        {scopeLabel && (
          <span className="mt-1 block font-mono opacity-80">{scopeLabel}</span>
        )}
      </TooltipContent>
    </Tooltip>
  );
}

/**
 * Full-page unauthorized state. Used as the default fallback for page-level RequireScope.
 */
function Unauthorized({
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
