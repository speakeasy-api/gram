import { useRBAC } from "@/hooks/useRBAC";
import { useScopeCatalogue } from "@/hooks/useScopeCatalogue";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { RequestAccessFormScope } from "@gram/client/models/components/requestaccessform.js";
import { useRequestAccessMutation } from "@gram/client/react-query/requestAccess.js";
import {
  invalidateAllAccessRequests,
  useAccessRequests,
} from "@gram/client/react-query/accessRequests.js";
import { useQueryClient } from "@tanstack/react-query";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useRoles } from "@gram/client/react-query/roles.js";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { SplashScreen } from "@/components/ui/LogoLoader";
import { Page, useInPageFrame } from "@/components/page-layout";
import {
  breadthLabel,
  suggestRolesForScope,
} from "@/pages/access/roleSuggestions";
import {
  scopeLabel,
  scopeTitle,
  taskNeeds,
  type ScopeCatalogue,
} from "@/pages/access/scopeCatalogue";
import React from "react";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/Tooltip";
import { Popover, PopoverAnchor, PopoverContent } from "./ui/Popover";
import { SelfGrantAccess } from "./temporary-access";
import {
  RequestAccessDialog,
  type AccessRequestDraft,
} from "./request-access-dialog";
import { requestedDurationFields } from "@/pages/access/requestedDuration";

type RenderFn = (props: { disabled: boolean }) => React.ReactNode;

type RequireScopeProps = {
  scope: Scope | Scope[];
  /** When true, ALL scopes must be present. Default: false (any scope suffices). */
  all?: boolean;
  /** Optional resource ID to check scope against. */
  resourceId?: string;
  /** Optional project the resource belongs to, for project-wide grants. */
  projectId?: string;
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
  const { scope, all = false, resourceId, projectId, children, level } = props;
  const { hasAllScopes, hasAnyScope, isLoading } = useRBAC();

  const scopes = Array.isArray(scope) ? scope : [scope];
  const allowed = all
    ? hasAllScopes(scopes, resourceId, projectId)
    : hasAnyScope(scopes, resourceId, projectId);

  const resolveChildren = (disabled: boolean): React.ReactNode =>
    typeof children === "function" ? children({ disabled }) : children;

  // While grants are loading, show the page loader (page level) or nothing,
  // so the unauthorized state never flashes
  if (isLoading) {
    if (level === "page") return <SplashScreen />;
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
        <RestrictedPageFrame>
          {props.fallback ?? (
            <Unauthorized
              scopes={scopes}
              all={all}
              resourceId={resourceId}
              projectId={projectId}
            />
          )}
        </RestrictedPageFrame>
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
          resourceId={resourceId}
          projectId={projectId}
        >
          {resolveChildren(true)}
        </ScopeDisabled>
      );
  }
}

/**
 * Pages are gated from outside their `Page` frame so data hooks never run for
 * people who can't view them, which left the restricted state without the
 * page header — and without the project switcher, the one way into a project
 * the viewer can open. Supply the frame when the gate sits outside one.
 */
function RestrictedPageFrame({
  children,
}: {
  children: React.ReactNode;
}): React.JSX.Element {
  if (useInPageFrame()) return <>{children}</>;
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>{children}</Page.Body>
    </Page>
  );
}

/**
 * Build the "required scope" hint shown alongside the tooltip reason so the
 * user knows exactly which grant they're missing (and can request it). The
 * catalogue label is used once it is loaded: "View tool logs (log:read)".
 */
function requiredScopeLabel(
  scopes: Scope[],
  all: boolean,
  catalogue: ScopeCatalogue,
): string {
  if (scopes.length === 0) return "";
  const titles = scopes.map((s) => scopeTitle(catalogue, s));
  if (titles.length === 1) return `Requires ${titles[0]}.`;
  const joined = titles.join(", ");
  return all
    ? `Requires all of these permissions: ${joined}.`
    : `Requires one of these permissions: ${joined}.`;
}

/**
 * Wraps children in a visually-disabled state with a tooltip explaining why.
 * Clicking the disabled control opens the same explanation with the way to get
 * the permission: Request Access, or a temporary self-grant for an admin.
 */
