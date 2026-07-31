import { useRBAC } from "@/hooks/useRBAC";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { cn } from "@/lib/utils";
import { Icon } from "@/components/ui/Icon";
import React from "react";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/Tooltip";

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
      return (
        <>{props.fallback ?? <Unauthorized scopes={scopes} all={all} />}</>
      );

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
  scopes,
  all,
}: {
  title?: string;
  description?: string;
  scopes: Scope[];
  all: boolean;
}) {
  const [open, setOpen] = React.useState(false);

  return (
    <div className="flex h-full min-h-[400px] w-full items-center justify-center">
      <div className="flex max-w-sm flex-col items-center gap-3 text-center">
        <div className="bg-muted flex h-12 w-12 items-center justify-center rounded-full">
          <Icon name="lock" className="text-muted-foreground h-5 w-5" />
        </div>
        <h2 className="text-lg font-medium">{title}</h2>
        <p className="text-muted-foreground text-sm">{description}</p>
        {scopes.length > 0 && (
          /* The wrapper keeps the collapsed height (h-7 == trigger height) in
             flow while the card is absolutely positioned on top of it, so
             expanding grows downward instead of re-centring the whole block. */
          <div className="relative h-7 w-full">
            <Collapsible
              open={open}
              onOpenChange={setOpen}
              className="absolute inset-x-0 top-0"
            >
              <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex w-full cursor-pointer items-center justify-center gap-1 px-3 py-1.5 text-xs transition-colors">
                <Icon
                  name="chevron-right"
                  className={cn(
                    "size-3.5 transition-transform",
                    open && "rotate-90",
                  )}
                />
                What access do I need?
              </CollapsibleTrigger>
              <CollapsibleContent className="pt-2">
                <div className="bg-muted/25 rounded-lg border px-3 py-5">
                  <p className="text-muted-foreground text-center text-xs">
                    {scopes.length === 1 || !all
                      ? "Your account is missing a permission."
                      : "Your account is missing permissions."}
                  </p>
                  <p className="text-muted-foreground mt-1 text-center text-xs">
                    {scopes.length === 1
                      ? "An organization admin can grant you:"
                      : all
                        ? "An organization admin can grant you all of:"
                        : "An organization admin can grant you any of:"}
                  </p>
                  <ul className="mt-2 flex flex-wrap justify-center gap-1.5">
                    {scopes.map((s) => (
                      <li
                        key={s}
                        className="bg-background text-foreground rounded border px-1.5 py-0.5 font-mono text-xs"
                      >
                        {s}
                      </li>
                    ))}
                  </ul>
                </div>
              </CollapsibleContent>
            </Collapsible>
          </div>
        )}
      </div>
    </div>
  );
}
