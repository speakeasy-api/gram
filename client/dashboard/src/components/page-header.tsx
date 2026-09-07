// oxlint-disable react/only-export-components -- compound component (Object.assign) pattern
import { CommandPaletteTrigger } from "./command-palette/CommandPaletteTrigger";
import { useOrganization, useProject } from "@/contexts/Auth.tsx";
import { useSlugs } from "@/contexts/Sdk.tsx";
import { useRBAC } from "@/hooks/useRBAC";
import { cn, titleCaseSlug } from "@/lib/utils.ts";
import React from "react";
import { ChevronRight } from "lucide-react";
import { Link, useLocation, useMatch, useParams } from "react-router";
import { PaygCapReachedBanners } from "./billing/billing-banners.tsx";
import { HatchRule } from "./hatch-rule.tsx";
import { InsightsDockShortcutHint } from "./insights-dock-shortcut-hint.tsx";
import { OnboardingBanner } from "./onboarding-banner.tsx";
import { ReleaseStage, ReleaseStageBadge } from "./release-stage-badge.tsx";
import { Heading } from "@/components/ui/Heading";
import { WorkspaceSwitcher } from "./workspace-switcher.tsx";

function PageHeaderComponent({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  // The org billing route, matched as a route rather than by suffix: a project
  // path ending in the same segment is a different page, and suppressing the
  // banner there would hide a paused organization from the page it was working
  // on.
  const onBillingPage = useMatch("/:orgSlug/billing") !== null;
  const showBreadcrumbs = useShowBreadcrumbs();

  return (
    <>
      <header
        className={cn(
          "flex h-(--header-height) shrink-0 items-center gap-2 transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-(--header-height)",
          className,
        )}
      >
        {/* px-8 matches Page.Body's padding; the switcher pulls back by its own
            inner padding so its tile lines up with the content edge. */}
        <div className="flex w-full items-center gap-3 px-8">
          {/* Project context lives here, in the slot the breadcrumbs used to
              occupy, rather than in the sidebar. The collapse control moved to
              the sidebar header alongside the logo. */}
          <WorkspaceSwitcher className="-ml-1.5 w-auto border-0 px-1.5" />
          {!showBreadcrumbs && children}
          <div className="ml-auto flex shrink-0 items-center gap-2">
            <InsightsDockShortcutHint />
            <CommandPaletteTrigger />
          </div>
        </div>
      </header>
      {/* Crosshatch rule (marketing-site idiom) divides header from content and
          continues the sidebar header's own rule across the pane boundary. */}
      <HatchRule />
      {/* The trail gets its own bar rather than sharing the header row: the
          switcher names where you are and the trail names how you got there,
          and on one line the two read as a single path. px-8 matches
          Page.Body so the crumbs line up with the content edge. */}
      {showBreadcrumbs && (
        <div className="border-foreground/10 flex shrink-0 items-center gap-2 border-b px-8 py-2.5">
          {children}
        </div>
      )}
      <OnboardingBanner />
      {/* Inference stopping is felt on whichever page the user was working on,
          so the reason for it rides the header rather than waiting on the
          billing page. Billing renders all of its banners together so payment
          failure can remain the first, destructive state. */}
      {!onBillingPage && <PaygCapReachedBanners />}
    </>
  );
}

function PageHeaderTitle({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    // 1270 carefully chosen to make the header line up with the max width of the page content
    <Heading
      variant="h4"
      className={cn("ml-1 w-full max-w-[1270px]", className)}
    >
      {children}
    </Heading>
  );
}

// Static path segments are auto Title-Cased (see `titleCaseSlug`), so this map
// only needs to hold the exceptions where that produces the wrong text:
//   - acronyms / non-standard casing (MCP, SDKs, OpenAPI, API)
//   - lowercased connector words ("from")
//   - rebrands where the display name differs from the URL segment. `slack` is
//     kept in the URL for backwards compatibility but was renamed.
const breadcrumbSubstitutions = {
  mcp: "MCP",
  "platform-mcp": "Platform MCP",
  "shadow-mcp": "Shadow MCP",
  sdks: "SDKs",
  "add-openapi": "Add OpenAPI",
  "add-from-catalog": "Add from Catalog",
  // Segments of the unified MCP add flow (S-853).
  add: "Add Server",
  "from-existing-source": "From Existing Source",
  openapi: "OpenAPI",
  remote: "Remote MCP",
  tunneled: "Tunneled MCP",
  unproxied: "Unproxied MCP",
  "ai-integrations": "AI Integrations",
  "api-keys": "API Keys",
  "mdm-integrations": "MDM Integrations",
  rbac: "RBAC Override",
  jamf: "Jamf Pro",
  slack: "Assistants",
};

// Segments that appear in crumb trails but are not routable pages themselves.
// Their crumb links to the surface that owns them instead of a dead path —
// the requests segment (present only in legacy request-detail URLs) points
// at the unified Shadow MCP servers table rather than at a 404.
const breadcrumbUrlSubstitutions: Record<string, string> = {
  "/shadow-mcp/requests": "/shadow-mcp",
};

// One rendered crumb. Pending crumbs (substitution key present, value not yet
// resolved) show a placeholder so the raw id/slug never flashes before its
// human label arrives.
function BreadcrumbCrumb({
  elem,
}: {
  elem: {
    url: string;
    display: string;
    isCurrentPage: boolean;
    disableLink?: boolean;
    pending?: boolean;
  };
}) {
  if (elem.pending) {
    return (
      <span
        aria-hidden="true"
        className="bg-muted inline-block h-3.5 w-20 animate-pulse align-middle"
      />
    );
  }
  if (elem.isCurrentPage || elem.disableLink) {
    return (
      <span
        className={elem.isCurrentPage ? undefined : "text-muted-foreground"}
      >
        {elem.display}
      </span>
    );
  }
  return (
    <Link
      to={elem.url}
      className="text-muted-foreground hover:text-foreground trans hover:underline underline-offset-4"
    >
      {elem.display}
    </Link>
  );
}

type PageHeaderBreadcrumbsProps = {
  fullWidth?: boolean;
  className?: string;
  substitutions?: Record<string, string | undefined>;
  skipSegments?: string[];
  stage?: ReleaseStage;
};

type PageHeaderBreadcrumbsTrailProps = PageHeaderBreadcrumbsProps & {
  /** Prepend the org and project crumbs. Default: true. */
  rootCrumbs?: boolean;
};

// Breadcrumbs are hidden: the header now carries the project switcher instead.
// The renderer below is kept intact (and every caller keeps mounting
// <PageHeader.Breadcrumbs>) so flipping this back on is a one-line change.
// Widened to boolean so the renderer below doesn't type as unreachable.
// MCP is the exception to the app-wide hiding above. S-853 made it the
// inventory and nested the add flow, the catalog, and sources underneath it, so
// those pages sit two and three levels deep with no way back up but browser
// back. Listed by first path segment; every caller still mounts
// <PageHeader.Breadcrumbs>, so adding a surface here is all it takes.
const BREADCRUMB_PAGE_SLUGS = new Set(["mcp"]);

// Whether the current page shows a breadcrumb trail. Shared by the header (to
// give the trail its own bar) and by the breadcrumbs themselves (to render
// nothing elsewhere), so the two can't disagree.
function useShowBreadcrumbs(): boolean {
  const { pathname } = useLocation();
  // Anchored to the fixed /:orgSlug/projects/:projectSlug/:page shape, mirroring
  // useNavArea, so an org or project slugged "projects" can't shift which
  // segment is read as the page.
  const projectPage = pathname.match(
    /^\/[^/]+\/projects\/[^/]+\/([^/?#]+)/,
  )?.[1];
  return !!projectPage && BREADCRUMB_PAGE_SLUGS.has(projectPage);
}

function PageHeaderBreadcrumbs(props: PageHeaderBreadcrumbsProps) {
  const show = useShowBreadcrumbs();
  if (!show) return null;
  // The org and project already have a home in the header's project switcher,
  // so the trail starts at the page itself rather than repeating them.
  return <PageHeaderBreadcrumbsTrail {...props} rootCrumbs={false} />;
}

function PageHeaderBreadcrumbsTrail({
  fullWidth,
  className,
  substitutions = {}, // Any segment and how it should be displayed, for example toolset slug -> toolset name
  skipSegments = [], // Segments to skip/hide from breadcrumbs
  stage,
  rootCrumbs = true,
}: PageHeaderBreadcrumbsTrailProps) {
  const params = useParams();
  const { orgSlug, projectSlug } = useSlugs();
  const organization = useOrganization();
  const project = useProject();
  const { hasAnyScope } = useRBAC();
  const location = useLocation();

  const toPreserve = Object.values(params).filter(Boolean);
  const allSubstitutions: Record<string, string | undefined> = {
    ...breadcrumbSubstitutions,
    ...substitutions,
  };

  // Build page-level breadcrumb elements from URL segments
  // For project-level pages (/:orgSlug/projects/:projectSlug/...), strip 3 leading segments
  // For org-level pages (/:orgSlug/...), strip 1 leading segment (just the orgSlug)
  const segmentsToStrip = projectSlug ? 3 : 1;
  const baseUrl = projectSlug
    ? `/${orgSlug}/projects/${projectSlug}`
    : `/${orgSlug}`;

  // Build URLs from ALL segments (so skipped segments are still in the path),
  // then filter out the ones we don't want to display.
  const allSegments = location.pathname
    .split("/")
    .filter(Boolean) // Remove empty strings
    .slice(segmentsToStrip);

  const pageElements = allSegments
    .map((segment, index) => {
      const relativeUrl = "/" + allSegments.slice(0, index + 1).join("/");

      // Decode for both display and param matching. `useParams()` returns
      // decoded values, so an encoded segment like adam%40speakeasy.com would
      // otherwise miss the toPreserve check and get JS-capitalized.
      let decoded = segment;
      try {
        decoded = decodeURIComponent(segment);
      } catch {
        // ignore malformed encodings; fall back to the raw segment
      }

      let display = decoded;
      // A substitution whose KEY is present but VALUE is still undefined means
      // the caller intends to replace this segment but the replacement isn't
      // ready yet (e.g. a name loaded from a query). Treat it as pending and
      // render a placeholder rather than flashing the raw id/slug first and the
      // real text a moment later.
      const subValue = allSubstitutions[segment] ?? allSubstitutions[decoded];
      const pending =
        subValue === undefined &&
        (segment in allSubstitutions || decoded in allSubstitutions);
      if (subValue !== undefined) {
        display = subValue;
      } else if (
        !pending &&
        !toPreserve.includes(decoded) &&
        !decoded.includes("@")
      ) {
        // Only synthesize a Title-Case display for the static parts of the
        // path. Route params (in toPreserve) and email-like identifiers are
        // dynamic slugs and keep their original casing.
        display = titleCaseSlug(decoded);
      }

      return {
        url: baseUrl + (breadcrumbUrlSubstitutions[relativeUrl] ?? relativeUrl),
        display,
        pending,
        isCurrentPage: location.pathname.endsWith(relativeUrl),
        skip: skipSegments.includes(segment),
      };
    })
    .filter((elem) => !elem.skip);

  // Build full breadcrumb list: {org} > [project >] page segments
  const canAccessOrg = hasAnyScope(["org:read", "org:admin"]);
  const visibleElements: {
    url: string;
    display: string;
    isCurrentPage: boolean;
    disableLink?: boolean;
    pending?: boolean;
  }[] = [];

  if (rootCrumbs) {
    // 1. Org name (always first; only clickable if user has org access)
    visibleElements.push({
      url: `/${orgSlug}`,
      display: organization.name || orgSlug || "Home",
      isCurrentPage: false,
      disableLink: !canAccessOrg,
    });

    // 2. Project name (only for project-level pages)
    if (projectSlug) {
      visibleElements.push({
        url: `/${orgSlug}/projects/${projectSlug}`,
        display: project.name || projectSlug || "Project",
        isCurrentPage: pageElements.length === 0,
      });
    } else if (pageElements.length === 0) {
      // Org root page — show "Home" as the current page
      visibleElements.push({
        url: `/${orgSlug}`,
        display: "Home",
        isCurrentPage: true,
      });
    }
  }

  // 3. Page segments
  visibleElements.push(...pageElements);

  return (
    <div className="flex w-full items-center justify-between gap-2">
      <PageHeader.Title
        className={cn(fullWidth ? "max-w-full" : "", className)}
      >
        <div className="flex items-center gap-2 normal-case">
          {visibleElements.map((elem, index) => (
            <React.Fragment key={`${elem.url}-${index}`}>
              <BreadcrumbCrumb elem={elem} />
              {index < visibleElements.length - 1 && (
                <ChevronRight
                  aria-hidden="true"
                  className="text-muted-foreground/60 size-3.5 shrink-0"
                />
              )}
            </React.Fragment>
          ))}
          {stage && <ReleaseStageBadge stage={stage} />}
        </div>
      </PageHeader.Title>
    </div>
  );
}

function PageHeaderActions({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <div className={cn("ml-auto flex shrink-0 items-center gap-2", className)}>
      {children}
    </div>
  );
}

export const PageHeader = Object.assign(PageHeaderComponent, {
  Title: PageHeaderTitle,
  Breadcrumbs: PageHeaderBreadcrumbs,
  Actions: PageHeaderActions,
});