function ScopeDisabled({
  reason = "You don't have permission to perform this action.",
  className,
  scopes,
  all,
  resourceId,
  projectId,
  children,
}: {
  reason?: string;
  className?: string;
  scopes: Scope[];
  all: boolean;
  resourceId?: string;
  projectId?: string;
  children: React.ReactNode;
}) {
  const { catalogue } = useScopeCatalogue();
  const [open, setOpen] = React.useState(false);
  const scopeHint = requiredScopeLabel(scopes, all, catalogue);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip>
        <PopoverAnchor asChild>
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
                  setOpen(true);
                }}
              >
                {children}
              </div>
            </div>
          </TooltipTrigger>
        </PopoverAnchor>
        {!open && (
          <TooltipContent>
            <span>{reason}</span>
            {scopeHint && (
              <span className="mt-1 block opacity-80">{scopeHint}</span>
            )}
          </TooltipContent>
        )}
      </Tooltip>
      <PopoverContent align="start" className="w-80 space-y-3 text-sm">
        <div className="space-y-1">
          <p>{reason}</p>
          {scopeHint && <p className="text-muted-foreground">{scopeHint}</p>}
        </div>
        <RequestScopeAccess
          scopes={scopes}
          all={all}
          resourceId={resourceId}
          projectId={projectId}
          align="start"
        />
      </PopoverContent>
    </Popover>
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

type RequestScopeAccessProps = {
  scopes: Scope[];
  /** When true, every missing scope is requested, not only the first. */
  all?: boolean;
  resourceId?: string;
  /** The gated resource's name, shown to the requester and the admins. */
  resourceName?: string;
  /** The project the gated resource belongs to, for project-wide grants. */
  projectId?: string;
  /**
   * For an admin who can give themselves the access: names the resource for
   * the "covers" choice (this one, or every one of its kind).
   */
  coverage?: { one: string; all: string };
  /** Runs once an admin has given themselves the access. */
  onGranted?: () => void;
  /**
   * Where the result lines up: centered under a full-page message, or with
   * the start of a banner's text above it.
   */
  align?: "center" | "start";
  className?: string;
};

/**
 * The one missing scope an organization admin can give themselves for a
 * limited time, or null. An all-scopes gate qualifies only when a single
 * scope is missing: granting one of several would still leave the page shut.
 */
function useSelfGrantableScope(
  scopes: Scope[],
  all: boolean,
  resourceId: string | undefined,
  projectId: string | undefined,
): Scope | null {
  const { hasScope } = useRBAC();
  const { catalogue } = useScopeCatalogue();
  if (!hasScope("org:admin")) return null;
  // A gate on "anything in this project" has no single resource to grant, and
  // a grant on every resource would reach past the project. Ask instead.
  if (projectId && (resourceId === undefined || resourceId === "*")) {
    return null;
  }
  const missing = scopes.filter((s) => !hasScope(s, resourceId, projectId));
  if (all && missing.length !== 1) return null;
  return missing.find((s) => catalogue.get(s)?.temporaryGrantable) ?? null;
}

/**
 * Gets the viewer the scopes they are missing. An organization admin gives
 * themselves a scope for a few hours when the API allows it; anyone else asks
 * an admin, and is told how that went. Renders nothing when every requestable
 * scope is already held.
 */
export function RequestScopeAccess(
  props: RequestScopeAccessProps,
): React.JSX.Element | null {
  const selfGrantScope = useSelfGrantableScope(
    props.scopes,
    props.all ?? false,
    props.resourceId,
    props.projectId,
  );
  if (selfGrantScope) {
    return (
      <SelfGrantAccess
        scope={selfGrantScope}
        resourceId={props.resourceId}
        coverage={props.coverage}
        onGranted={props.onGranted}
        className={props.className}
      />
    );
  }
  // A request is for one resource and one set of scopes. The key remounts
  // the form when either changes, so "Request sent" for one project does not
  // carry over to the next.
  return (
    <RequestScopeAccessForm
      key={`${props.projectId ?? ""}|${props.resourceId ?? ""}|${props.all ? "all" : "any"}|${props.scopes.join(",")}`}
      {...props}
    />
  );
}

function RequestScopeAccessForm(
  props: RequestScopeAccessProps,
): React.JSX.Element | null {
  // With stored access requests, an approval is a temporary grant, so only
  // scopes that can be given temporarily can be asked for, and a request made
  // earlier shows as pending.
  const accessRequestsOn =
    useFeatureFlag(FEATURE_FLAGS.accessRequests).status === "enabled";
  if (accessRequestsOn) return <StoredRequestScopeAccess {...props} />;
  return (
    <RequestScopeAccessButton
      {...props}
      accessRequestsOn={false}
      pending={NO_PENDING_REQUESTS}
      pendingUpdatedAt={0}
      onRequested={undefined}
    />
  );
}

