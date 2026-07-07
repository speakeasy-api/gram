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
   * When `"form_post"`, credentials are delivered by auto-submitting a POST
   * form to the localhost callback instead of appending them to its URL. This
   * keeps the minted key out of the browser history and the local listener's
   * process arguments. Any other value (or absence) preserves the default
   * query-parameter redirect, so older clients keep working.
   */
  responseMode?: string | null;
}

/**
 * CliCallback is an authentication handler for local clients such as the CLI
 * and coding-agent hooks. By default it generates an API key in the requested
 * scope and returns it to the localhost callback URL as query parameters. When
 * the request sets `response_mode=form_post` it instead POSTs the credentials
 * as a form body, keeping the minted key out of the callback URL.
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
    responseMode,
  } = props;
  const { session, status } = useSessionData();
  const [error, setError] = useState<string | null>(null);
  const { mutateAsync: createKey } = useCreateAPIKeyMutation();
  const { mutateAsync: authorizeCode } = useCliAuthAuthorizeMutation();
  const hasCreatedKey = useRef(false);
  const validCallback = isCallbackLocal(localCallbackUrl);
  const isPkceFlow = Boolean(codeChallenge);
  const useFormPost = responseMode === "form_post";

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
        .then((code) => transmitCode(localCallbackUrl, code, useFormPost))
        .catch((err) => {
          setError(
            err instanceof Error ? err.message : "Failed to authorize device",
          );
        });
      return;
    }

    createScopedKey(createKey, session.session, keyScope)
      .then((key) =>
        transmitKey(
          localCallbackUrl,
          key,
          selectedProjectSlug,
          session.user.email,
          session.activeOrganizationId,
          useFormPost,
        ),
      )
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
    projectSlug,
    organizationId,
    session,
    validCallback,
    isPkceFlow,
    codeChallenge,
    codeChallengeMethod,
    useFormPost,
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

async function transmitKey(
  callbackUrl: string,
  apiKey: string,
  projectSlug: string | null,
  userEmail: string,
  organizationId?: string | null,
  useFormPost?: boolean,
): Promise<void> {
  const fields: Record<string, string> = { api_key: apiKey };
  if (projectSlug) {
    fields.project = projectSlug;
  }
  if (userEmail) {
    fields.email = userEmail;
  }
  // The receiving client binds its credential cache to this org so a cache
  // minted here is never reused by a plugin generated for a different org.
  if (organizationId) {
    fields.organization_id = organizationId;
  }

  transmit(callbackUrl, fields, useFormPost);
}

/**
 * transmit hands the credential fields to the localhost callback. In form_post
 * mode it auto-submits a POST form so the secret rides in the request body
 * (never the URL); otherwise it falls back to a query-parameter redirect.
 */
function transmit(
  callbackUrl: string,
  fields: Record<string, string>,
  useFormPost?: boolean,
): void {
  if (useFormPost) {
    postCallback(callbackUrl, fields);
    return;
  }

  const url = new URL(callbackUrl);
  for (const [name, value] of Object.entries(fields)) {
    url.searchParams.set(name, value);
  }
  window.location.replace(url.toString());
}

function postCallback(
  callbackUrl: string,
  fields: Record<string, string>,
): void {
  const form = document.createElement("form");
  form.method = "POST";
  form.action = callbackUrl;
  // Default enctype (application/x-www-form-urlencoded) URL-encodes the field
  // values, matching what the localhost listener decodes on the other side.
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

async function transmitCode(
  callbackUrl: string,
  code: string,
  useFormPost?: boolean,
): Promise<void> {
  transmit(callbackUrl, { code }, useFormPost);
}
