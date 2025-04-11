import { createContext, Suspense, useState, useEffect } from "react";
import {
  InfoResponseBody,
  Organization,
  Project,
} from "@gram/sdk/models/components";
import { useContext } from "react";
import { useSessionInfoSuspense } from "@gram/sdk/react-query";
import { ErrorBoundary } from "react-error-boundary";
import { GramLogo } from "@/components/gram-logo";

type Session = InfoResponseBody & {
  session: string;
};

const emptySession: Session = {
  userId: "",
  userEmail: "",
  organizations: [],
  activeOrganizationId: "",
  session: "",
};

const SessionContext = createContext<Session>(emptySession);

export const useSession = () => {
  return useContext(SessionContext);
};

export const useProject = () => {
  const organization = useOrganization();

  const defaultProject = organization.projects[0];

  if (!defaultProject) {
    throw new Error("No projects found");
  }

  const [activeProject, setActiveProject] = useState<Project>(defaultProject);

  const switchProject = (projectId: string) => {
    setActiveProject(
      organization.projects.find((p) => p.projectId === projectId) ?? defaultProject
    );
  };

  return Object.assign(activeProject, {
    organizationId: organization.organizationId,
    switchProject,
  });
};

export const useOrganization = (): Organization => {
  const session = useSession();

  const organization =
    session.organizations.find(
      (org) => org.organizationId === session.activeOrganizationId
    ) ?? session.organizations[0];

  if (!organization) {
    throw new Error("Organization not found");
  }

  return organization;
};

// Custom Suspense component with minimum loading time
// This is used to ensure the loader is visible for a minimum amount of time to avoid flickering
const MinimumSuspense = ({
  children,
  fallback,
  minimumLoadTimeMs = 1000,
}: {
  children: React.ReactNode;
  fallback: React.ReactNode;
  minimumLoadTimeMs?: number;
}) => {
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const timer = setTimeout(() => {
      setIsLoading(false);
    }, minimumLoadTimeMs);

    return () => clearTimeout(timer);
  }, [minimumLoadTimeMs]);

  return (
    <Suspense fallback={fallback}>
      {isLoading ? (
        // This additional Suspense ensures the fallback stays visible
        // even if the children resolve quickly
        <NeverResolves />
      ) : (
        children
      )}
    </Suspense>
  );
};

// Component that never resolves during the minimum loading time
const NeverResolves = () => {
  throw new Promise(() => {});
  return null;
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

export const AuthProvider = ({ children }: { children: React.ReactNode }) => {
  const FullScreenLoader = () => {
    return (
      <div className="flex justify-center items-center h-screen">
        <GramLogo animate className="scale-125" />
      </div>
    );
  };

  return (
    <ErrorBoundary FallbackComponent={ErrorFallback}>
      <MinimumSuspense fallback={<FullScreenLoader />}>
        <AuthContent>{children}</AuthContent>
      </MinimumSuspense>
    </ErrorBoundary>
  );
};

const AuthContent = ({ children }: { children: React.ReactNode }) => {
  const sessionResponse = useSessionInfoSuspense({
    sessionHeaderGramSession: "", // We are using cookies instead, so this won't get set
  });

  const sessionId = sessionResponse.data.headers["gram-session"]?.[0];

  if (!sessionId) {
    throw new Error("Session ID not found");
  }

  console.log(sessionId);

  const session: Session = {
    ...sessionResponse.data.result,
    session: sessionId,
  };

  return (
    <SessionContext.Provider value={session}>
      {children}
    </SessionContext.Provider>
  );
};
