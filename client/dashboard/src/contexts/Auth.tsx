import { createContext, Suspense, useState, useEffect } from "react";
import {
  InfoResponseBody,
  OrganizationEntry,
  ProjectEntry,
} from "@gram/client/models/components";
import { useContext } from "react";
import {
  useListEnvironmentsSuspense,
  useListToolsetsSuspense,
  useSessionInfo,
} from "@gram/client/react-query";
import { ErrorBoundary } from "react-error-boundary";
import { GramLogo } from "@/components/gram-logo";

type Session = InfoResponseBody & {
  session: string;
  organization: OrganizationEntry;
  refetch: () => Promise<InfoResponseBody>;
};

const emptySession: Session = {
  userId: "",
  userEmail: "",
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

const SessionContext = createContext<Session>(emptySession);

const ProjectContext = createContext<{
  activeProject: ProjectEntry | null;
  setActiveProject: (project: ProjectEntry) => void;
}>({
  activeProject: null,
  setActiveProject: () => {},
});

export const useSession = () => {
  return useContext(SessionContext);
};

export const useProject = () => {
  const organization = useOrganization();
  const { activeProject, setActiveProject } = useContext(ProjectContext);

  const defaultProject = organization.projects[0];
  if (!defaultProject) {
    throw new Error("No projects found");
  }

  // Initialize project if not set
  useEffect(() => {
    if (!activeProject) {
      setActiveProject(defaultProject);
    }
  }, [defaultProject, activeProject, setActiveProject]);

  const currentProject = activeProject ?? defaultProject;

  const switchProject = async (projectId: string) => {
    // Refetch in case the project was just created
    const newOrg = await organization.refetch();

    const project = newOrg.projects.find((p) => p.id === projectId);
    if (!project) return;

    setActiveProject(project);
  };

  return Object.assign(currentProject, {
    organizationId: organization.id,
    switchProject,
  });
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

// Custom Suspense component with minimum loading time
// This is used to ensure the loader is visible for a minimum amount of time to avoid flickering
const MinimumSuspense = ({
  children,
  fallback,
  minimumLoadTimeMs = 750,
}: {
  children: React.ReactNode;
  fallback: React.ReactNode;
  minimumLoadTimeMs?: number;
}) => {
  const [isLoading, setIsLoading] = useState(true);
  const [showFallback, setShowFallback] = useState(false);

  useEffect(() => {
    if (!showFallback) {
      return;
    }

    setIsLoading(true);
    const timer = setTimeout(() => {
      setIsLoading(false);
    }, minimumLoadTimeMs);

    return () => clearTimeout(timer);
  }, [minimumLoadTimeMs, showFallback]);

  // This is used to ensure the timer gets reset every time the fallback is shown
  const FallbackHandler = () => {
    useEffect(() => {
      setShowFallback(true);
      return () => setShowFallback(false);
    }, []);
    return <>{fallback}</>;
  };

  return (
    <Suspense fallback={<FallbackHandler />}>
      {isLoading ? <NeverResolves /> : children}
    </Suspense>
  );
};

// Component that never resolves during the minimum loading time
const NeverResolves = () => {
  throw new Promise(() => {});
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
      <GramLogo animate className="scale-125" />
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
  const project = useProject();

  useListToolsetsSuspense({
    gramProject: project.slug,
  });

  useListEnvironmentsSuspense({
    gramProject: project.slug,
  });

  return children;
};

const AuthHandler = ({ children }: { children: React.ReactNode }) => {
  // you cannot use useSessionInfoSuspense here because it will not catch the error correctly
  const { data, isLoading, error, refetch } = useSessionInfo(
    { sessionHeaderGramSession: "" },
    undefined,
    {
      refetchOnWindowFocus: false,
      retries: {
        strategy: "none",
      },
    }
  );

  const [activeProject, setActiveProject] = useState<ProjectEntry | null>(null);

  const sessionRefetch = async () => {
    const newSession = await refetch();
    return newSession.data?.result ?? emptySession;
  };

  const sessionId = data?.headers["gram-session"]?.[0];

  // you need something like this so you don't redirect with empty session to soon
  if (isLoading) {
    return <FullScreenLoader />;
  }

  if (error || !sessionId || !data.result?.organizations) {
    return (
      <SessionContext.Provider value={emptySession}>
        {children}
      </SessionContext.Provider>
    );
  }

  const organization =
    data.result.organizations.find(
      (org) => org.id === data.result.activeOrganizationId
    ) ?? data.result.organizations[0]!;

  const session: Session = {
    ...data.result,
    session: sessionId,
    organization,
    refetch: sessionRefetch,
  };

  return (
    <SessionContext.Provider value={session}>
      <ProjectContext.Provider value={{ activeProject, setActiveProject }}>
        <PrefetchedQueries>{children}</PrefetchedQueries>
      </ProjectContext.Provider>
    </SessionContext.Provider>
  );
};
