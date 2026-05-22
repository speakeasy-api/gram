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
  });

  if (!response.ok) {
    throw new Error(`Registration failed (HTTP ${response.status})`);
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
