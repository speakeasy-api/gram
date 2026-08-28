import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { getInitials } from "@/lib/initials";
import { isNotFoundError } from "@/lib/route-errors";
import { useOrgRoutes } from "@/routes";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import { useIdentity } from "@gram/client/react-query/identity.js";
import { Navigate, Outlet, useLocation, useParams } from "react-router";
import type { IdentityOutletContext } from "./identityRoute";
import { useIdentityProject } from "./useIdentityQueries";

/** How each resolved subject kind reads in the header chip. */
const KIND_LABELS: Record<IdentityModel["kind"], string> = {
  user: "Person",
  apikey: "API key",
  agent: "Agent",
  unattributed: "Unattributed",
};

export default function IdentityDetailRoot(): JSX.Element {
  return (
    // org:read, matching both the index and the server gate on
    // identity.resolve. The admin-only panels gate themselves.
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <IdentityDetailContent />
    </RequireScope>
  );
}

function IdentityDetailContent(): JSX.Element {
  const { identityUrn: encodedUrn } = useParams<{ identityUrn: string }>();
  const urn = encodedUrn ? decodeURIComponent(encodedUrn) : "";
  const orgRoutes = useOrgRoutes();
  const location = useLocation();

  const identityQuery = useIdentity({ urn }, undefined, {
    throwOnError: false,
    enabled: !!urn,
  });
  // Without this the recents entry is the sub-page segment ("overview"), since
  // the URN is neither an id nor long enough for the label heuristic to reject.
  useRecentLabelOverride(location.pathname, identityQuery.data?.displayName);

  // The bare route carries no panels of its own; overview is the landing view.
  if (
    encodedUrn &&
    location.pathname === orgRoutes.identities.href(encodedUrn)
  ) {
    return (
      <Navigate
        to={orgRoutes.identities.detail.overview.href(encodedUrn)}
        replace
      />
    );
  }

  if (
    identityQuery.error &&
    !identityQuery.data &&
    !isNotFoundError(identityQuery.error)
  ) {
    throw identityQuery.error;
  }

  if (!urn || (identityQuery.error && !identityQuery.data)) {
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
              <orgRoutes.identities.Link>
                <SecondaryRouteAction>Back to identities</SecondaryRouteAction>
              </orgRoutes.identities.Link>
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
        />
      </Page.Header>
      <Page.Body fullWidth fullHeight noPadding className="gap-0">
        {/* The person runs the full width of the body: they are the subject of
            the page, not a column in it. */}
        <div className="bg-card border-border border-b px-8 py-6">
          <IdentityHeader identity={identity} />
        </div>
        {/* Rail beside the content, not in the app sidebar: the reader keeps
            the project nav they arrived with. The recessed ground separates
            the navigation from the panels, which carry the card surface. */}
        <div className="bg-background flex min-h-0 flex-1 gap-8 px-8 py-8">
          <IdentityRail
            items={identityRailItems(
              orgRoutes,
              encodedUrn ?? "",
              location.search,
            )}
            className="sticky top-8 hidden w-44 shrink-0 self-start lg:flex"
          />
          <div className="min-w-0 flex-1">
            <Outlet context={context} />
          </div>
        </div>
      </Page.Body>
    </Page>
  );
}

function IdentityHeader({
  identity,
}: {
  identity: IdentityModel;
}): JSX.Element {
  const project = useIdentityProject();
  const {
    dateRange,
    customRange,
    customRangeLabel,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  } = useDateRangeFilter();

  const primaryEmail = identity.emails[0];
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
            <h1 className="text-display-sm truncate font-thin">
              {identity.displayName}
            </h1>
            <div className="flex items-center gap-2">
              {primaryEmail && (
                <Text variant="small" muted className="truncate">
                  {primaryEmail}
                </Text>
              )}
              <Badge variant="neutral">{KIND_LABELS[identity.kind]}</Badge>
            </div>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {/* Usage, cost and risk are recorded per project, so the project is a
              filter on an org-level page rather than part of its address. */}
          {project.options.length > 1 && (
            <Select value={project.slug} onValueChange={project.setSlug}>
              <SelectTrigger className="h-9 w-[180px]">
                <SelectValue placeholder="Project" />
              </SelectTrigger>
              <SelectContent>
                {project.options.map((option) => (
                  <SelectItem key={option.slug} value={option.slug}>
                    {option.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <TimeRangePicker
            preset={customRange ? null : dateRange}
            customRange={customRange}
            customRangeLabel={customRangeLabel}
            onPresetChange={setDateRangeParam}
            onCustomRangeChange={setCustomRangeParam}
            onClearCustomRange={clearCustomRange}
          />
        </div>
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
