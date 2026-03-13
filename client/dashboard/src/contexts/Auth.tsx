import { FullPageError } from "@/components/full-page-error";
import {
  InfoResponseBody,
  OrganizationEntry,
  ProjectEntry,
} from "@gram/client/models/components";
import { SessionInfoResponse } from "@gram/client/models/operations";
import { useSessionInfo } from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useState } from "react";
import { ErrorBoundary } from "react-error-boundary";
import {
  Navigate,
  useLocation,
  useNavigate,
  useSearchParams,
} from "react-router";
import { useSlugs } from "./Sdk";
import {
  useCaptureUserAuthorizationEvent,
  useIdentifyUserForTelemetry,
  useRegisterProjectForTelemetry,
} from "./Telemetry";

// We don't include accountType here because it is actively confusing. See useProductTier
type Session = Omit<
  InfoResponseBody,
  "userEmail" | "userId" | "isAdmin" | "gramAccountType"
> & {
  user: User;
  session: string;
  organization: OrganizationEntry;
  rawGramAccountType: string; // "raw" -- should not be used directly unless you know what you are doing
  refetch: () => Promise<Session>;
};

export type User = {
  id: string;
  email: string;
  isAdmin: boolean;
  signature?: string;
  displayName?: string;
  photoUrl?: string;
};

const emptyOrganization: OrganizationEntry = {
  id: "",
  name: "",
  slug: "",
  projects: [],
};

const emptySession: Session = {
  user: {
    id: "",
    email: "",
    isAdmin: false,
  },
  organizations: [],
  activeOrganizationId: "",
  session: "",
  rawGramAccountType: "",
  organization: emptyOrganization,
  refetch: () => Promise.resolve(emptySession),
};

const PREFERRED_PROJECT_KEY = "preferredProject";

const SessionContext = createContext<Session>(emptySession);

export const useSession = () => {
  return useContext(SessionContext);
};

const emptyProject = {
  id: "",
  name: "",
  slug: "",
  switchProject: () => {},
};
const ProjectContext = createContext<
  ProjectEntry & {
    switchProject: (slug: string) => void;
  }
>(emptyProject);

export const useProject = () => {
  return useContext(ProjectContext);
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

  const defaultProject = organization.projects[0];

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

export const useOrganization = (): OrganizationEntry & {
  refetch: () => Promise<OrganizationEntry>;
} => {
  const { organization, refetch } = useSession();

  const orgRefetch = async () => {
    const newSession = await refetch();
    return (
      newSession.organizations.find((org) => org.id === organization.id) ??
      newSession.organizations[0]!
    );
  };

  return Object.assign(organization, {
    refetch: orgRefetch,
  });
};

export const AuthProvider = ({ children }: { children: React.ReactNode }) => {
  return (
    <ErrorBoundary FallbackComponent={FullPageError}>
      <AuthHandler>{children}</AuthHandler>
    </ErrorBoundary>
  );
};

// Paths that are authenticated but don't require org/project slug context.
const SLUG_EXEMPT_PATHS = ["/slack/register"];

const AuthHandler = ({ children }: { children: React.ReactNode }) => {
  const { orgSlug, projectSlug } = useSlugs();
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const { session, error, status } = useSessionData();

  const isLoading = status === "pending";

  useIdentifyUserForTelemetry(session?.user);
  usePylonInAppChat(session?.user);

  // you need something like this so you don't redirect with empty session too soon
  // isLoading is not synchronized with the session data actually being populated, so we need to wait for the session to actually finish loading
  // !! Very important that auth.info returns an error if there's no session
  if (isLoading || (!session && !error)) {
    return null;
  }

  if (error || !session || !session.session) {
    return (
      <SessionContext.Provider value={emptySession}>
        {children}
      </SessionContext.Provider>
    );
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
  // Known org-level route paths that should not be treated as project slugs
  const ORG_ROUTE_PATHS = [
    "billing",
    "api-keys",
    "domains",
    "logs",
    "projects",
  ];
  const isProjectSlug = session.organization?.projects.some(
    (p) => p.slug === pathParts[1],
  );
  const isOrgRoutePath = ORG_ROUTE_PATHS.includes(pathParts[1]);
  // Redirect if: (1) it's a project slug and not an org route, OR
  // (2) it's both a project slug and an org route but has sub-paths (org routes don't have sub-paths)
  if (
    pathParts.length >= 2 &&
    pathParts[0] === session.organization?.slug &&
    isProjectSlug &&
    (!isOrgRoutePath || pathParts.length >= 3)
  ) {
    const rest = pathParts.slice(2).join("/");
    const newPath = `/${pathParts[0]}/projects/${pathParts[1]}${rest ? `/${rest}` : ""}`;
    return <Navigate to={newPath + location.search} replace />;
  }

  // Handle initial navigation
  const redirectParam = searchParams.get("redirect");
  if (redirectParam) {
    return <Navigate to={redirectParam} replace />;
  } else if (isSlugExempt) {
    // Fall through to render children
  } else if (session.organization && !projectSlug) {
    // On an org-level page or bare URL with no project context — that's fine,
    // unless we're at the root "/" with no org slug either
    if (!orgSlug || orgSlug !== session.organization.slug) {
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

export const useSessionData = () => {
  const {
    data: sessionData,
    error,
    refetch,
    status,
  } = useSessionInfo(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
    throwOnError: false,
  });

  const asSession = (sessionData: SessionInfoResponse): Session => {
    const sessionId = sessionData?.headers["gram-session"]?.[0];
    const result = sessionData.result;

    const organization =
      result.organizations.find(
        (org) => org.id === result.activeOrganizationId,
      ) ?? result.organizations[0];

    return {
      ...result,
      organization: organization ?? emptyOrganization,
      user: {
        id: result.userId,
        email: result.userEmail,
        isAdmin: result.isAdmin,
        signature: result.userSignature,
        displayName: result.userDisplayName,
        photoUrl: result.userPhotoUrl,
      },
      session: sessionId ?? "",
      rawGramAccountType: result.gramAccountType,
      refetch: async () => {
        const newSession = await refetch();
        return newSession.data ? asSession(newSession.data) : emptySession;
      },
    };
  };

  const session = sessionData ? asSession(sessionData) : null;

  return { session, error, status };
};

export const useUser = () => {
  const { user } = useSession();
  return user;
};

export const useIsAdmin = () => {
  const { isAdmin } = useUser();
  return isAdmin;
};

export function usePylonInAppChat(user: User | undefined) {
  useEffect(() => {
    if (!user) {
      return;
    }
    const random = Math.random().toString(36).substring(7) + "-anonymous";
    const email = user.email;
    const displayName = user.displayName || random;

    window.pylon = {
      chat_settings: {
        app_id: "f9cade16-8d3c-4826-9a2a-034fad495102",
        email: email,
        name: displayName,
        avatar_url: user?.photoUrl,
        ...(user?.signature && { email_hash: user.signature }),
      },
    };

    if (window.Pylon) {
      window.Pylon("setNewIssueCustomFields", { gram: true });
    }

    // This is for the marketing site
    localStorage.setItem("pylon_user_email", email);
    localStorage.setItem("pylon_user_display_name", displayName);
  }, [user]);
}
