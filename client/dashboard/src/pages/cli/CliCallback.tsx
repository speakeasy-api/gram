import { CodeChallengeMethod } from "@gram/client/models/components";
import { useCliAuthAuthorizeMutation } from "@gram/client/react-query/cliAuthAuthorize";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useEffect, useState, useRef } from "react";
import { useSessionData } from "@/contexts/Auth";

interface CliCallbackProps {
  localCallbackUrl: string;
  /**
   * PKCE parameters. Their presence — a `codeChallenge` — is what selects the
   * PKCE one-time-code enrollment flow over the default producer-key flow. The
   * request opts into PKCE by supplying these, so no client has to identify
   * itself (or spoof another client) to use it.
   */
  codeChallenge?: string | null;
  codeChallengeMethod?: string | null;
}

/**
 * CliCallback is an authentication handler for the CLI. By default it generates
 * a producer-scoped API key and transmits it to the client by appending it to
 * the local callback URL as a query parameter.
 *
 * When the request supplies a PKCE `code_challenge`, it instead runs the PKCE
 * enrollment flow: it exchanges the challenge for a short-lived one-time code
 * via `cliAuth.authorize` and transmits only that code back to the local
 * callback (the raw key is minted server-side at redeem, never in a URL). The
 * flow is selected by the presence of PKCE parameters, not by client identity.
 */
export default function CliCallback(props: CliCallbackProps): JSX.Element {
  const { localCallbackUrl, codeChallenge, codeChallengeMethod } = props;
  const { session, status } = useSessionData();
  const [error, setError] = useState<string | null>(null);
  const { mutateAsync: createKey } = useCreateAPIKeyMutation();
  const { mutateAsync: authorizeCode } = useCliAuthAuthorizeMutation();
  const hasCreatedKey = useRef(false);
  const validCallback = isCallbackLocal(localCallbackUrl);
  const isPkceFlow = Boolean(codeChallenge);

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

    if (isPkceFlow) {
      authorizePkceCode(
        authorizeCode,
        session.session,
        codeChallenge,
        codeChallengeMethod,
        projectSlug,
      )
        .then((code) => transmitCode(localCallbackUrl, code))
        .catch((err) => {
          setError(
            err instanceof Error ? err.message : "Failed to authorize device",
          );
        });
      return;
    }

    createProducerKey(createKey, session.session)
      .then((key) =>
        transmitKey(localCallbackUrl, key, projectSlug, session.user.email),
      )
      .catch((err) => {
        setError(
          err instanceof Error ? err.message : "Failed to create API key",
        );
      });
  }, [
    createKey,
    authorizeCode,
    session,
    localCallbackUrl,
    validCallback,
    isPkceFlow,
    codeChallenge,
    codeChallengeMethod,
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

async function authorizePkceCode(
  authorize: ReturnType<typeof useCliAuthAuthorizeMutation>["mutateAsync"],
  sessionId: string,
  codeChallenge: string | null | undefined,
  codeChallengeMethod: string | null | undefined,
  projectSlug: string | null,
): Promise<string> {
  if (!codeChallenge) throw new Error("Missing code_challenge parameter");
  if (codeChallengeMethod !== CodeChallengeMethod.S256) {
    throw new Error("Unsupported code_challenge_method (only S256 is allowed)");
  }

  const result = await authorize({
    request: {
      gramSession: sessionId,
      authorizeRequestBody: {
        codeChallenge,
        codeChallengeMethod: CodeChallengeMethod.S256,
        projectSlug: projectSlug ?? undefined,
      },
    },
  });
  if (!result.code) throw new Error("No code returned from server");

  return result.code;
}

async function transmitCode(callbackUrl: string, code: string): Promise<void> {
  const url = new URL(callbackUrl);
  url.searchParams.set("code", code);

  window.location.replace(url.toString());
}
