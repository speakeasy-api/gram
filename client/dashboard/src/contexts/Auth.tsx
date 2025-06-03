import { GramLogo } from "@/components/gram-logo";
import { MinimumSuspense } from "@/components/ui/minimum-suspense";
import { getServerURL } from "@/lib/utils";
import {
  InfoResponseBody,
  OrganizationEntry,
  ProjectEntry,
} from "@gram/client/models/components";
import { SessionInfoResponse } from "@gram/client/models/operations";
import {
  useListEnvironmentsSuspense,
  useListToolsetsSuspense,
  useSessionInfo,
} from "@gram/client/react-query";
import { createContext, useContext, useState } from "react";
import { ErrorBoundary } from "react-error-boundary";
import { useNavigate } from "react-router";
import { useSlugs } from "./Sdk";
import {
  useCaptureUserAuthorizationEvent,
  useIdentifyUserForTelemetry,
  useRegisterProjectForTelemetry,
} from "./Telemetry";

type Session = Omit<InfoResponseBody, "userEmail" | "userId" | "isAdmin"> & {
  user: User;
  session: string;
  organization: OrganizationEntry;
  refetch: () => Promise<Session>;
};

export type User = {
  id: string;
  email: string;
  isAdmin: boolean;
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
  organization: {
    id: "",
    name: "",
    slug: "",
    projects: [],
    accountType: "",
  },
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

  const { projectSlug } = useSlugs();
  const [project, setProject] = useState<ProjectEntry | null>(null);

  const defaultProject = organization.projects[0];
  if (!defaultProject) {
    throw new Error("No projects found");
  }

  const currentProject =
    organization.projects.find((p) => p.slug === projectSlug) ?? defaultProject;

  if (!project || project.slug !== currentProject.slug) {
    setProject(currentProject);
  }

  const switchProject = async (slug: string) => {
    localStorage.setItem(PREFERRED_PROJECT_KEY, slug);
    navigate(`/${organization.slug}/${slug}`);
  };

  useRegisterProjectForTelemetry({
    projectSlug: currentProject.slug,
    organizationSlug: organization.slug,
  });

  useCaptureUserAuthorizationEvent({
    projectSlug: currentProject.slug,
    organizationSlug: organization.slug,
    email: user.email,
  });

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

// Error fallback component
const ErrorFallback = ({ error }: { error: Error }) => {
  return (
    <div role="alert">
      <p>Something went wrong:</p>
      <pre>{error.message}</pre>
    </div>
  );
};

const FullScreenLoader = () => {
  return (
    <div className="flex justify-center items-center h-screen">
      <GramLogo />
    </div>
  );
};

export const AuthProvider = ({ children }: { children: React.ReactNode }) => {
  return (
    <ErrorBoundary FallbackComponent={ErrorFallback}>
      <MinimumSuspense fallback={<FullScreenLoader />}>
        <AuthHandler>{children}</AuthHandler>
      </MinimumSuspense>
    </ErrorBoundary>
  );
};

// Prefetch any queries while we're in the top-level loading state
const PrefetchedQueries = ({ children }: { children: React.ReactNode }) => {
  useListToolsetsSuspense();
  useListEnvironmentsSuspense();

  return children;
};

const AuthHandler = ({ children }: { children: React.ReactNode }) => {
  const navigate = useNavigate();
  const { projectSlug } = useSlugs();

  const {
    data: sessionData,
    error,
    refetch,
    isLoading,
  } = useSessionInfo(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
  });

  const asSession = (sessionData: SessionInfoResponse): Session => {
    const sessionId = sessionData?.headers["gram-session"]?.[0];
    const result = sessionData.result;

    const organization =
      result.organizations.find(
        (org) => org.id === result.activeOrganizationId
      ) ?? result.organizations[0];

    if (!organization) {
      throw new Error("No organization found");
    }

    return {
      ...result,
      organization,
      user: {
        id: result.userId,
        email: result.userEmail,
        isAdmin: result.isAdmin,
      },
      session: sessionId ?? "",
      refetch: sessionRefetch,
    };
  };

  const sessionRefetch: () => Promise<Session> = async () => {
    const newSession = await refetch();
    return newSession.data ? asSession(newSession.data) : emptySession;
  };

  const session = sessionData ? asSession(sessionData) : undefined;

  useIdentifyUserForTelemetry(session?.user);

  // you need something like this so you don't redirect with empty session too soon
  if (isLoading || !session) {
    return <FullScreenLoader />;
  }

  if (error || !session.session || !session.organization) {
    return (
      <SessionContext.Provider value={emptySession}>
        {children}
      </SessionContext.Provider>
    );
  }

  // if we're logged in but the URL doesn't have a project slug, redirect to the default project
  if (session.organization && !projectSlug) {
    let preferredProject = localStorage.getItem(PREFERRED_PROJECT_KEY);

    if (
      !preferredProject ||
      !session.organization.projects.find((p) => p.slug === preferredProject)
    ) {
      preferredProject = session.organization.projects[0]!.slug;
    }

    navigate(`/${session.organization.slug}/${preferredProject}`);
  }

  return (
    <SessionContext.Provider value={session}>
      <PrefetchedQueries>{children}</PrefetchedQueries>
    </SessionContext.Provider>
  );
};

export const useUser = () => {
  const { user } = useSession();
  return user;
};

export const useIsAdmin = () => {
  const { isAdmin } = useUser();
  const isLocal = getServerURL().includes("localhost");
  return isAdmin || isLocal;
};
