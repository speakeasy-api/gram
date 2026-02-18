import { FullPageError } from "@/components/full-page-error";
import { LINKED_FROM_PARAM } from "@/pages/home/Home";
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
    if (!project || project.slug !== currentProject.slug) {
      setProject(currentProject);
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
    navigate(`/${organization.slug}/${slug}`);
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

const AuthHandler = ({ children }: { children: React.ReactNode }) => {
  const { orgSlug, projectSlug } = useSlugs();
  const [searchParams] = useSearchParams();
  const location = useLocation();
  const { session, error, status } = useSessionData();

  const isLoading = status === "pending";

  // Pages that don't require authentication
  const publicPaths = ["/login", "/register"];
  const isPublicPage = publicPaths.some((path) =>
    location.pathname.startsWith(path),
  );

  useIdentifyUserForTelemetry(session?.user);
  usePylonInAppChat(session?.user);

  // you need something like this so you don't redirect with empty session too soon
  // isLoading is not synchronized with the session data actually being populated, so we need to wait for the session to actually finish loading
  // !! Very important that auth.info returns an error if there's no session
  if (isLoading || (!session && !error)) {
    return null;
  }

  if (error || !session || !session.session) {
    // Redirect to login if not already on a public page
    if (!isPublicPage) {
      return <Navigate to="/login" replace />;
    }
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

  // Handle initial navigation
  const redirectParam = searchParams.get("redirect");
  if (redirectParam) {
    if (!import.meta.env.DEV) {
      return <Navigate to={redirectParam} replace />;
    }
  } else if (session.organization && !projectSlug) {
    // if we're logged in but the URL doesn't have a project slug, redirect to
    // the default project
    let preferredProject = localStorage.getItem(PREFERRED_PROJECT_KEY);

    if (
      !preferredProject ||
      !session.organization.projects.find((p) => p.slug === preferredProject)
    ) {
      preferredProject = session.organization.projects[0]!.slug;
    }

    const paramsToForward = [LINKED_FROM_PARAM];
    const forwardParams = new URLSearchParams();
    paramsToForward.forEach((param) => {
      const value = searchParams.get(param);
      if (value !== null) {
        forwardParams.set(param, value);
      }
    });
    const paramsToForwardString = forwardParams.toString();

    return (
      <Navigate
        to={`/${session.organization.slug}/${preferredProject}?${paramsToForwardString}`}
        replace
      />
    );
  } else if (session.organization.slug !== orgSlug) {
    // make sure we don't direct to an org we aren't authenticated with
    return (
      <Navigate to={`/${session.organization.slug}/${projectSlug}`} replace />
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