const NO_PENDING_REQUESTS: PendingRequest[] = [];

type PendingRequest = { scope: string; selector: { resourceId: string } };

function StoredRequestScopeAccess(
  props: RequestScopeAccessProps,
): React.JSX.Element | null {
  const queryClient = useQueryClient();
  const myPending = useAccessRequests(
    { status: "pending", mine: true },
    undefined,
    { throwOnError: false },
  );
  return (
    <RequestScopeAccessButton
      {...props}
      accessRequestsOn
      pending={myPending.data?.requests ?? NO_PENDING_REQUESTS}
      pendingUpdatedAt={myPending.dataUpdatedAt ?? 0}
      onRequested={() => invalidateAllAccessRequests(queryClient)}
    />
  );
}

function RequestScopeAccessButton({
  scopes,
  all = false,
  resourceId,
  resourceName,
  projectId,
  className,
  align = "center",
  accessRequestsOn,
  pending,
  pendingUpdatedAt,
  onRequested,
}: RequestScopeAccessProps & {
  accessRequestsOn: boolean;
  pending: PendingRequest[];
  pendingUpdatedAt: number;
  onRequested: (() => Promise<unknown>) | undefined;
}): React.JSX.Element | null {
  const [requestState, setRequestState] = React.useState<
    | "idle"
    | "sending"
    | "sent"
    | "pending"
    | "not-requestable"
    | "no-admin"
    | "error"
  >("idle");
  const { hasScope } = useRBAC();
  const { catalogue } = useScopeCatalogue();
  const requestAccessMutation = useRequestAccessMutation();
  // While requests are stored, asking opens a dialog for how long the access
  // is needed and why; otherwise the button asks straight away.
  const [dialogOpen, setDialogOpen] = React.useState(false);
  // Only request scopes the user is actually missing — an all-scopes gate can
  // trip on a partial grant, and admins shouldn't be asked for held scopes.
  const missingScopes = scopes.filter(
    (s) => !hasScope(s, resourceId, projectId),
  );
  // Members who can't read the organization can't load the catalogue either;
  // a scope it does not describe is offered, and the server refuses it if it
  // can't be given temporarily.
  const askableScopes = getRequestableScopes(missingScopes);
  const requestableScopes = accessRequestsOn
    ? askableScopes.filter(
        (s) => catalogue.get(s)?.temporaryGrantable !== false,
      )
    : askableScopes;
  // A pending request for this access, from an earlier visit, shows as
  // pending instead of offering to ask again.
  const alreadyPending = pending.some(
    (request) =>
      requestableScopes.includes(request.scope as RequestAccessFormScope) &&
      request.selector.resourceId === (resourceId ?? "*"),
  );

  // A request made here shows as pending until a pending list read after it
  // was made no longer holds it: the app-wide watcher refreshes that list
  // once the request is decided, even when that happens before any poll.
  const [requestedAt, setRequestedAt] = React.useState(0);
  React.useEffect(() => {
    if (requestState !== "pending") return;
    if (pendingUpdatedAt > requestedAt && !alreadyPending) {
      setRequestState("idle");
    }
  }, [alreadyPending, pendingUpdatedAt, requestedAt, requestState]);

  if (missingScopes.length === 0) return null;
  // An all-scopes gate stays shut unless every missing scope can be asked for.
  const cannotUnblock =
    requestableScopes.length === 0 ||
    (all && requestableScopes.length < missingScopes.length);
  const stack = cn(
    "flex flex-col gap-1",
    align === "start" ? "items-start text-left" : "items-center text-center",
    className,
  );
  if (cannotUnblock || requestState === "not-requestable") {
    return (
      <p className={cn("text-muted-foreground max-w-sm text-sm", stack)}>
        This access can't be requested here. Ask an organization administrator
        to give it to you.
      </p>
    );
  }
  // An "any scope suffices" gate only needs the first requestable scope; an
  // all-scopes gate must request every missing scope or granting one still
  // leaves the user blocked. The dialog shows exactly these.
  const requestedScopes = all
    ? requestableScopes
    : requestableScopes.slice(0, 1);
  const handleRequestAccess = async (draft?: AccessRequestDraft) => {
    setRequestState("sending");
    // An "any scope suffices" gate only needs the first requestable scope; an
    // all-scopes gate must request every missing scope or granting one still
    // leaves the user blocked. Judge on settled results so one failed request
    // doesn't report failure when another admin notification went out.
    const scopesToRequest = requestedScopes;
    const results = await Promise.allSettled(
      scopesToRequest.map((scopeToRequest) =>
        requestAccessMutation.mutateAsync({
          request: {
            requestAccessForm: {
              scope: scopeToRequest,
              resourceId,
              resourceName,
              ...(draft
                ? {
                    ...requestedDurationFields(draft.duration, new Date()),
                    message: draft.message || undefined,
                  }
                : {}),
            },
          },
        }),
      ),
    );
    const stored = results.some(
      (r) => r.status === "fulfilled" && r.value.request !== undefined,
    );
    if (stored) {
      // The app-wide watcher tells the viewer when it is decided.
      setRequestedAt(Date.now());
      setRequestState("pending");
      void onRequested?.();
      return;
    }
    const notified = results.some(
      (r) => r.status === "fulfilled" && r.value.sentToCount > 0,
    );
    if (notified) {
      setRequestState("sent");
      return;
    }
    const refused = results.every(
      (r) =>
        r.status === "rejected" &&
        r.reason instanceof GramError &&
        r.reason.statusCode === 400,
    );
    if (accessRequestsOn && refused) {
      setRequestState("not-requestable");
      return;
    }
    const reachedServer = results.some((r) => r.status === "fulfilled");
    setRequestState(reachedServer ? "no-admin" : "error");
  };

  const state =
    requestState === "idle" && alreadyPending ? "pending" : requestState;
  switch (state) {
    case "pending":
      return (
        <div className={stack}>
          <div className="text-default-success flex items-center gap-1.5 text-sm font-medium">
            <Icon name="check" className="size-4" />
            Request pending
          </div>
          <p className="text-muted-foreground text-xs">
            An organization admin can approve it in Speakeasy or Slack. It
            lapses after 24 hours.
          </p>
        </div>
      );
    case "idle":
    case "sending":
      return (
        <>
          <Button
            variant="secondary"
            size="sm"
            className={className}
            onClick={() =>
              accessRequestsOn
                ? setDialogOpen(true)
                : void handleRequestAccess()
            }
            disabled={state === "sending"}
          >
            {state === "sending" && !dialogOpen ? "Sending…" : "Request Access"}
          </Button>
          {accessRequestsOn && (
            <RequestAccessDialog
              key={`${requestedScopes.join(",")}|${resourceId ?? ""}`}
              open={dialogOpen}
              onOpenChange={setDialogOpen}
              permissions={requestedScopes.map(
                (scope) => catalogue.get(scope)?.label ?? scope,
              )}
              resourceName={
                resourceName ?? (resourceId ? "This resource" : undefined)
              }
              askDuration
              sending={state === "sending"}
              onSubmit={(draft) => {
                void handleRequestAccess(draft).finally(() =>
                  setDialogOpen(false),
                );
              }}
            />
          )}
        </>
      );
    case "sent":
      return (
        <div className={stack}>
          <div className="text-default-success flex items-center gap-1.5 text-sm font-medium">
            <Icon name="check" className="size-4" />
            Request sent
          </div>
          <p className="text-muted-foreground text-xs">
            Your organization admins have been notified.
          </p>
        </div>
      );
    case "no-admin":
      return (
        <p className={cn("text-destructive max-w-sm text-sm", stack)}>
          No organization administrator could be notified. Contact an
          administrator directly to request access.
        </p>
      );
    case "error":
      return (
        <div className={stack}>
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
      );
  }
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
  projectId,
}: {
  title?: string;
  description?: string;
  scopes: Scope[];
  all: boolean;
  resourceId?: string;
  projectId?: string;
}) {
  const [open, setOpen] = React.useState(false);

  const { hasScope } = useRBAC();
  const { catalogue, tasks } = useScopeCatalogue();

  return (
    <div className="flex h-full min-h-[400px] w-full items-center justify-center">
      <div className="flex max-w-sm flex-col items-center gap-3 text-center">
        <div className="bg-muted flex h-12 w-12 items-center justify-center rounded-full">
          <Icon name="lock" className="text-muted-foreground h-5 w-5" />
        </div>
        <h2 className="text-lg font-medium">{title}</h2>
        <p className="text-muted-foreground text-sm">{description}</p>

        <RequestScopeAccess
          scopes={scopes}
          all={all}
          resourceId={resourceId}
          projectId={projectId}
        />

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
                <div className="bg-muted/25 border px-3 py-5 text-left">
                  <p className="text-muted-foreground text-center text-xs">
                    {scopes.length === 1
                      ? "An organization admin can grant you:"
                      : all
                        ? "An organization admin can grant you all of:"
                        : "An organization admin can grant you any of:"}
                  </p>
                  <ul className="mt-2 space-y-2">
                    {scopes.map((s) => {
                      const entry = catalogue.get(s);
                      return (
                        <li
                          key={s}
                          className="bg-background text-foreground border px-2 py-1.5 text-xs"
                        >
                          <span className="font-medium">
                            {scopeLabel(catalogue, s)}
                          </span>{" "}
                          <span className="text-muted-foreground font-mono">
                            {s}
                          </span>
                          {entry?.description && (
                            <p className="text-muted-foreground mt-0.5">
                              {entry.description}
                            </p>
                          )}
                        </li>
                      );
                    })}
                  </ul>
                  <TaskHint
                    scopes={scopes}
                    catalogue={catalogue}
                    tasks={tasks}
                    held={(s) => hasScope(s, resourceId, projectId)}
                  />
                  {scopes.length === 1 && (
                    <RoleHint
                      scope={scopes[0]!}
                      catalogue={catalogue}
                      resourceId={resourceId}
                      projectId={projectId}
                    />
                  )}
                </div>
              </CollapsibleContent>
            </Collapsible>
          </div>
        )}
      </div>
    </div>
  );
}

