import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useEffect, useState, useRef } from "react";
import { useSessionData } from "@/contexts/Auth";

interface CliCallbackProps {
  localCallbackUrl: string;
}

/**
 * CliCallback is an authentication handler for the CLI. When this component
 * receives a local callback URL, it generates a producer-scoped API key and
 * transmits it to the client by appending it to the callback URL as a query
 * parameter.
 */
export default function CliCallback(props: CliCallbackProps) {
  const { localCallbackUrl } = props;
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

    // Only get project from localStorage if it exists and is valid
    let projectSlug: string | null = null;
    const preferredProject = localStorage.getItem(PREFERRED_PROJECT_KEY);
    if (
      preferredProject &&
      session.organization.projects.find((p) => p.slug === preferredProject)
    ) {
      projectSlug = preferredProject;
    }

    createProducerKey(createKey, session.session)
      .then((key) => transmitKey(localCallbackUrl, key, projectSlug))
      .catch((err) => {
        setError(
          err instanceof Error ? err.message : "Failed to create API key",
        );
      });
  }, [createKey, session, localCallbackUrl]);

  if (error) {
    return <FailedScreen error={error} />;
  }

  return <WaitScreen />;
}

function FailedScreen({ error }: { error: string }) {
  return (
    <div className="flex items-center justify-center h-screen">
      <div className="text-center">
        <h1 className="text-2xl font-bold text-red-600 mb-2">Error</h1>
        <p className="text-gray-600">{error}</p>
      </div>
    </div>
  );
}

function WaitScreen() {
  return (
    <div className="flex items-center justify-center h-screen">
      <div className="text-center">
        <h1 className="text-2xl font-bold mb-2">Redirecting...</h1>
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
  const name = generateKeyName();

  return {
    createKeyForm: { name, scopes },
    gramSession: sessionId,
  };
}

async function createProducerKey(
  createKey: ReturnType<typeof useCreateAPIKeyMutation>["mutateAsync"],
  sessionId: string,
): Promise<string> {
  const scopes = ["producer"];
  const result = await createKey({
    request: keyRequest({ sessionId, scopes }),
  });
  if (!result.key) throw new Error("No API key returned from server");

  return result.key;
}

async function transmitKey(
  callbackUrl: string,
  apiKey: string,
  projectSlug: string | null,
): Promise<void> {
  const url = new URL(callbackUrl);
  url.searchParams.set("api_key", apiKey);
  if (projectSlug) {
    url.searchParams.set("project", projectSlug);
  }

  window.location.replace(url.toString());
}
