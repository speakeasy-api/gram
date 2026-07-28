import { CodeChallengeMethod } from "@gram/client/models/components/authorizerequestbody.js";
import { useCliAuthAuthorizeMutation } from "@gram/client/react-query/cliAuthAuthorize";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useEffect, useState, useRef } from "react";
import { useSessionData } from "@/contexts/Auth";

interface CliCallbackProps {
  keyScope?: "producer" | "hooks";
  localCallbackUrl: string;
  projectSlug?: string | null;
  organizationId?: string | null;
  /**
   * PKCE parameters. Their presence — a `codeChallenge` — is what selects the
   * PKCE one-time-code enrollment flow over the default producer-key flow. The
   * request opts into PKCE by supplying these, so no client has to identify
   * itself (or spoof another client) to use it.
   */
  codeChallenge?: string | null;
  codeChallengeMethod?: string | null;
  /**
   * How the credentials travel to the local callback. "post" submits them as
   * an auto-submitted form body (Apple-style form_post) so the API key never
   * appears in a URL; "get" is the legacy query-string redirect.
   */
  callbackMethod: "get" | "post";
}

/**
 * CliCallback is an authentication handler for local clients such as the CLI
 * and coding-agent hooks. By default it generates an API key in the requested
 * scope and returns it to the localhost callback URL as query parameters.
 *
 * When the request supplies a PKCE `code_challenge`, it instead runs the PKCE
 * enrollment flow: it exchanges the challenge for a short-lived one-time code
 * via `cliAuth.authorize` and transmits only that code back to the local
 * callback (the raw key is minted server-side at redeem, never in a URL). The
 * flow is selected by the presence of PKCE parameters, not by client identity.
 */
export default function CliCallback(props: CliCallbackProps): JSX.Element {
  const {
    keyScope = "producer",
    localCallbackUrl,
    projectSlug,
    organizationId,
    codeChallenge,
    codeChallengeMethod,
    callbackMethod,
  } = props;
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

    // The requesting plugin was generated for a specific organization; a key
    // minted in whichever org this browser session happens to have active
    // would silently route that machine's telemetry and policy checks to the
    // wrong org.
    if (organizationId && session.activeOrganizationId !== organizationId) {
      setError(errWrongOrganization);
      return;
    }

    hasCreatedKey.current = true;

    const selectedProjectSlug = selectCallbackProjectSlug(
      session.organization.projects,
      projectSlug,
    );

    if (isPkceFlow) {
      authorizePkceCode(
        authorizeCode,
        session.session,
        codeChallenge,
        codeChallengeMethod,
        selectedProjectSlug,
      )
        .then((code) => transmitCode(localCallbackUrl, code))
        .catch((err) => {
          setError(
            err instanceof Error ? err.message : "Failed to authorize device",
          );
        });
      return;
    }

    createScopedKey(createKey, session.session, keyScope)
      .then((key) => {
        const fields: Record<string, string> = { api_key: key };
        if (selectedProjectSlug) {
          fields.project = selectedProjectSlug;
        }
        if (session.user.email) {
          fields.email = session.user.email;
        }
        // The receiving client binds its credential cache to this org so a
        // cache minted here is never reused by a plugin generated for a
        // different org.
        if (session.activeOrganizationId) {
          fields.organization_id = session.activeOrganizationId;
        }
        transmitKey(localCallbackUrl, callbackMethod, fields);
      })
      .catch((err) => {
        setError(
          err instanceof Error ? err.message : "Failed to create API key",
        );
      });
  }, [
    createKey,
    authorizeCode,
    keyScope,
    localCallbackUrl,
    callbackMethod,
    projectSlug,
    organizationId,
    session,
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

function generateHooksKeyName(): string {
  const timestamp = Date.now();
  const maxLength = 40;

  return `Hooks Key (Generated) - ${timestamp}`.slice(0, maxLength);
}

const errInvalidCallback = "Callback URL must be localhost or 127.0.0.1";
const errWrongOrganization =
  "This connection link belongs to a different organization. Switch to that organization in the dashboard, then retry the connection.";
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

  // No validated selection: omit the project rather than guess one. Hook
  // plugins fall back to their generated project slug on an empty callback.
  return null;
}

function transmitKey(
  callbackUrl: string,
  callbackMethod: "get" | "post",
  fields: Record<string, string>,
): void {
  if (callbackMethod === "post") {
    // A form navigation (not fetch) so the localhost listener does not need
    // CORS or private-network-access preflight handling. The callback URL's
    // own query string (the client's state token) is preserved by POST.
    submitCallbackForm(callbackUrl, fields);
    return;
  }

  const url = new URL(callbackUrl);
  for (const [name, value] of Object.entries(fields)) {
    url.searchParams.set(name, value);
  }

  window.location.replace(url.toString());
}

function submitCallbackForm(
  actionUrl: string,
  fields: Record<string, string>,
): void {
  const form = document.createElement("form");
  form.method = "post";
  form.action = actionUrl;
  form.style.display = "none";
  for (const [name, value] of Object.entries(fields)) {
    const input = document.createElement("input");
    input.type = "hidden";
    input.name = name;
    input.value = value;
    form.appendChild(input);
  }
  document.body.appendChild(form);
  form.submit();
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
