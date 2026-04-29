import { FullPageError } from "@/components/full-page-error";
import { GramLogo } from "@/components/gram-logo";
import { PageHeader } from "@/components/page-header";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import BookDemo from "@/pages/demo/BookDemo";
import { Icon } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useIsAdminRef } from "@/contexts/Sdk";
import { useEffect, useState } from "react";
import { ErrorBoundary } from "react-error-boundary";
import {
  Navigate,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router";
import { orgRoutePaths } from "@/routes";
import { useSlugs } from "./Sdk";
import {
  useCaptureUserAuthorizationEvent,
  useIdentifyUserForTelemetry,
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
} from "./Auth";
import type { ProjectEntry } from "@gram/client/models/components";

const PREFERRED_PROJECT_KEY = "preferredProject";

const UNAUTHENTICATED_PATHS = ["/login", "/register", "/invite", "/book-demo"];

const SLUG_EXEMPT_PATHS = ["/slack/register"];

export const AuthProvider = ({ children }: { children: React.ReactNode }) => {
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
  const isAdminRef = useIsAdminRef();

  const isLoading = status === "pending";

  useIdentifyUserForTelemetry(session?.user);
  usePylonInAppChat(session?.user);

  // Sync isAdmin into the SDK fetcher so it can attach X-Gram-Scope-Override in production.
  isAdminRef.current = session?.user.isAdmin ?? false;

  // you need something like this so you don't redirect with empty session too soon
  // isLoading is not synchronized with the session data actually being populated, so we need to wait for the session to actually finish loading
  // !! Very important that auth.info returns an error if there's no session
  if (isLoading || (!session && !error)) {
    // Don't show the authenticated app skeleton on routes that always redirect
    // (root "/" and unauthenticated pages like /login). This avoids a jarring
    // skeleton flash for logged-out users before the redirect to /login fires.
    if (
      location.pathname === "/" ||
      UNAUTHENTICATED_PATHS.some((p) => location.pathname.startsWith(p))
    ) {
      return null;
    }
    return <AppLoadingShell />;
  }

  if (error || !session || !session.session) {
    return (
      <SessionContext.Provider value={emptySession}>
        {children}
      </SessionContext.Provider>
    );
  }

  // Show book demo page if organization is not whitelisted
  // Check this before the no-org fallback so non-whitelisted orgs are blocked before reaching the normal app flow
  if (session.activeOrganizationId && !session.whitelisted) {
    return <BookDemo />;
  }

  if (!session.activeOrganizationId) {
    return (
      <SessionContext.Provider value={session}>
        {children}
      </SessionContext.Provider>
    );
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
  const isOrgRoutePath = ORG_ROUTE_PATHS.includes(pathParts[1]);
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

  // Handle initial navigation. Honor `returnTo` first, fall back to the legacy
  // `redirect` query param, so existing internal callers (CLI callback, Slack
  // register) keep working while the assistants growth-hack flow can use the
  // new `returnTo` name documented in its design diagram.
  const redirectParam =
    searchParams.get("returnTo") ?? searchParams.get("redirect");
  if (redirectParam) {
    return <Navigate to={redirectParam} replace />;
  } else if (isSlugExempt) {
    // Fall through to render children
  } else if (session.organization && !projectSlug) {
    // On an org-level page or bare URL with no project context — that's fine,
    // unless we're at the root "/" with no org slug either
    if (!orgSlug || orgSlug !== session.organization.slug) {
      // Promotable bare paths: marketing URLs like /onboarding?disposition=assistants
      // elide the org/project slugs. When a logged-in user lands on these paths,
      // prepend the org+project slugs and preserve the path/query/hash so the
      // project-scoped route can mount.
      const PROMOTABLE_BARE_PATHS = new Set<string>([
        "/onboarding",
        "/assistants-onboarding",
      ]);
      const isPromotable = PROMOTABLE_BARE_PATHS.has(location.pathname);
      const promotedSuffix = isPromotable
        ? location.pathname + location.search + location.hash
        : "";

      const preferredSlug = localStorage.getItem(PREFERRED_PROJECT_KEY);
      const preferredProject = preferredSlug
        ? session.organization.projects.find((p) => p.slug === preferredSlug)
        : undefined;
      // For promotable paths, fall back to projects[0] when there is no preferred
      // project. Covers the post-register case where the user just got their
      // first project and has no localStorage preference yet.
      const targetProject =
        preferredProject ??
        (isPromotable ? session.organization.projects[0] : undefined);

      if (targetProject) {
        return (
          <Navigate
            to={`/${session.organization.slug}/projects/${targetProject.slug}${promotedSuffix}`}
            replace
          />
        );
      }
      // Redirect to org home (no project context — a promoted suffix would be unmountable)
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
}) => {
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
    navigate(`/${organization.slug}/projects/${slug}`);
  };

  const value = Object.assign(currentProject, {
    organizationId: organization.id,
    switchProject,
  });

  return (
    <ProjectContext.Provider value={value}>{children}</ProjectContext.Provider>
  );
};

/** Static nav items matching the real sidebar groups — no auth context needed. */
const LOADING_NAV = {
  project: [{ label: "Home", icon: "house" as const }],
  connect: [
    { label: "Sources", icon: "file-code" as const },
    { label: "Catalog", icon: "store" as const },
    { label: "Playground", icon: "message-circle" as const },
  ],
  build: [
    { label: "Chat Elements", icon: "message-circle" as const },
    { label: "MCP", icon: "network" as const },
    { label: "Assistants", icon: "bot" as const },
    { label: "Skills", icon: "terminal" as const },
  ],
  observe: [
    { label: "Insights", icon: "layout-dashboard" as const },
    { label: "MCP Logs", icon: "file-text" as const },
    { label: "Agent Sessions", icon: "messages-square" as const },
    { label: "Hooks", icon: "webhook" as const },
  ],
  settings: [{ label: "Settings", icon: "settings" as const }],
};

/**
 * Lightweight shell that mirrors the real AppLayout structure,
 * shown while the auth session is still loading so the user
 * sees the app chrome immediately instead of a blank screen.
 */
const AppLoadingShell = () => (
  <SidebarProvider
    style={{ "--sidebar-width": "14rem" } as React.CSSProperties}
  >
    <div className="flex h-screen w-full flex-col">
      {/* Header */}
      <header className="dark:bg-background flex h-14 shrink-0 items-center border-b bg-white pr-4 pl-5">
        <div className="flex items-center gap-3">
          <GramLogo className="w-28" />
          <span className="text-muted-foreground/50 text-xl select-none">
            /
          </span>
          <Skeleton className="h-5 w-24" />
          <span className="text-muted-foreground/50 text-xl select-none">
            /
          </span>
          <Skeleton className="h-5 w-20" />
        </div>
        <div className="ml-auto flex items-center gap-4">
          <Skeleton className="h-8 w-8 rounded-full" />
        </div>
      </header>
      {/* Body */}
      <div className="flex w-full flex-1 overflow-hidden pt-2">
        <Sidebar collapsible="offcanvas" variant="inset">
          <SidebarContent className="pt-2">
            {Object.entries(LOADING_NAV).map(([group, items]) => (
              <SidebarGroup key={group}>
                <SidebarGroupLabel className="text-sidebar-foreground">
                  {group}
                </SidebarGroupLabel>
                <SidebarGroupContent>
                  <SidebarMenu>
                    {items.map((item) => (
                      <SidebarMenuItem key={item.label}>
                        <SidebarMenuButton>
                          <Icon
                            name={item.icon}
                            className="text-muted-foreground"
                          />
                          <Type variant="small">{item.label}</Type>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    ))}
                  </SidebarMenu>
                </SidebarGroupContent>
              </SidebarGroup>
            ))}
          </SidebarContent>
        </Sidebar>
        <SidebarInset>
          <PageHeader>
            <PageHeader.Breadcrumbs />
            <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
          </PageHeader>
        </SidebarInset>
      </div>
    </div>
  </SidebarProvider>
);