/**
 * Names the task the missing permission is part of, and what else it needs
 * that the person does not hold yet.
 */
function TaskHint({
  scopes,
  catalogue,
  tasks,
  held,
}: {
  scopes: Scope[];
  catalogue: ScopeCatalogue;
  tasks: ReturnType<typeof useScopeCatalogue>["tasks"];
  held: (scope: Scope) => boolean;
}): React.JSX.Element | null {
  const partOf = tasks.filter((task) =>
    scopes.some((s) => taskNeeds(catalogue, task, s)),
  );
  if (partOf.length === 0) return null;
  return (
    <ul className="text-muted-foreground mt-3 space-y-1 text-xs">
      {partOf.map((task) => {
        const alsoNeeds = task.scopes.filter(
          (s) => !scopes.includes(s as Scope) && !held(s as Scope),
        );
        return (
          <li key={task.slug}>
            Part of <span className="font-medium">{task.label}</span>
            {alsoNeeds.length > 0 &&
              `, which also needs ${alsoNeeds
                .map((s) => scopeLabel(catalogue, s))
                .join(", ")}`}
            .
          </li>
        );
      })}
    </ul>
  );
}

/**
 * Names the roles an admin could assign: the ones that give exactly this
 * permission first, then the ones that give more. Reading roles needs
 * org:read, which members hold by default; without it the hint is skipped.
 */
