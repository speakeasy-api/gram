import { createContext, Suspense, useState } from "react";
import { InfoResponseBody, Project } from "@gram/sdk/models/components";
import { useContext } from "react";
import { useSessionInfo, useSessionInfoSuspense } from "@gram/sdk/react-query";
import { ErrorBoundary } from "react-error-boundary";

type Session = InfoResponseBody & {
  session: string;
}

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

  const [activeProject, setActiveProject] = useState<Project>(
    organization.projects[0]
  );

  const switchProject = (projectId: string) => {
    setActiveProject(
      organization.projects.find((p) => p.projectId === projectId) ??
        organization.projects[0]
    );
  };

  return Object.assign(activeProject, {
    organizationId: organization.organizationId,
    switchProject,
  });
};

export const useOrganization = () => {
  const session = useSession();

  const organization =
    session.organizations.find(
      (org) => org.organizationId === session.activeOrganizationId
    ) ?? session.organizations[0];

  return organization;
};

// Create a separate component for the suspended content
const AuthContent = ({ children }: { children: React.ReactNode }) => {
  const sessionResponse = useSessionInfoSuspense(
    {
      sessionHeaderGramSession: "", // We are using cookies instead, so this won't get set
    },
  );

  const sessionId = sessionResponse.data.headers["gram-session"][0];

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
  return (
    <ErrorBoundary FallbackComponent={ErrorFallback}>
      <Suspense fallback={<div>Loading auth state...</div>}>
        <AuthContent>{children}</AuthContent>
      </Suspense>
    </ErrorBoundary>
  );
};
