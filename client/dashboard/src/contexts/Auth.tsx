import { GramLogo } from "@/components/gram-logo";
import { MinimumSuspense } from "@/components/ui/minimum-suspense";
import { getServerURL } from "@/lib/utils";
import {
  InfoResponseBody,
  OrganizationEntry,
  ProjectEntry,
} from "@gram/client/models/components";
import {
  useListEnvironmentsSuspense,
  useListToolsetsSuspense,
  useSessionInfo,
} from "@gram/client/react-query";
import { createContext, useContext, useState } from "react";
import { ErrorBoundary } from "react-error-boundary";
import { useNavigate } from "react-router";
import { useSlugs } from "./Sdk";

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
  isAdmin: false,
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

export const useProject = () => {
  const organization = useOrganization();
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

  const sessionRefetch = async () => {
    const newSession = await refetch();
    return newSession.data?.result ?? emptySession;
  };

  const sessionId = sessionData?.headers["gram-session"]?.[0];

  // you need something like this so you don't redirect with empty session to soon
  if (isLoading) {
    return <FullScreenLoader />;
  }

  const organization =
    sessionData?.result.organizations.find(
      (org) => org.id === sessionData.result.activeOrganizationId
    ) ?? sessionData?.result.organizations[0];

  if (error || !sessionId || !organization) {
    return (
      <SessionContext.Provider value={emptySession}>
        {children}
      </SessionContext.Provider>
    );
  }

  // if we're logged in but the URL doesn't have a project slug, redirect to the default project
  if (organization && !projectSlug) {
    let preferredProject = localStorage.getItem(PREFERRED_PROJECT_KEY);

    if (
      !preferredProject ||
      !organization.projects.find((p) => p.slug === preferredProject)
    ) {
      preferredProject = organization.projects[0]!.slug;
    }

    navigate(`/${organization.slug}/${preferredProject}`);
  }

  const session: Session = {
    ...sessionData.result,
    session: sessionId,
    organization,
    refetch: sessionRefetch,
  };

  return (
    <SessionContext.Provider value={session}>
      <PrefetchedQueries>{children}</PrefetchedQueries>
    </SessionContext.Provider>
  );
};

export const useIsAdmin = () => {
  const { isAdmin } = useSession();
  const isLocal = getServerURL().includes("localhost");
  return isAdmin || isLocal;
};