function RoleHint({
  scope,
  catalogue,
  resourceId,
  projectId,
}: {
  scope: Scope;
  catalogue: ScopeCatalogue;
  resourceId?: string;
  projectId?: string;
}): React.JSX.Element | null {
  const { data: rolesData } = useRoles();
  const suggestions = React.useMemo(
    () =>
      suggestRolesForScope(
        rolesData?.roles ?? [],
        scope,
        catalogue,
        resourceId,
        projectId,
      ),
    [rolesData?.roles, scope, catalogue, resourceId, projectId],
  );
  if (suggestions.exact.length === 0 && suggestions.broader.length === 0) {
    return null;
  }
  return (
    <div className="text-muted-foreground mt-3 space-y-1 text-xs">
      {suggestions.exact.length > 0 && (
        <p>
          Roles that give exactly this:{" "}
          {suggestions.exact.map(({ role, breadth }, i) => {
            const reach = breadthLabel(scope, breadth);
            return (
              <React.Fragment key={role.id}>
                {i > 0 && ", "}
                <span className="text-foreground">{role.name}</span>
                {reach && ` (${reach})`}
              </React.Fragment>
            );
          })}
          .
        </p>
      )}
      {suggestions.broader.length > 0 && (
        <p>
          Roles that give more than this:{" "}
          {suggestions.broader.map(
            ({ role, extraScopes, extraSensitive }, i) => (
              <React.Fragment key={role.id}>
                {i > 0 && ", "}
                <span className="text-foreground">{role.name}</span> (+
                {extraScopes.length}
                {extraSensitive.length > 0
                  ? `, ${extraSensitive.length} sensitive`
                  : ""}
                )
              </React.Fragment>
            ),
          )}
          .
        </p>
      )}
    </div>
  );
}
