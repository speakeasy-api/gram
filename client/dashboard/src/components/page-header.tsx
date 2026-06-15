// oxlint-disable react/only-export-components -- compound component (Object.assign) pattern
import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { useOrganization, useProject } from "@/contexts/Auth.tsx";
import { useSlugs } from "@/contexts/Sdk.tsx";
import { useRBAC } from "@/hooks/useRBAC";
import { cn, titleCaseSlug } from "@/lib/utils.ts";
import React from "react";
import { Link, useLocation, useParams } from "react-router";
import { BrandGradientLine } from "./brand-gradient-line.tsx";
import { InsightsDockShortcutHint } from "./insights-dock-shortcut-hint.tsx";
import { OnboardingBanner } from "./onboarding-banner.tsx";
import { ReleaseStage, ReleaseStageBadge } from "./release-stage-badge.tsx";
import { Heading } from "./ui/heading.tsx";

function PageHeaderComponent({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <>
      <header
        className={cn(
          "flex h-(--header-height) shrink-0 items-center gap-2 transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-(--header-height)",
          className,
        )}
      >
        <div className="flex w-full items-center gap-3 px-3">
          <SidebarTrigger className="mx-0 -ml-1 px-0" />
          <Separator
            orientation="vertical"
            className="data-[orientation=vertical]:h-4"
          />
          {children}
        </div>
      </header>
      {/* Brand gradient signature, relocated here from the old top bar — it now
          divides the main panel's header from its content on the right side. */}
      <BrandGradientLine />
      <OnboardingBanner />
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
      className={cn("mx-auto ml-1 w-full max-w-[1270px]", className)}
    >
      {children}
    </Heading>
  );
}

// Static path segments are auto Title-Cased (see `titleCaseSlug`), so this map
// only needs to hold the exceptions where that produces the wrong text:
//   - acronyms / non-standard casing (MCP, SDKs, OpenAPI, API)
//   - lowercased connector words ("from")
//   - rebrands where the display name differs from the URL segment. `slack` and
//     `clis` are kept in the URL for backwards compatibility but were renamed.
const breadcrumbSubstitutions = {
  mcp: "MCP",
  sdks: "SDKs",
  elements: "Chat Elements",
  "add-openapi": "Add OpenAPI",
  "add-from-catalog": "Add from Catalog",
  "api-keys": "API Keys",
  slack: "Assistants",
  clis: "Skills",
};

function PageHeaderBreadcrumbs({
  fullWidth,
  className,
  substitutions = {}, // Any segment and how it should be displayed, for example toolset slug -> toolset name
  skipSegments = [], // Segments to skip/hide from breadcrumbs
  stage,
}: {
  fullWidth?: boolean;
  className?: string;
  substitutions?: Record<string, string | undefined>;
  skipSegments?: string[];
  stage?: ReleaseStage;
}) {
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
      const subSegment = allSubstitutions[segment];
      const subDecoded = allSubstitutions[decoded];
      if (subSegment !== undefined) {
        display = subSegment;
      } else if (subDecoded !== undefined) {
        display = subDecoded;
      } else if (!toPreserve.includes(decoded) && !decoded.includes("@")) {
        // Only synthesize a Title-Case display for the static parts of the
        // path. Route params (in toPreserve) and email-like identifiers are
        // dynamic slugs and keep their original casing.
        display = titleCaseSlug(decoded);
      }

      return {
        url: baseUrl + relativeUrl,
        display,
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
  }[] = [];

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

  // 3. Page segments
  visibleElements.push(...pageElements);

  return (
    <PageHeader.Title className={cn(fullWidth ? "max-w-full" : "", className)}>
      <div className="ml-auto flex items-center gap-2 normal-case">
        {visibleElements.map((elem, index) => (
          <React.Fragment key={`${elem.url}-${index}`}>
            {elem.isCurrentPage || elem.disableLink ? (
              <span
                className={
                  elem.isCurrentPage ? undefined : "text-muted-foreground"
                }
              >
                {elem.display}
              </span>
            ) : (
              <Link
                to={elem.url}
                className="text-muted-foreground hover:text-foreground trans"
              >
                {elem.display}
              </Link>
            )}
            {index < visibleElements.length - 1 && (
              <span className="text-muted-foreground"> / </span>
            )}
          </React.Fragment>
        ))}
        {stage && <ReleaseStageBadge stage={stage} />}
        {/* Cmd+/ hint for the docked Project Assistant composer — lives here
            (rather than inside the dock's input) so the pill stays clean. */}
        <InsightsDockShortcutHint className="ml-auto" />
      </div>
    </PageHeader.Title>
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
