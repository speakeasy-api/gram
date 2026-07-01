import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useEffect, useState, useRef } from "react";
import { useSessionData } from "@/contexts/Auth";

interface CliCallbackProps {
  keyScope?: "producer" | "hooks";
  localCallbackUrl: string;
  projectSlug?: string | null;
}

/**
 * CliCallback is an authentication handler for local clients such as the CLI
 * and coding-agent hooks. It generates the requested API key scope and returns
 * it to the localhost callback URL as query parameters.
 */
export default function CliCallback(props: CliCallbackProps): JSX.Element {
  const { keyScope = "producer", localCallbackUrl, projectSlug } = props;
  const { session, status } = useSessionData();
  const [error, setError] = useState<string | null>(null);
  const { mutateAsync: createKey } = useCreateAPIKeyMutation();
  const hasCreatedKey = useRef(false);
  const validCallback = isCallbackLocal(localCallbackUrl);

  useEffect(() => {
    if (status === "pending") return;

    const redirectUrl = encodeURIComponent(window.location.href);
    if (!session?.session) {
      window.location.href = `/login?redirect=${redirectUrl}`;
      return;
    }

    if (!session?.activeOrganizationId) {
      window.location.href = `/register?redirect=${redirectUrl}`;
      return;
    }
  }, [session, status]);

  useEffect(() => {
    if (!session) return;
    if (hasCreatedKey.current) return;

    if (!validCallback) {
      setError(errInvalidCallback);
      return;
    }

    hasCreatedKey.current = true;

    const selectedProjectSlug = selectCallbackProjectSlug(
      session.organization.projects,
      projectSlug,
    );

    createScopedKey(createKey, session.session, keyScope)
      .then((key) =>
        transmitKey(
          localCallbackUrl,
          key,
          selectedProjectSlug,
          session.user.email,
        ),
      )
      .catch((err) => {
        setError(
          err instanceof Error ? err.message : "Failed to create API key",
        );
      });
  }, [
    createKey,
    keyScope,
    localCallbackUrl,
    projectSlug,
    session,
    validCallback,
  ]);

  if (error) {
    return <FailedScreen error={error} />;
  }

  return <WaitScreen />;
}

function FailedScreen({ error }: { error: string }) {
  return (
    <div className="flex h-screen items-center justify-center">
      <div className="text-center">
        <h1 className="mb-2 text-2xl font-bold text-red-600">Error</h1>
        <p className="text-gray-600">{error}</p>
      </div>
    </div>
  );
}

function WaitScreen() {
  return (
    <div className="flex h-screen items-center justify-center">
      <div className="text-center">
        <h1 className="mb-2 text-2xl font-bold">Redirecting...</h1>
        <p className="text-gray-600">You will be redirected shortly</p>
      </div>
    </div>
  );
}

function generateKeyName(): string {
  const timestamp = Date.now();
  const maxLength = 40;

  return `CLI Key (Generated) - ${timestamp}`.slice(0, maxLength);
}

function generateHooksKeyName(): string {
  const timestamp = Date.now();
  const maxLength = 40;

  return `Hooks Key (Generated) - ${timestamp}`.slice(0, maxLength);
}

const errInvalidCallback = "Callback URL must be localhost or 127.0.0.1";
const PREFERRED_PROJECT_KEY = "preferredProject";

function isCallbackLocal(callbackUrl: string): boolean {
  try {
    const url = new URL(callbackUrl);
    const hostname = url.hostname.toLowerCase();

    return hostname === "localhost" || hostname === "127.0.0.1";
  } catch {
    return false;
  }
}

interface KeyRequestParams {
  scopes: string[];
  sessionId: string;
}
function keyRequest(params: KeyRequestParams) {
  const { scopes, sessionId } = params;
  const name = scopes.includes("hooks")
    ? generateHooksKeyName()
    : generateKeyName();

  return {
    createKeyForm: { name, scopes },
    gramSession: sessionId,
  };
}

async function createScopedKey(
  createKey: ReturnType<typeof useCreateAPIKeyMutation>["mutateAsync"],
  sessionId: string,
  keyScope: "producer" | "hooks",
): Promise<string> {
  const scopes = [keyScope];
  const result = await createKey({
    request: keyRequest({ sessionId, scopes }),
  });
  if (!result.key) throw new Error("No API key returned from server");

  return result.key;
}

function selectCallbackProjectSlug(
  projects: { slug: string }[],
  requestedProjectSlug: string | null | undefined,
): string | null {
  if (
    requestedProjectSlug &&
    projects.find((p) => p.slug === requestedProjectSlug)
  ) {
    return requestedProjectSlug;
  }

  const preferredProject = localStorage.getItem(PREFERRED_PROJECT_KEY);
  if (preferredProject && projects.find((p) => p.slug === preferredProject)) {
    return preferredProject;
  }

  return projects[0]?.slug ?? null;
}

async function transmitKey(
  callbackUrl: string,
  apiKey: string,
  projectSlug: string | null,
  userEmail: string,
): Promise<void> {
  const url = new URL(callbackUrl);
  url.searchParams.set("api_key", apiKey);
  if (projectSlug) {
    url.searchParams.set("project", projectSlug);
  }
  if (userEmail) {
    url.searchParams.set("email", userEmail);
  }

  window.location.replace(url.toString());
}
