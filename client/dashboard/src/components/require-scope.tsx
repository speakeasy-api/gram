import { useRBAC } from "@/hooks/useRBAC";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { RequestAccessFormScope } from "@gram/client/models/components/requestaccessform.js";
import { useRequestAccessMutation } from "@gram/client/react-query/requestAccess.js";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/Button";
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
        <>
          {props.fallback ?? (
            <Unauthorized scopes={scopes} all={all} resourceId={resourceId} />
          )}
        </>
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
 * Returns the set of scopes that can be requested via the requestAccess API.
 * Filters out "blocked" scopes since those cannot be requested.
 */
function getRequestableScopes(scopes: Scope[]): RequestAccessFormScope[] {
  const validScopes = Object.values(RequestAccessFormScope);
  return scopes.filter((s): s is RequestAccessFormScope =>
    validScopes.includes(s as RequestAccessFormScope),
  );
}

/**
 * Full-page unauthorized state. Used as the default fallback for page-level RequireScope.
 */
function Unauthorized({
  title = "Access restricted",
  description = "You don't have permission to view this page.",
  scopes,
  all,
  resourceId,
}: {
  title?: string;
  description?: string;
  scopes: Scope[];
  all: boolean;
  resourceId?: string;
}) {
  const [open, setOpen] = React.useState(false);
  const [requestState, setRequestState] = React.useState<
    "idle" | "sending" | "sent" | "error"
  >("idle");

  const { hasScope } = useRBAC();
  const requestAccessMutation = useRequestAccessMutation();
  // Only request scopes the user is actually missing — an all-scopes gate can
  // trip on a partial grant, and admins shouldn't be asked for held scopes.
  const requestableScopes = getRequestableScopes(scopes).filter(
    (s) => !hasScope(s, resourceId),
  );
  const canRequestAccess = requestableScopes.length > 0;

  const handleRequestAccess = async () => {
    if (!canRequestAccess || requestableScopes.length === 0) return;

    setRequestState("sending");
    // An "any scope suffices" gate only needs the first requestable scope; an
    // all-scopes gate must request every missing scope or granting one still
    // leaves the user blocked. Judge on settled results so one failed request
    // doesn't report failure when another admin notification went out.
    const scopesToRequest = all ? requestableScopes : [requestableScopes[0]!];
    const results = await Promise.allSettled(
      scopesToRequest.map((scopeToRequest) =>
        requestAccessMutation.mutateAsync({
          request: {
            requestAccessForm: {
              scope: scopeToRequest,
              resourceId,
            },
          },
        }),
      ),
    );
    const notified = results.some(
      (r) => r.status === "fulfilled" && r.value.sentToCount > 0,
    );
    setRequestState(notified ? "sent" : "error");
  };

  return (
    <div className="flex h-full min-h-[400px] w-full items-center justify-center">
      <div className="flex max-w-sm flex-col items-center gap-3 text-center">
        <div className="bg-muted flex h-12 w-12 items-center justify-center rounded-full">
          <Icon name="lock" className="text-muted-foreground h-5 w-5" />
        </div>
        <h2 className="text-lg font-medium">{title}</h2>
        <p className="text-muted-foreground text-sm">{description}</p>

        {/* Request Access Button */}
        {canRequestAccess && requestState === "idle" && (
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void handleRequestAccess()}
            disabled={requestAccessMutation.isPending}
          >
            {requestAccessMutation.isPending ? "Sending..." : "Request Access"}
          </Button>
        )}

        {requestState === "sending" && (
          <p className="text-muted-foreground text-sm">Sending request...</p>
        )}

        {requestState === "sent" && (
          <div className="flex flex-col items-center gap-1">
            <div className="text-default-success flex items-center gap-1.5 text-sm font-medium">
              <Icon name="check" className="size-4" />
              Request sent
            </div>
            <p className="text-muted-foreground text-xs">
              Your organization admins have been notified.
            </p>
          </div>
        )}

        {requestState === "error" && (
          <div className="flex flex-col items-center gap-2">
            <p className="text-destructive text-sm">
              Failed to send request. Please try again.
            </p>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setRequestState("idle")}
            >
              Retry
            </Button>
          </div>
        )}

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
                <div className="bg-muted/25 border px-3 py-5">
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
                        className="bg-background text-foreground border px-1.5 py-0.5 font-mono text-xs"
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
