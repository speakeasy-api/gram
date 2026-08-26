import { FullPageError } from "@/components/full-page-error";
import { GramLogo } from "@/components/gram-logo";
import { HatchRule } from "@/components/hatch-rule";
import { SidebarNavSkeleton } from "@/components/sidebar-nav-skeleton";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarInset,
  SidebarProvider,
} from "@/components/ui/Sidebar";
import { Skeleton } from "@/components/ui/Skeleton";
import BookDemo from "@/pages/demo/BookDemo";
import SwitchOrg from "@/pages/demo/SwitchOrg";
import { getTrialLifecycleFromDates } from "@/lib/trial-status";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useIsPlatformAdminRef } from "@/contexts/Sdk";
import { useEffect, useState } from "react";
import { ErrorBoundary } from "react-error-boundary";
import {
  Navigate,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router";
import { orgRoutePaths } from "@/routes";
import { isPortablePath, resolvePortablePath } from "@/lib/portable-path";
import { safeRedirectPath, UNAUTHENTICATED_PATHS } from "@/lib/session-expired";
import { useSlugs } from "./Sdk";
import {
  useCaptureUserAuthorizationEvent,
  useIdentifyUserForTelemetry,
  useRegisterOrganizationForTelemetry,
  useRegisterProjectForTelemetry,
} from "./Telemetry";
import {
  SessionContext,
  ProjectContext,
  emptySession,
  emptyProject,
  useOrganization,
  useSessionData,
  useUser,
  usePylonInAppChat,
  useFermatPixel,
} from "./Auth";
import type { ProjectEntry } from "@gram/client/models/components/projectentry.js";

const PREFERRED_PROJECT_KEY = "preferredProject";

const SLUG_EXEMPT_PATHS = [
  "/switch-org",
  "/explore-demo",
  "/guide",
  "/talk-to-us",
  "/shadow-mcp/request",
  "/risk-policy-bypass/request",
  "/risk-policy-challenge/acknowledge",
  "/blocks",
  "/shared",
];

// Exact match, with or without the trailing slash. A prefix match would let
// deeper paths (e.g. /explore-demo/projects/x) through the gate.
function isPath(pathname: string, path: string): boolean {
  return pathname === path || pathname === `${path}/`;
}

export const AuthProvider = ({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element => {
  return (
    <ErrorBoundary FallbackComponent={FullPageError}>
      <AuthHandler>{children}</AuthHandler>
    </ErrorBoundary>
  );
};

const AuthHandler = ({ children }: { children: React.ReactNode }) => {
  const { orgSlug, projectSlug } = useSlugs();
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const { session, error, status } = useSessionData();
  const isPlatformAdminRef = useIsPlatformAdminRef();

  const isLoading = status === "pending";

  useIdentifyUserForTelemetry(session?.user);
  // Runs above every gate below, including the ones that return before
  // ProjectProvider (and its own group registration) can mount, so
  // organization-targeted feature flags resolve on the lockout pages too.
  useRegisterOrganizationForTelemetry(session?.organization?.slug ?? "");
  usePylonInAppChat(session?.user);
  useFermatPixel(session?.user, session?.activeOrganizationId ?? "");

  // Sync isAdmin into the SDK fetcher so it can attach X-Gram-Scope-Override in production.
  isPlatformAdminRef.current = session?.user.isAdmin ?? false;

  // you need something like this so you don't redirect with empty session too soon
  // isLoading is not synchronized with the session data actually being populated, so we need to wait for the session to actually finish loading
  // !! Very important that auth.info returns an error if there's no session
  if (isLoading || (!session && !error)) {
    // Don't show the authenticated app skeleton on routes that always redirect
    // (root "/" and unauthenticated pages like /login). This avoids a jarring
    // skeleton flash for logged-out users before the redirect to /login fires.
    // A minimal centered pending state (not a blank viewport) covers the
    // seconds the session check can take before login paints.
    if (
      location.pathname === "/" ||
      UNAUTHENTICATED_PATHS.some((p) => location.pathname.startsWith(p)) ||
      location.pathname.endsWith("/setup")
    ) {
      return <AuthPendingScreen />;
    }
    return <AppLoadingShell />;
  }

  // A portable "/~" path (an external link that cannot know the viewer's
  // slugs) matches no route, so the gates below must resolve it before route
  // matching gets a say. Logged out it bounces through login carrying the
  // full destination — the same shape LoginCheck produces for slugged paths.
  const portableRedirect = isPortablePath(location.pathname)
    ? encodeURIComponent(location.pathname + location.search + location.hash)
    : undefined;

  if (error || !session || !session.session) {
    if (portableRedirect) {
      return <Navigate to={`/login?redirect=${portableRedirect}`} replace />;
    }
    return (
      <SessionContext.Provider value={emptySession}>
        {children}
      </SessionContext.Provider>
    );
  }

  // Show book demo page if organization is not whitelisted
  // Check this before the no-org fallback so non-whitelisted orgs are blocked before reaching the normal app flow
  // /explore-demo stays reachable: it's the gate page's own escape hatch into
  // the shared demo org (which is whitelisted).
  if (
    session.activeOrganizationId &&
    !session.whitelisted &&
    !isPath(location.pathname, "/explore-demo")
  ) {
    // The switcher wins even on the upgrade gate's own route. Someone with a
    // second organization has somewhere to go, and that page offers no way to
    // reach it — landing there would strand them.
    if (session.organizations.length > 1) {
      return <SwitchOrg gate />;
    }
    // Past this point the upgrade gate has to render, or the redirect below
    // sends the user to a route that bounces them straight back to it.
    if (!isPath(location.pathname, "/talk-to-us")) {
      // An org that never trialed (or is still mid-trial) falls through to the
      // cold-signup gate.
      if (getTrialLifecycleFromDates(session.trial, new Date()) === "expired") {
        return <Navigate to="/talk-to-us" replace />;
      }
      return <BookDemo />;
    }
  }

  if (!session.activeOrganizationId) {
    if (portableRedirect) {
      return <Navigate to={`/sign-up?redirect=${portableRedirect}`} replace />;
    }
    return (
      <SessionContext.Provider value={session}>
        {children}
      </SessionContext.Provider>
    );
  }

  // Fully authenticated: expand "/~" into the active org and the project the
  // user last visited, keeping the destination's own query and hash.
  if (session.organization) {
    const resolved = resolvePortablePath(
      location,
      session.organization,
      localStorage.getItem(PREFERRED_PROJECT_KEY),
    );
    if (resolved) {
      return <Navigate to={resolved} replace />;
    }
  }

  // Skip all slug-based redirect logic for exempt paths
  const isSlugExempt = SLUG_EXEMPT_PATHS.some((p) =>
    location.pathname.startsWith(p),
  );

  const pathParts = location.pathname.split("/").filter(Boolean);

  // Backwards-compat: redirect old /:orgSlug/:projectSlug/... URLs to /:orgSlug/projects/:projectSlug/...
  // If the second segment is a known project slug (and not "projects" or an org-level route),
  // redirect to the new URL structure.
  // Derived from org route structure so new org routes are automatically excluded from project slug redirects
  const ORG_ROUTE_PATHS = ["projects", ...orgRoutePaths];
  const isProjectSlug = session.organization?.projects.some(
    (p) => p.slug === pathParts[1],
  );
  const isOrgRoutePath = ORG_ROUTE_PATHS.includes(pathParts[1] ?? "");
  // Redirect if: (1) it's a project slug and not an org route, OR
  // (2) it's both a project slug and an org route but has sub-paths (org routes don't have sub-paths)
  // Never redirect if pathParts[1] is "projects" to avoid infinite redirect loops
  if (
    !isSlugExempt &&
    pathParts.length >= 2 &&
    pathParts[0] === session.organization?.slug &&
    pathParts[1] !== "projects" &&
    isProjectSlug &&
    (!isOrgRoutePath || pathParts.length >= 3)
  ) {
    const rest = pathParts.slice(2).join("/");
    const newPath = `/${pathParts[0]}/projects/${pathParts[1]}${rest ? `/${rest}` : ""}`;
    return <Navigate to={newPath + location.search + location.hash} replace />;
  }

  // Handle initial navigation. The param is attacker-controllable, so only
  // same-origin paths are honored — a protocol-relative value would send the
  // freshly authenticated user to a foreign origin.
  const redirectParam = safeRedirectPath(searchParams.get("redirect"));
  if (redirectParam) {
    return <Navigate to={redirectParam} replace />;
  } else if (isSlugExempt) {
    // Fall through to render children
  } else if (session.organization && !projectSlug) {
    // On an org-level page or bare URL with no project context — that's fine,
    // unless we're at the root "/" with no org slug either
    if (!orgSlug || orgSlug !== session.organization.slug) {
      // If the user has a preferred project, redirect to it instead of org home
      const preferredSlug = localStorage.getItem(PREFERRED_PROJECT_KEY);
      const preferredProject = preferredSlug
        ? session.organization.projects.find((p) => p.slug === preferredSlug)
        : undefined;
      if (preferredProject) {
        return (
          <Navigate
            to={`/${session.organization.slug}/projects/${preferredProject.slug}`}
            replace
          />
        );
      }
      // Redirect to org home
      return <Navigate to={`/${session.organization.slug}`} replace />;
    }
    // Otherwise we're on a valid org-level path, fall through
  } else if (session.organization.slug !== orgSlug) {
    // make sure we don't direct to an org we aren't authenticated with
    return (
      <Navigate
        to={`/${session.organization.slug}/projects/${projectSlug}`}
        replace
      />
    );
  }

  return (
    <SessionContext.Provider value={session}>
      {children}
    </SessionContext.Provider>
  );
};

export const ProjectProvider = ({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element => {
  const organization = useOrganization();
  const user = useUser();
  const navigate = useNavigate();
  const client = useQueryClient();

  const { projectSlug } = useSlugs();
  const [project, setProject] = useState<ProjectEntry | null>(null);

  // Fall back to the user's most recently used project, then to the first project
  const preferredSlug = localStorage.getItem(PREFERRED_PROJECT_KEY);
  const preferredProject = preferredSlug
    ? organization.projects.find((p) => p.slug === preferredSlug)
    : undefined;
  const defaultProject = preferredProject ?? organization.projects[0];

  const currentProject =
    organization.projects.find((p) => p.slug === projectSlug) ?? defaultProject;

  useRegisterProjectForTelemetry({
    projectId: currentProject?.id ?? "",
    projectSlug: currentProject?.slug ?? "",
    organizationSlug: organization.slug,
  });

  useCaptureUserAuthorizationEvent({
    projectId: currentProject?.id ?? "",
    projectSlug: currentProject?.slug ?? "",
    organizationSlug: organization.slug,
    email: user.email,
  });

  // Store the last project the user visited so we can redirect to it
  useEffect(() => {
    if (currentProject) {
      localStorage.setItem(PREFERRED_PROJECT_KEY, currentProject.slug);
    }
  }, [currentProject]);

  // Update project state when current project changes
  useEffect(() => {
    if (!project || project.slug !== currentProject?.slug) {
      setProject(currentProject ?? null);
    }
  }, [currentProject, project]);

  // Not logged in
  if (!currentProject) {
    return (
      <ProjectContext.Provider value={emptyProject}>
        {children}
      </ProjectContext.Provider>
    );
  }

  const switchProject = async (slug: string) => {
    client.clear();
    void navigate(`/${organization.slug}/projects/${slug}`);
  };

  const value = Object.assign(currentProject, {
    organizationId: organization.id,
    switchProject,
  });

  return (
    <ProjectContext.Provider value={value}>{children}</ProjectContext.Provider>
  );
};

/**
 * Minimal centered pending state shown while the session check resolves on
 * routes that always redirect (root "/", /login, setup). Mirrors the
 * thin-serif treatment CliCallback uses; the copy stays a neutral "Loading…"
 * because on /login itself no redirect is coming.
 */
const AuthPendingScreen = () => (
  <div className="flex h-screen items-center justify-center">
    <h1 className="text-display-sm font-thin">Loading…</h1>
  </div>
);

/**
 * Lightweight shell that mirrors the real AppLayout structure,
 * shown while the auth session is still loading so the user
 * sees the app chrome immediately instead of a blank screen.
 *
 * Keep the structure in sync with AppLayout/AppSidebar: the logo belongs
 * inside SidebarHeader, not a sibling header. The sidebar renders as a
 * `fixed inset-y-0 z-10` container, so a sibling header would sit under it.
 */
const AppLoadingShell = () => (
  <SidebarProvider
    style={{ "--sidebar-width": "16rem" } as React.CSSProperties}
  >
    <div className="flex h-screen w-full flex-col">
      <div className="flex w-full flex-1 overflow-hidden">
        <Sidebar collapsible="icon">
          {/* Logo row + crosshatch rule, exactly as AppSidebar renders it —
              the switcher lives in the page header now, so no placeholder
              for it here. */}
          <SidebarHeader className="gap-0 p-0">
            <div className="flex h-(--header-height) items-center px-2">
              <div className="flex h-full items-center px-1">
                <GramLogo className="w-28" />
              </div>
            </div>
            <HatchRule />
          </SidebarHeader>
          <SidebarContent className="pt-2">
            <SidebarNavSkeleton
              rows={8}
              divideAfter={3}
              className="gap-0.5 px-2 group-data-[collapsible=icon]:px-0"
            />
          </SidebarContent>
          <SidebarFooter className="border-t">
            <div className="flex items-center gap-2 py-2">
              <Skeleton className="size-8 shrink-0 rounded-full" />
              <Skeleton className="h-3.5 w-24" />
            </div>
          </SidebarFooter>
        </Sidebar>
        <SidebarInset>
          {/* Mirrors PageHeader's own geometry (h-(--header-height), px-8,
              hatch rule) without mounting it: the switcher inside needs the
              auth context this shell is still waiting on. */}
          <header className="flex h-(--header-height) shrink-0 items-center gap-3 px-8">
            <Skeleton className="h-6 w-40" />
            <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
          </header>
          <HatchRule />
        </SidebarInset>
      </div>
    </div>
  </SidebarProvider>
);
