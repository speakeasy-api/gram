export type AuthedFetch = (
  endpoint: string,
  opts: RequestInit,
) => Promise<Response>;

export type ProxyRegisteredClient = {
  clientId: string;
  clientSecret: string;
  tokenEndpointAuthMethod: string | null;
};

export type ProxyRegisterUpstreamClientInput = {
  registrationEndpoint: string;
  scope?: string;
  tokenEndpointAuthMethod?: string;
};

export class ProxyRegistrationError extends Error {
  readonly title: string;

  constructor(status: number, message?: string) {
    const title = `Registration failed (HTTP ${status})`;
    super(message ?? title);
    this.name = "ProxyRegistrationError";
    this.title = title;
  }
}

export async function proxyRegisterUpstreamClient(
  authedFetch: AuthedFetch,
  input: ProxyRegisterUpstreamClientInput,
  { signal }: { signal?: AbortSignal } = {},
): Promise<ProxyRegisteredClient> {
  const body: Record<string, unknown> = {
    registration_endpoint: input.registrationEndpoint,
  };
  if (input.scope !== undefined) body.scope = input.scope;
  if (input.tokenEndpointAuthMethod !== undefined) {
    body.token_endpoint_auth_method = input.tokenEndpointAuthMethod;
  }

  const response = await authedFetch("/oauth/proxy-register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal,
    ...(import.meta.env.DEV ? { credentials: "include" } : {}),
  });

  if (!response.ok) {
    const message = await registrationErrorMessage(response);
    throw new ProxyRegistrationError(response.status, message ?? undefined);
  }

  const result = (await response.json()) as {
    client_id?: string;
    client_secret?: string;
    token_endpoint_auth_method?: string;
  };

  if (!result.client_id) {
    throw new Error("Upstream did not return a client_id");
  }

  return {
    clientId: result.client_id,
    clientSecret: result.client_secret ?? "",
    tokenEndpointAuthMethod: result.token_endpoint_auth_method ?? null,
  };
}

// registrationErrorMessage pulls the most actionable message out of a failed
// /oauth/proxy-register response. The backend passes an upstream 4xx through as
// a bad request whose `Message` already carries the upstream
// error/error_description; some responses may instead surface the raw RFC 7591
// `error_description`/`error`.
async function registrationErrorMessage(
  response: Response,
): Promise<string | null> {
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    return null;
  }

  if (body && typeof body === "object") {
    const record = body as Record<string, unknown>;
    for (const key of [
      "Message",
      "message",
      "error_description",
      "error",
    ] as const) {
      const value = record[key];
      if (typeof value === "string" && value.trim()) return value;
    }
  }
  return null;
}
