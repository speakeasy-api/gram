import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useEffect, useState, useRef } from "react";
import { useSessionData } from "@/contexts/Auth";

interface HooksLoginProps {
  callbackUrl: string;
}

/**
 * HooksLogin handles authentication for Claude Code hooks. When this component
 * receives a callback URL, it generates an API key with appropriate scopes and
 * transmits it to the local CLI by redirecting to the callback URL with the
 * API key as a query parameter.
 */
export default function HooksLogin(props: HooksLoginProps) {
  const { callbackUrl } = props;
  const { session, status } = useSessionData();
  const [error, setError] = useState<string | null>(null);
  const { mutateAsync: createKey } = useCreateAPIKeyMutation();
  const hasCreatedKey = useRef(false);
  const validCallback = isCallbackLocal(callbackUrl);

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

    // Get preferred project from localStorage if it exists
    let projectSlug: string | null = null;
    const preferredProject = localStorage.getItem(PREFERRED_PROJECT_KEY);
    if (
      preferredProject &&
      session.organization.projects.find((p) => p.slug === preferredProject)
    ) {
      projectSlug = preferredProject;
    }

    createHooksKey(createKey, session.session)
      .then((key) => transmitKey(callbackUrl, key, projectSlug))
      .catch((err) => {
        setError(
          err instanceof Error ? err.message : "Failed to create API key",
        );
      });
  }, [createKey, session, callbackUrl]);

  if (error) {
    return <FailedScreen error={error} />;
  }

  return <WaitScreen />;
}

function FailedScreen({ error }: { error: string }) {
  return (
    <div className="flex items-center justify-center h-screen">
      <div className="text-center">
        <h1 className="text-2xl font-bold text-red-600 mb-2">
          Authentication Failed
        </h1>
        <p className="text-gray-600">{error}</p>
        <p className="text-gray-500 mt-4 text-sm">
          Please close this window and try again from your terminal.
        </p>
      </div>
    </div>
  );
}

function WaitScreen() {
  return (
    <div className="flex items-center justify-center h-screen">
      <div className="text-center">
        <div className="mb-4">
          <svg
            className="animate-spin h-12 w-12 text-blue-600 mx-auto"
            xmlns="http://www.w3.org/2000/svg"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              className="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              strokeWidth="4"
            ></circle>
            <path
              className="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
        </div>
        <h1 className="text-2xl font-bold mb-2">Authenticating...</h1>
        <p className="text-gray-600">
          Setting up your Claude Code hooks integration
        </p>
      </div>
    </div>
  );
}

function generateKeyName(): string {
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
  const name = generateKeyName();

  return {
    createKeyForm: { name, scopes },
    gramSession: sessionId,
  };
}

async function createHooksKey(
  createKey: ReturnType<typeof useCreateAPIKeyMutation>["mutateAsync"],
  sessionId: string,
): Promise<string> {
  // Hooks need producer scope to send hook events
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
