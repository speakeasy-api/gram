import { isBillingRecoveryPath } from "@/lib/billing-recovery-path";
import { FullPageError } from "@/components/full-page-error";
import { SplashScreen } from "@/components/ui/LogoLoader";
import BookDemo from "@/pages/demo/BookDemo";
import SwitchOrg from "@/pages/demo/SwitchOrg";
import { useOrganizationHostMove } from "@/hooks/useOrganizationHostMove";
import { useTrialNow } from "@/hooks/useTrialNow";
import { getTrialLifecycleFromDates } from "@/lib/trial-status";
import {
  organizationHostRedirectTarget,
  recordOrganizationSwitch,
  switchedRecently,
} from "@/lib/organization-host";
import { isGramSessionUnauthorizedError } from "@/lib/route-errors";
import { useQueryClient } from "@tanstack/react-query";
import { useIsPlatformAdminRef, useSdkClient } from "@/contexts/Sdk";
import {
  capturePreservedStorage,
  setPreservedStorageImpersonating,
} from "@/lib/logout-storage";
import { useEffect, useRef, useState } from "react";
import { ErrorBoundary } from "react-error-boundary";
import {
  Navigate,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router";
import { orgRoutePaths } from "@/routes";
import { isPortablePath, resolvePortablePath } from "@/lib/portable-path";
import { isServerRenderedPath, safeRedirectPath } from "@/lib/session-expired";
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
const ORGANIZATION_SCOPE_SWITCH_KEY = "organizationScopeSwitchAttempt";

const SLUG_EXEMPT_PATHS = [
  "/switch-org",
  "/explore-demo",
  "/guide",
  "/trial-ended",
  "/shadow-mcp/request",
  "/risk-policy-bypass/request",
  "/risk-policy-challenge/acknowledge",
  "/ack",
  "/blocks",
  "/shared",
];

// Exact match, with or without the trailing slash. A prefix match would let
// deeper paths (e.g. /explore-demo/projects/x) through the gate.
function isPath(pathname: string, path: string): boolean {
  return pathname === path || pathname === `${path}/`;
}

/**
 * Whether an org-relative path is one of the org's own routes. Route paths
 * carry dynamic segments ("setup/:taskSlug"), so a string comparison would
 * miss "setup/idp" and hand it to the legacy project redirect for any org
 * that also has a project slugged "setup".
 */
function matchesOrgRoutePath(routePath: string, actual: string): boolean {
  const route = routePath.split("/");
  const parts = actual.split("/");
  return (
    route.length === parts.length &&
    route.every(
      (segment, index) => segment.startsWith(":") || segment === parts[index],
    )
  );
}

function organizationScopeSwitchAttempt(
  organizationId: string,
  destination: string,
): string {
  return JSON.stringify({ organizationId, destination });
}

function organizationScopeSwitchTarget(
  value: string | null,
): string | undefined {
  if (!value) return undefined;
  try {
    const parsed = JSON.parse(value) as unknown;
    if (
      typeof parsed === "object" &&
      parsed !== null &&
      "organizationId" in parsed &&
      typeof parsed.organizationId === "string"
    ) {
      return parsed.organizationId;
    }
  } catch {
    // Invalid or stale marker.
  }
  return undefined;
}

function clearOrganizationScopeSwitchAttempt(attempt: string): void {
  try {
    if (sessionStorage.getItem(ORGANIZATION_SCOPE_SWITCH_KEY) === attempt) {
      sessionStorage.removeItem(ORGANIZATION_SCOPE_SWITCH_KEY);
    }
  } catch {
    // sessionStorage unavailable
  }
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
  const trialNow = useTrialNow(session?.trial);
  const isPlatformAdminRef = useIsPlatformAdminRef();

  const isLoading = status === "pending";

  useEffect(() => {
    if (!session?.activeOrganizationId) return;
    try {
      const attempt = sessionStorage.getItem(ORGANIZATION_SCOPE_SWITCH_KEY);
      if (
        organizationScopeSwitchTarget(attempt) === session.activeOrganizationId
      ) {
        sessionStorage.removeItem(ORGANIZATION_SCOPE_SWITCH_KEY);
      }
    } catch {
      // sessionStorage unavailable
    }
  }, [session?.activeOrganizationId]);

  useIdentifyUserForTelemetry(session?.user);
  // Runs above every gate below, including the ones that return before
  // ProjectProvider (and its own group registration) can mount, so
  // organization-targeted feature flags resolve on the lockout pages too.
  useRegisterOrganizationForTelemetry(session?.organization?.slug ?? "");
  usePylonInAppChat(session?.user);
  useFermatPixel(session?.user, session?.activeOrganizationId ?? "");

  // Sync isAdmin into the SDK fetcher so it can attach X-Gram-Scope-Override in production.
  isPlatformAdminRef.current = session?.user.isAdmin ?? false;

  // Snapshot theme/favorites only for a normal session. Impersonation and
  // support access must not refresh it, or logout would restore the
  // customer org's keys instead of the admin's. The flag is set during
  // render so child effects (favorites, theme) cannot capture first.
  const isImpersonating =
    Boolean(session?.impersonatorEmail) ||
    Boolean(session?.organizationOverride);
  // Only classify when a session is present. A disappearing session (401 /
  // expiry before /login) must not flip the flag to false — that would let
  // session-expiry cleanup live-capture the impersonated org's keys.
  if (session?.session) {
    setPreservedStorageImpersonating(isImpersonating);
  }
  useEffect(() => {
    if (session?.session && !isImpersonating) {
      capturePreservedStorage();
    }
  }, [session?.session, isImpersonating]);

  // An organization that lives on another platform host is used there, on the
  // same page. auth.info names that host only for an ordinary session whose
  // active organization belongs elsewhere; session cookies are host-only, so
  // the session is handed over with a session transfer.
  // A URL naming another of the user's organizations switches scope first;
  // the move then follows the organization the switch selects.
  const urlNamesOtherOrganization = Boolean(
    session?.organizations.some(
      (organization) =>
        organization.slug === orgSlug &&
        organization.id !== session.activeOrganizationId,
    ),
  );
  const sessionAuthorized =
    Boolean(session?.session) && !isGramSessionUnauthorizedError(error);
  const organizationHostTarget =
    sessionAuthorized &&
    session?.activeOrganizationId &&
    !isImpersonating &&
    !urlNamesOtherOrganization
      ? organizationHostRedirectTarget(
          session.activeOrganizationDashboardUrl,
          window.location,
          switchedRecently(session.activeOrganizationId),
        )
      : undefined;
  const movingHost = useOrganizationHostMove(
    session?.activeOrganizationId,
    organizationHostTarget,
  );

  // you need something like this so you don't redirect with empty session too soon
  // isLoading is not synchronized with the session data actually being populated, so we need to wait for the session to actually finish loading
  // !! Very important that auth.info returns an error if there's no session
  if (isLoading || (!session && !error)) {
    return <SplashScreen fullScreen />;
  }

  // A portable "/@self" path (an external link that cannot know the viewer's
  // slugs) matches no route, so the gates below must resolve it before route
  // matching gets a say. Logged out it bounces through login carrying the
  // full destination — the same shape LoginCheck produces for slugged paths.
  const portableRedirect = isPortablePath(location.pathname)
    ? encodeURIComponent(location.pathname + location.search + location.hash)
    : undefined;

  if (isGramSessionUnauthorizedError(error) || !session || !session.session) {
    if (portableRedirect) {
      return <Navigate to={`/login?redirect=${portableRedirect}`} replace />;
    }
    return (
      <SessionContext.Provider value={emptySession}>
        {children}
      </SessionContext.Provider>
    );
  }

  if (movingHost) {
    return <SplashScreen fullScreen />;
  }

  // Show book demo page if organization is not whitelisted
  // Check this before the no-org fallback so non-whitelisted orgs are blocked before reaching the normal app flow
  // /explore-demo stays reachable: it's the gate page's own escape hatch into
  // the shared demo org (which is whitelisted).
  if (
    session.activeOrganizationId &&
    !session.whitelisted &&
    !isBillingRecoveryPath(
      location.pathname,
      location.search,
      session.organization?.slug,
    ) &&
    !isPath(location.pathname, "/explore-demo")
  ) {
    // The switcher wins even on the upgrade gate's own route. Someone with a
    // second organization has somewhere to go, and that page offers no way to
    // reach it — landing there would strand them.
    if (session.organizations.length > 1) {
      return <SwitchOrg gate />;
    }
    const trialLifecycle = getTrialLifecycleFromDates(session.trial, trialNow);

    if (trialLifecycle === "expired") {
      if (!isPath(location.pathname, "/trial-ended")) {
        return <Navigate to="/trial-ended" replace />;
      }
    } else {
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

  // Fully authenticated: expand "/@self" into the active org, keeping the
  // destination's own query and hash.
  if (session.organization) {
    const resolved = resolvePortablePath(location, session.organization);
    if (resolved) {
      return <Navigate to={resolved} replace />;
    }
  }

  // Skip all slug-based redirect logic for exempt paths
  // Match whole path segments, so an org slug that only starts with an
  // exempt path (e.g. /ackermann for /ack) still gets slug handling.
  const isSlugExempt = SLUG_EXEMPT_PATHS.some(
    (p) => location.pathname === p || location.pathname.startsWith(`${p}/`),
  );

  const pathParts = location.pathname.split("/").filter(Boolean);

  // Backwards-compat: redirect old /:orgSlug/:projectSlug/... URLs to
  // /:orgSlug/projects/:projectSlug/... while preserving exact org routes.
  const ORG_ROUTE_PATHS = ["projects", ...orgRoutePaths];
  const isProjectSlug = session.organization?.projects.some(
    (p) => p.slug === pathParts[1],
  );
  const orgRelativePath = pathParts.slice(1).join("/");
  const isExactOrgRoutePath = ORG_ROUTE_PATHS.some((routePath) =>
    matchesOrgRoutePath(routePath, orgRelativePath),
  );
  if (
    !isSlugExempt &&
    pathParts.length >= 2 &&
    pathParts[0] === session.organization?.slug &&
    pathParts[1] !== "projects" &&
    isProjectSlug &&
    !isExactOrgRoutePath
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
    if (isServerRenderedPath(redirectParam)) {
      return <ServerRenderedRedirect to={redirectParam} />;
    }
    return <Navigate to={redirectParam} replace />;
  } else if (isSlugExempt) {
    // Fall through to render children
  } else if (orgSlug && session.organization.slug !== orgSlug) {
    const requestedOrganization = session.organizations.find(
      (organization) => organization.slug === orgSlug,
    );
    const requestedProjectSlug =
      projectSlug ??
      (!isExactOrgRoutePath && pathParts[1] !== "projects"
        ? pathParts[1]
        : undefined);
    const requestedProjectExists =
      !requestedProjectSlug ||
      requestedOrganization?.projects.some(
        (project) => project.slug === requestedProjectSlug,
      );
    const destination = safeRedirectPath(
      location.pathname + location.search + location.hash,
    );

    if (requestedOrganization && requestedProjectExists && destination) {
      return (
        <OrganizationScopeSwitch
          organizationId={requestedOrganization.id}
          destination={destination}
          fallbackDestination={`/${session.organization.slug}`}
        />
      );
    }

    // Never combine the active organization with a project slug from an
    // inaccessible or stale cross-organization link.
    return <Navigate to={`/${session.organization.slug}`} replace />;
  } else if (session.organization && !projectSlug) {
    // On an org-level page or bare URL with no project context — that's fine,
    // unless we're at the root "/" with no org slug either
    if (!orgSlug) {
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
  }

  return (
    <SessionContext.Provider value={session}>
      {children}
    </SessionContext.Provider>
  );
};

// A return target the server renders (see isServerRenderedPath) is loaded
// with a full navigation. A router <Navigate> would keep the dashboard in
// charge of a path it has no route for.
function ServerRenderedRedirect({ to }: { to: string }): JSX.Element {
  useEffect(() => {
    window.location.replace(to);
  }, [to]);
  return <SplashScreen fullScreen />;
}

function OrganizationScopeSwitch({
  organizationId,
  destination,
  fallbackDestination,
}: {
  organizationId: string;
  destination: string;
  fallbackDestination: string;
}): JSX.Element {
  const client = useSdkClient();
  const requestRef = useRef<
    | {
        attempt: string;
        promise: ReturnType<typeof client.auth.switchScopes>;
      }
    | undefined
  >(undefined);
  const attempt = organizationScopeSwitchAttempt(organizationId, destination);
  const [alreadyAttempted] = useState(() => {
    try {
      return sessionStorage.getItem(ORGANIZATION_SCOPE_SWITCH_KEY) === attempt;
    } catch {
      return false;
    }
  });
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    if (alreadyAttempted) {
      try {
        sessionStorage.removeItem(ORGANIZATION_SCOPE_SWITCH_KEY);
      } catch {
        // sessionStorage unavailable
      }
      return;
    }

    let cancelled = false;
    let request = requestRef.current;
    if (!request || request.attempt !== attempt) {
      try {
        sessionStorage.setItem(ORGANIZATION_SCOPE_SWITCH_KEY, attempt);
      } catch {
        // sessionStorage unavailable; the in-memory request still prevents duplicates.
      }
      request = {
        attempt,
        promise: client.auth.switchScopes({ organizationId }),
      };
      requestRef.current = request;
    }

    void request.promise
      .then(() => {
        recordOrganizationSwitch(organizationId);
        if (cancelled) {
          clearOrganizationScopeSwitchAttempt(attempt);
          return;
        }
        window.location.replace(destination);
      })
      .catch((switchError: unknown) => {
        clearOrganizationScopeSwitchAttempt(attempt);
        if (!cancelled) setError(switchError);
      });

    return () => {
      cancelled = true;
    };
  }, [alreadyAttempted, attempt, client, destination, organizationId]);

  if (alreadyAttempted) {
    return <Navigate to={fallbackDestination} replace />;
  }
  if (error) {
    return <FullPageError error={error} />;
  }
  return <SplashScreen fullScreen />;
}

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
