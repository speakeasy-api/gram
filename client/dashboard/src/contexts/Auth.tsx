import { createContext, useState } from "react";
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
import { useSlugs } from "./Sdk";
import { useNavigate } from "react-router";
import { MinimumSuspense } from "@/components/ui/minimum-suspense";

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
  } = useSessionInfo({ sessionHeaderGramSession: "" }, undefined, {
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
    navigate(`/${organization.slug}/${organization.projects[0]!.slug}`);
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

// TODO: Make this better
export const useIsAdmin = () => {
  const session = useSession();

  return (
    session.organization.slug === "organization-123" ||
    session.organization.slug === "speakeasy-self"
  );
};
