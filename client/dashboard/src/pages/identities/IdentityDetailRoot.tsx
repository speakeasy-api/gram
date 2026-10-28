import { Page } from "@/components/page-layout";
import { IdentityRail } from "@/components/identity-rail";
import { identityRailItems } from "@/components/identity-rail-items";
import { useRecentLabelOverride } from "@/components/command-palette/recentlyVisited";
import { RequireScope } from "@/components/require-scope";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { Badge } from "@/components/ui/Badge";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { getInitials } from "@/lib/initials";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { isBadRequestError, isNotFoundError } from "@/lib/route-errors";
import { useRoutes } from "@/routes";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import {
  AgentHeaderActions,
  AgentIdentityProfile,
  AgentRenameAction,
} from "./AgentIdentityProfile";
import { useIdentitySubject } from "./useIdentitySubject";
import {
  useCallback,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { Navigate, Outlet, useLocation, useParams } from "react-router";
import type { IdentityOutletContext } from "./identityRoute";
import { useCanReadRisk, useIdentityIsKnown } from "./useIdentityQueries";
import { withoutFindingsFilter } from "./identityFindingsLink";

/** How each resolved subject kind reads in the header chip. */
/**
 * How the subject reads in the header.
 *
 * An address the directory does not know still belongs to a person, so it is
 * labelled as one; that they hold no account here is said separately, beside
 * the label, rather than by calling them a different kind of thing.
 */
function kindLabel(identity: IdentityModel): string {
  switch (identity.kind) {
    case "user":
      return "Person";
    case "apikey":
      return "API key";
    case "agent":
      return "Agent";
    case "unattributed":
      // An address is someone's; a bare agent id may name no one.
      return identity.emails.length > 0 ? "Person" : "Agent";
  }
  return "Unknown identity";
}

/**
 * True for a person whose activity we see but who is not linked to one account
 * here. The resolver reports both "matches no member" and "several members
 * claim this address" as unattributed, so the wording says what is certain —
 * there is no single account behind this identity — rather than asserting that
 * no account exists.
 */
function hasNoLinkedAccount(identity: IdentityModel): boolean {
  return identity.kind === "unattributed" && identity.emails.length > 0;
}

export default function IdentityDetailRoot(): JSX.Element {
  const { identityUrn: urn = "" } = useParams<{ identityUrn: string }>();
  // An agent resolves through its own management API, which authorizes per
  // agent, so ownership is enough to read one. This used to send such a
  // caller to a second agent page built for exactly that case; there is one
  // agent page now, and it serves them.
  if (urn.startsWith("agent:")) {
    return <IdentityDetailContent />;
  }
  return (
    // org:read, matching the server gate on identity.resolve — which is what
    // this page opens with, so a project:read-only reader would otherwise get
    // a rendered page that fails its first request rather than a straight
    // answer about access. The roster stays on project:read: it reads project
    // telemetry, not the directory. org:admin carries org:read as a sub-scope,
    // and the admin-only panels gate themselves.
    <RequireScope scope={["org:read"]} level="page">
      <IdentityDetailContent />
    </RequireScope>
  );
}

function IdentityDetailContent(): JSX.Element {
  // useParams hands back the decoded segment already; decoding it again would
  // corrupt an identifier that legitimately contains a percent escape.
  const { identityUrn: urn = "" } = useParams<{ identityUrn: string }>();
  const encodedUrn = urn ? encodeIdentityUrn(urn) : undefined;
  const canReadRisk = useCanReadRisk();
  const routes = useRoutes();
  const location = useLocation();

  const identityQuery = useIdentitySubject(urn);
  // How far down the rail has to stick to clear the header above it. The
  // header's height is not a constant — a long name wraps, and so does a row
  // of directory groups — so a fixed offset parks the rail under the header's
  // own surface, which wins on z-index, as soon as the header outgrows it.
  // Measured through a ref callback rather than an effect: the header is not
  // mounted on the loading and not-found branches above.
  const [headerHeight, setHeaderHeight] = useState<number | null>(null);
  const measureHeader = useCallback((node: HTMLDivElement | null) => {
    if (!node) return;
    const observer = new ResizeObserver(() => {
      setHeaderHeight(node.getBoundingClientRect().height);
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  // Without this the recents entry is the sub-page segment ("overview"), since
  // the URN is neither an id nor long enough for the label heuristic to reject.
  useRecentLabelOverride(location.pathname, identityQuery.data?.displayName);
  // The resolver answers any well-formed URN with a subject, so a typo renders
  // a complete page for someone who does not exist unless we check.
  const subject = useIdentityIsKnown(identityQuery.data);

  // The bare route carries no panels of its own; overview is the landing view.
  if (encodedUrn && location.pathname === routes.identities.href(encodedUrn)) {
    return (
      <Navigate
        to={routes.identities.detail.overview.href(encodedUrn)}
        replace
      />
    );
  }

  // A URN the server rejects is a bad address the reader can see and mistype,
  // so it reads as "not found" rather than as the app falling over.
  if (
    identityQuery.error &&
    !identityQuery.data &&
    !isNotFoundError(identityQuery.error) &&
    !isBadRequestError(identityQuery.error)
  ) {
    throw identityQuery.error;
  }

  if (
    !urn ||
    (identityQuery.error && !identityQuery.data) ||
    (identityQuery.data && !subject.isPending && !subject.known)
  ) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RouteNotFoundState
            title="Identity not found"
            description="No activity in this organization resolves to that identifier."
            action={
              <routes.identities.Link>
                <SecondaryRouteAction>Back to identities</SecondaryRouteAction>
              </routes.identities.Link>
            }
          />
        </Page.Body>
      </Page>
    );
  }

  if (identityQuery.isPending || !identityQuery.data) {
    return <IdentityDetailLoading />;
  }

  const identity = identityQuery.data;
  const context: IdentityOutletContext = { identity, urn };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={
            encodedUrn ? { [encodedUrn]: identity.displayName } : undefined
          }
          // The identity's own path has no page on it, so its crumb points
          // at the section the page opens on.
          urlSubstitutions={
            encodedUrn
              ? {
                  [`/identities/${encodedUrn}`]: `/identities/${encodedUrn}/overview`,
                }
              : {}
          }
          // The list this identity was opened from. It is a sibling route
          // rather than a parent, so the path cannot supply the crumb.
          insertAfter={{
            segment: "identities",
            display: identity.agent ? "Agents" : "People",
            url: identity.agent
              ? routes.identities.agents.href()
              : routes.identities.people.href(),
          }}
        />
      </Page.Header>
      {/* `h-auto min-h-full` rather than the `h-full` fullHeight applies:
          that pins the content box to the viewport, so a page taller than it
          overflows a box that has already stopped — the recessed ground ends
          mid-scroll with white under it, and the last panel cannot be
          reached. Growing with the content keeps the scroll container and
          the painted ground the same height. */}
      <Page.Body
        fullWidth
        fullHeight
        noPadding
        className="h-auto min-h-full gap-0"
      >
        {/* The person runs the full width of the body: they are the subject of
            the page, not a column in it. */}
        {/* Sticky: the rail beside the content already is, and the subject
            of the page is worth as much as its navigation — scrolling a long
            activity list otherwise leaves no sign of whose it is. */}
        <div
          ref={measureHeader}
          className="bg-card border-border sticky top-0 z-20 border-b px-8 py-6"
        >
          <IdentityHeader
            identity={identity}
            nameAction={
              identity.agent && (
                <AgentRenameAction
                  agent={identity.agent}
                  refresh={() => void identityQuery.refetch()}
                />
              )
            }
            actions={
              identity.agent && <AgentHeaderActions agent={identity.agent} />
            }
          />
        </div>
        {/* Rail beside the content, not in the app sidebar: the reader keeps
            the project nav they arrived with. The recessed ground separates
            the navigation from the panels, which carry the card surface. */}
        {/* pb-16, not py-8: the body is the scroll container, so a symmetric
            inset leaves the last panel sitting flush against the bottom edge
            of the viewport with nothing under it. */}
        {/* No `min-h-0`: that lets this shrink below its content, which is
            what cut the ground short of the panels standing on it. */}
        <div
          className="bg-background flex flex-1 flex-col gap-6 px-8 pt-8 pb-16 lg:flex-row lg:gap-8"
          style={
            headerHeight === null
              ? undefined
              : ({
                  "--identity-rail-top": `${headerHeight + 16}px`,
                } as CSSProperties)
          }
        >
          <IdentityRail
            items={identityRailItems(
              routes,
              encodedUrn ?? "",
              // The Findings filter is that page's own, not a setting to
              // carry into the other sections and back.
              withoutFindingsFilter(location.search),
              // Agents render the agent profile, which has no Findings view.
              canReadRisk && !identity.agent,
              identity.kind,
            )}
            // Narrow, the rail is a scrollable row above the content: hiding
            // it left the other sub-pages reachable only by editing the URL.
            // Sticks clear of the header above it rather than under it: both
            // are pinned to the same scroll container, and the header wins
            // on z-index, so a smaller offset parks the rail out of sight.
            // The offset is the measured header height, set on the parent;
            // the 12rem fallback holds for the frame before it is measured.
            className="border-border -mx-2 shrink-0 flex-row overflow-x-auto border-b px-2 pb-1 lg:sticky lg:top-[var(--identity-rail-top,12rem)] lg:mx-0 lg:w-44 lg:flex-col lg:self-start lg:overflow-visible lg:border-b-0 lg:px-0 lg:pb-0"
          />
          {/* Room under the last section: without it the danger zone and
              the federated access panel sit flush against the viewport. */}
          <div className="min-w-0 flex-1 pb-24">
            {identity.agent ? (
              <AgentIdentityProfile
                key={identity.agent.id}
                agent={identity.agent}
                section={
                  location.pathname.split("/").filter(Boolean).at(-1) ??
                  "overview"
                }
                refresh={() => void identityQuery.refetch()}
              />
            ) : (
              <Outlet context={context} />
            )}
          </div>
        </div>
      </Page.Body>
    </Page>
  );
}

function IdentityHeader({
  identity,
  nameAction,
  actions,
}: {
  identity: IdentityModel;
  /** A control on the name itself (rename), shown beside it on hover. */
  nameAction?: ReactNode;
  /** Controls on the subject itself, in the header's top right. */
  actions?: ReactNode;
}): JSX.Element {
  const primaryEmail = identity.emails[0];
  // Tobias is for names. When the only name we have is the address itself, the
  // display face loses the punctuation that makes it readable, and repeating it
  // underneath says nothing twice.
  const nameIsAddress = identity.displayName === primaryEmail;
  const { directory } = identity;
  // Department, title and employment type read as one line rather than as
  // three chips: they describe the person, they are not filters.
  const directoryLine = [
    directory.departmentName,
    directory.jobTitle,
    directory.employeeType,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div className="flex min-w-0 items-center gap-4">
          <Avatar className="size-12">
            {identity.photoUrl && (
              <AvatarImage src={identity.photoUrl} alt={identity.displayName} />
            )}
            <AvatarFallback>{getInitials(identity.displayName)}</AvatarFallback>
          </Avatar>
          <div className="flex min-w-0 flex-col gap-1">
            <div className="group/name flex min-w-0 items-center gap-1">
              <h1
                className={
                  nameIsAddress
                    ? "truncate font-mono text-xl"
                    : "text-display-sm truncate font-thin"
                }
              >
                {identity.displayName}
              </h1>
              {nameAction && (
                <span className="shrink-0 opacity-0 transition-opacity group-hover/name:opacity-100 focus-within:opacity-100">
                  {nameAction}
                </span>
              )}
            </div>
            <div className="flex items-center gap-2">
              {primaryEmail && !nameIsAddress && (
                <Text variant="small" muted className="truncate">
                  {primaryEmail}
                </Text>
              )}
              <Badge variant="neutral">{kindLabel(identity)}</Badge>
              {hasNoLinkedAccount(identity) && (
                <Badge variant="neutral">No linked account</Badge>
              )}
            </div>
          </div>
        </div>
        {actions && (
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {directory.groups.map((group) => (
          <Badge key={group} variant="neutral">
            {group}
          </Badge>
        ))}
        {directoryLine && (
          <Text variant="small" muted>
            {directoryLine}
          </Text>
        )}
        <Text variant="small" muted className="ml-auto font-mono text-xs">
          {identity.canonicalUrn}
        </Text>
      </div>
    </div>
  );
}

function IdentityDetailLoading(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body fullWidth className="gap-0">
        <div
          aria-label="Loading identity"
          className="mx-auto w-full max-w-[1270px] flex-1 space-y-8 px-8 py-8"
        >
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-64 w-full" />
          <Skeleton className="h-48 w-full" />
        </div>
      </Page.Body>
    </Page>
  );
}
