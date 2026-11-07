import { createServer, type Server } from "node:http";
import { randomBytes, createHash, timingSafeEqual } from "node:crypto";

export interface FixtureMcpClientOptions {
  endpoint: string;
  clientName?: string;
  headers?: Record<string, string>;
  bearerToken?: string;
  allowInsecureLoopback?: boolean;
  trustedOrigins?: string[];
  // Sensitive opt-in capture: callers must keep bodies out of logs and attachments.
  onProtocolFailure?: (event: {
    operation: string;
    body: unknown;
  }) => Promise<void>;
}
export interface Discovery {
  resource: string;
  authorizationServer: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  registrationEndpoint?: string;
  scopes?: string[];
}
export interface TranscriptEvent {
  operation: string;
  status?: number;
  protocolCode?: number | string;
  reason?: string;
  errorBodyUnavailable?: boolean;
  errorBodyTooLarge?: boolean;
  sessionSent?: boolean;
  retryAfterSeconds?: number;
  responseReceivedAtMs?: number;
}
type JsonObject = Record<string, unknown>;
function object(value: unknown): JsonObject {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Expected JSON object");
  return value as JsonObject;
}
function string(value: unknown): string {
  if (typeof value !== "string" || !value)
    throw new Error("Expected nonempty string");
  return value;
}

// Retain only a bounded delay, never the raw Retry-After header or wall-clock date.
function unavailableRetryAfter(
  value: string | null,
  receivedWallMs: number,
): number | undefined {
  if (value === null || value.length > 64) return undefined;
  const trimmed = value.trim();
  let seconds: number;
  if (/^\d{1,3}$/.test(trimmed)) {
    seconds = Number(trimmed);
  } else if (
    /^(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun), \d{2} (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d{4} \d{2}:\d{2}:\d{2} GMT$/.test(
      trimmed,
    )
  ) {
    const deadline = Date.parse(trimmed);
    if (
      !Number.isFinite(deadline) ||
      new Date(deadline).toUTCString() !== trimmed
    )
      return undefined;
    seconds = Math.ceil((deadline - receivedWallMs) / 1000);
  } else {
    return undefined;
  }
  return Number.isInteger(seconds) && seconds >= 1 && seconds <= 300
    ? seconds
    : undefined;
}

// Only exact source-defined literals may escape an untrusted response body.
// Sources: oautherr/rfc_6749_codes.go, oops/{codes,mcp}.go, mcp/authnchallenge*.go.
function errorDiagnostics(value: unknown): Partial<TranscriptEvent> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const body = value as JsonObject;
  const error = body.error;
  const rpc =
    error && typeof error === "object" && !Array.isArray(error)
      ? (error as JsonObject)
      : undefined;
  const result: Partial<TranscriptEvent> = {};
  const oauthCodes = [
    "access_denied",
    "invalid_client",
    "invalid_grant",
    "invalid_request",
    "invalid_scope",
    "invalid_target",
    "server_error",
    "temporarily_unavailable",
    "unauthorized_client",
    "unsupported_grant_type",
    "unsupported_response_type",
  ];
  if (typeof error === "string" && oauthCodes.includes(error))
    result.protocolCode = error;
  const httpCodes = [
    "unauthorized",
    "forbidden",
    "bad_request",
    "failed_precondition",
    "gateway_error",
    "unavailable",
    "not_found",
    "unexpected",
  ];
  if (typeof body.name === "string" && httpCodes.includes(body.name))
    result.protocolCode = body.name;
  const rpcCodes = [
    -32700, -32600, -32601, -32602, -32603, -32000, -32001, -32002, -32003,
    -32020, -32021, -32022,
  ];
  if (typeof rpc?.code === "number" && rpcCodes.includes(rpc.code))
    result.protocolCode = rpc.code;
  const reasons = [
    [
      "The upstream connection for this MCP server is missing or expired. Reauthorize the MCP server to reconnect it.",
      "upstream_connection_missing_or_expired",
    ],
    [
      "session authorization is no longer valid",
      "session_authorization_invalid",
    ],
    [
      "agent authorization is no longer valid; reauthorize",
      "agent_authorization_invalid",
    ],
    [
      "authorization issuer is no longer available",
      "authorization_issuer_unavailable",
    ],
    [
      "the sign-in that authorized this grant was revoked; sign in again",
      "sign_in_revoked",
    ],
    [
      "refresh_token is unknown or already used",
      "refresh_token_unknown_or_used",
    ],
    [
      "refresh_token was issued to a different client",
      "refresh_token_client_mismatch",
    ],
    ["refresh_token has expired", "refresh_token_expired"],
    ["refreshed session is no longer active", "refreshed_session_inactive"],
    [
      "session tool selection is malformed; reauthorize",
      "session_tool_selection_malformed",
    ],
    [
      "session tool selection is bound to a different MCP endpoint; reauthorize",
      "session_tool_selection_endpoint_mismatch",
    ],
    [
      "session policy is invalid for this MCP endpoint; reauthorize",
      "session_policy_endpoint_invalid",
    ],
    ["code not found or expired", "code_not_found_or_expired"],
    ["code was issued to a different client", "code_client_mismatch"],
    [
      "redirect_uri does not match the original request",
      "redirect_uri_mismatch",
    ],
    ["code_verifier does not match code_challenge", "pkce_mismatch"],
    [
      "The upstream connection for this MCP server is misconfigured. Contact the MCP server administrator.",
      "remote_session_misconfigured",
    ],
    [
      "The upstream credential this MCP server uses is misconfigured or was rejected by the upstream. Contact the MCP server administrator.",
      "client_credential_misconfigured",
    ],
  ] as const;
  for (const [literal, reason] of reasons) {
    if (
      [
        body.error_description,
        body.message,
        rpc?.message,
        rpc?.error_description,
      ].some(
        (message) =>
          typeof message === "string" &&
          (message === literal || message.endsWith(`: ${literal}`)),
      )
    )
      result.reason = reason;
  }
  return result;
}

async function responseDiagnostics(
  response: Response,
): Promise<{ diagnostics: Partial<TranscriptEvent>; body?: unknown }> {
  const reader = response.clone().body?.getReader();
  if (!reader) return { diagnostics: { errorBodyUnavailable: true } };
  let timer: ReturnType<typeof setTimeout> | undefined;
  const deadline = new Promise<never>((_, reject) => {
    timer = setTimeout(
      () => reject(new Error("Diagnostic read timeout")),
      2_000,
    );
  });
  try {
    const chunks: Uint8Array[] = [];
    let size = 0;
    while (true) {
      const { value, done } = await Promise.race([reader.read(), deadline]);
      if (done) break;
      size += value.byteLength;
      if (size > 16_384) return { diagnostics: { errorBodyTooLarge: true } };
      chunks.push(value);
    }
    const body: unknown = JSON.parse(Buffer.concat(chunks).toString("utf8"));
    return { diagnostics: errorDiagnostics(body), body };
  } catch {
    return { diagnostics: { errorBodyUnavailable: true } };
  } finally {
    clearTimeout(timer);
    // A clone is a tee: awaiting cancellation can wait for the original consumer.
    void reader.cancel().catch(() => {});
  }
}

/** Protocol-only test client. Transcripts intentionally contain no URLs or payloads. */
export class FixtureMcpClient {
  private options: FixtureMcpClientOptions;
  private metadata?: Discovery;
  private revocationEndpoint?: string;
  private accessToken?: string;
  private refreshToken?: string;
  private clientId?: string;
  private clientSecret?: string;
  private tokenAuthMethod = "none";
  private sessionId?: string;
  private requestId = 0;
  private pendingCancel?: () => Promise<void>;
  private events: TranscriptEvent[] = [];
  private closed = false;

  constructor(options: FixtureMcpClientOptions) {
    this.options = {
      ...options,
      headers: { ...options.headers },
      trustedOrigins: [...(options.trustedOrigins ?? [])],
    };
    this.accessToken = options.bearerToken;
    this.safeUrl(options.endpoint);
  }

  setCallerHeader(name: string, value?: string): void {
    const normalized = name.toLowerCase();
    if (
      !/^x-[a-z0-9-]+$/.test(normalized) ||
      /^(?:x-gram-|x-forwarded-|x-mcp-|x-agent-)/.test(normalized)
    )
      throw new Error("Only non-control caller headers may be changed");
    const headers = new Headers(this.options.headers);
    if (value === undefined) headers.delete(name);
    else headers.set(name, value);
    const updated: Record<string, string> = {};
    headers.forEach((headerValue, headerName) => {
      updated[headerName] = headerValue;
    });
    this.options.headers = updated;
  }

  get transcript(): readonly TranscriptEvent[] {
    return this.events.map((event) => ({ ...event }));
  }

  private safeUrl(value: string): URL {
    const url = new URL(value);
    const loopback = ["127.0.0.1", "localhost", "[::1]"].includes(url.hostname);
    const trusted = this.options.trustedOrigins?.includes(url.origin);
    if (
      url.username ||
      url.password ||
      url.hash ||
      (!loopback && !trusted) ||
      (url.protocol !== "https:" &&
        !(
          loopback &&
          url.protocol === "http:" &&
          this.options.allowInsecureLoopback
        ))
    ) {
      throw new Error(
        "Fixture destination is not an approved secure or loopback origin",
      );
    }
    return url;
  }

  private responseEvents = new WeakMap<Response, TranscriptEvent>();

  private async request(
    url: string,
    operation: string,
    init: RequestInit = {},
  ): Promise<Response> {
    if (this.closed) throw new Error("Client is closed");
    this.safeUrl(url);
    // Never forward resource credentials to discovery, registration, or token endpoints.
    let response: Response;
    try {
      response = await fetch(url, {
        ...init,
        redirect: "error",
        signal: init.signal
          ? AbortSignal.any([init.signal, AbortSignal.timeout(15_000)])
          : AbortSignal.timeout(15_000),
      });
    } catch {
      this.events.push({ operation });
      throw new Error(`${operation} transport failed`);
    }
    const event: TranscriptEvent = { operation, status: response.status };
    if (response.status === 503) {
      event.responseReceivedAtMs = performance.now();
      const seconds = unavailableRetryAfter(
        response.headers.get("Retry-After"),
        Date.now(),
      );
      if (seconds !== undefined) event.retryAfterSeconds = seconds;
    }
    if (init.method === "POST" && url === this.options.endpoint)
      event.sessionSent = new Headers(init.headers).has("mcp-session-id");
    if (!response.ok) {
      const failure = await responseDiagnostics(response);
      Object.assign(event, failure.diagnostics);
      if ("body" in failure)
        await this.captureProtocolFailure(operation, failure.body);
    }
    this.events.push(event);
    this.responseEvents.set(response, event);
    return response;
  }

  private async captureProtocolFailure(
    operation: string,
    body: unknown,
  ): Promise<void> {
    if (!this.options.onProtocolFailure) return;
    try {
      // Successful HTTP responses can carry RPC errors, including in SSE. Apply
      // the same capture cap to those already-parsed messages before delivery.
      const encoded = JSON.stringify(body);
      if (encoded === undefined || Buffer.byteLength(encoded, "utf8") > 16_384)
        return;
      await this.options.onProtocolFailure({
        operation,
        body: JSON.parse(encoded),
      });
    } catch {
      // Capture is observational: never replace the actual protocol failure.
    }
  }

  private async jsonResponse(
    response: Response,
    operation: string,
  ): Promise<JsonObject> {
    if (!response.ok) {
      await response.body?.cancel();
      throw new Error(`${operation} failed (HTTP ${response.status})`);
    }
    try {
      return object(await response.json());
    } catch {
      throw new Error(`${operation} returned invalid JSON`);
    }
  }

  async discover(): Promise<Discovery> {
    const endpoint = this.safeUrl(this.options.endpoint);
    const probe = await this.request(endpoint.href, "resource-discovery", {
      headers: { Accept: "application/json, text/event-stream" },
    });
    const challenge = probe.headers.get("www-authenticate");
    await probe.body?.cancel();
    const advertised = challenge?.match(/\bresource_metadata="([^"]+)"/i)?.[1];
    const paths = advertised
      ? [advertised]
      : [
          `${endpoint.origin}/.well-known/oauth-protected-resource${endpoint.pathname === "/" ? "" : endpoint.pathname}`,
          `${endpoint.origin}/.well-known/oauth-protected-resource`,
        ];
    let resourceMetadata: JsonObject | undefined;
    for (const path of [...new Set(paths)]) {
      const response = await this.request(path, "protected-resource-metadata");
      if (response.status === 404 && !advertised) {
        await response.body?.cancel();
        continue;
      }
      resourceMetadata = await this.jsonResponse(
        response,
        "protected-resource-metadata",
      );
      break;
    }
    if (!resourceMetadata)
      throw new Error("Protected resource metadata not found");
    const resource = string(resourceMetadata.resource);
    this.safeUrl(resource);
    if (new URL(resource).origin !== endpoint.origin)
      throw new Error("Resource metadata origin mismatch");
    const servers = resourceMetadata.authorization_servers;
    if (!Array.isArray(servers) || !servers.length)
      throw new Error("No authorization server advertised");
    const issuer = this.safeUrl(string(servers[0]));
    if (issuer.search) throw new Error("Invalid authorization server issuer");
    const suffix =
      issuer.pathname === "/" ? "" : issuer.pathname.replace(/\/$/, "");
    const response = await this.request(
      `${issuer.origin}/.well-known/oauth-authorization-server${suffix}`,
      "authorization-server-metadata",
    );
    const as = await this.jsonResponse(
      response,
      "authorization-server-metadata",
    );
    if (as.issuer !== issuer.href && as.issuer !== string(servers[0]))
      throw new Error("Authorization server issuer mismatch");
    if (
      Array.isArray(as.code_challenge_methods_supported) &&
      !as.code_challenge_methods_supported.includes("S256")
    )
      throw new Error("Authorization server does not support S256");
    const metadata: Discovery = {
      resource,
      ...(Array.isArray(resourceMetadata.scopes_supported)
        ? { scopes: resourceMetadata.scopes_supported.map(string) }
        : {}),
      authorizationServer: string(servers[0]),
      authorizationEndpoint: this.safeUrl(string(as.authorization_endpoint))
        .href,
      tokenEndpoint: this.safeUrl(string(as.token_endpoint)).href,
      ...(as.registration_endpoint
        ? {
            registrationEndpoint: this.safeUrl(string(as.registration_endpoint))
              .href,
          }
        : {}),
    };
    this.revocationEndpoint = as.revocation_endpoint
      ? this.safeUrl(string(as.revocation_endpoint)).href
      : undefined;
    this.metadata = metadata;
    return { ...metadata };
  }

  async beginAuthorization(): Promise<{
    authorizationUrl: string;
    complete: () => Promise<void>;
    cancel: () => Promise<void>;
  }> {
    if (this.pendingCancel)
      throw new Error("Authorization already in progress");
    const metadata = this.metadata ?? (await this.discover());
    if (!metadata.registrationEndpoint)
      throw new Error("Dynamic registration is unavailable");
    const state = randomBytes(32).toString("base64url");
    const verifier = randomBytes(32).toString("base64url");
    let resolveCode!: (code: string) => void;
    let rejectCode!: (error: Error) => void;
    const codePromise = new Promise<string>((resolve, reject) => {
      resolveCode = resolve;
      rejectCode = reject;
    });
    void codePromise.catch(() => {});
    let settled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let server: Server;
    const stop = async () => {
      if (timer) clearTimeout(timer);
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
        server.closeAllConnections();
      });
    };
    const cancel = async () => {
      if (!settled) {
        settled = true;
        rejectCode(new Error("Authorization cancelled"));
      }
      await stop();
      if (this.pendingCancel === cancel) this.pendingCancel = undefined;
    };
    server = createServer((req, res) => {
      const url = new URL(req.url ?? "/", "http://127.0.0.1");
      res.setHeader("Content-Type", "text/plain; charset=utf-8");
      res.setHeader("Cache-Control", "no-store");
      res.setHeader("Content-Security-Policy", "default-src 'none'");
      if (req.method !== "GET" || url.pathname !== "/callback") {
        res.writeHead(404).end("Not found");
        return;
      }
      const received = url.searchParams.get("state") ?? "";
      if (
        settled ||
        url.searchParams.getAll("state").length !== 1 ||
        Buffer.byteLength(received) !== Buffer.byteLength(state) ||
        !timingSafeEqual(Buffer.from(received), Buffer.from(state))
      ) {
        res
          .writeHead(400)
          .end(
            "Invalid authorization state. Return to the test and try again.",
          );
        return;
      }
      settled = true;
      if (
        url.searchParams.has("error") ||
        url.searchParams.getAll("code").length !== 1 ||
        !url.searchParams.get("code")
      ) {
        rejectCode(new Error("Authorization callback failed"));
        res
          .writeHead(400)
          .end("Authorization failed. You can close this window.");
      } else {
        resolveCode(url.searchParams.get("code")!);
        res.end(
          "Authorization received. You can close this window and return to the test.",
        );
      }
      res.once("finish", () => {
        void stop();
      });
    });
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(0, "127.0.0.1", resolve);
    });
    this.pendingCancel = cancel;
    timer = setTimeout(() => {
      void cancel();
    }, 120_000);
    timer.unref();
    const address = server.address();
    if (!address || typeof address === "string") {
      await cancel();
      throw new Error("Callback listener unavailable");
    }
    const redirectUri = `http://127.0.0.1:${address.port}/callback`;
    try {
      const registration = await this.jsonResponse(
        await this.request(metadata.registrationEndpoint, "register", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            client_name:
              this.options.clientName ?? "Speakeasy auth E2E fixture",
            redirect_uris: [redirectUri],
            grant_types: ["authorization_code", "refresh_token"],
            response_types: ["code"],
            token_endpoint_auth_method: "none",
          }),
        }),
        "register",
      );
      this.clientId = string(registration.client_id);
      this.tokenAuthMethod =
        typeof registration.token_endpoint_auth_method === "string"
          ? registration.token_endpoint_auth_method
          : registration.client_secret
            ? "client_secret_basic"
            : "none";
      if (
        !["none", "client_secret_post", "client_secret_basic"].includes(
          this.tokenAuthMethod,
        )
      )
        throw new Error("Unsupported token authentication method");
      this.clientSecret = registration.client_secret
        ? string(registration.client_secret)
        : undefined;
      if (this.tokenAuthMethod !== "none" && !this.clientSecret)
        throw new Error("Missing client secret");
      const url = new URL(metadata.authorizationEndpoint);
      const params = {
        response_type: "code",
        client_id: this.clientId,
        redirect_uri: redirectUri,
        state,
        code_challenge: createHash("sha256")
          .update(verifier)
          .digest("base64url"),
        code_challenge_method: "S256",
        resource: metadata.resource,
        ...(metadata.scopes?.length
          ? { scope: metadata.scopes.join(" ") }
          : {}),
      };
      for (const [key, value] of Object.entries(params))
        url.searchParams.set(key, value);
      let completion: Promise<void> | undefined;
      return {
        authorizationUrl: url.href,
        cancel,
        complete: () => {
          completion ??= (async () => {
            try {
              const code = await codePromise;
              await this.exchange({
                grant_type: "authorization_code",
                code,
                code_verifier: verifier,
                redirect_uri: redirectUri,
                resource: metadata.resource,
              });
            } finally {
              await cancel();
            }
          })();
          return completion;
        },
      };
    } catch (error) {
      await cancel();
      throw error;
    }
  }

  private oauthRequest(
    url: string,
    operation: string,
    params: Record<string, string>,
  ): Promise<Response> {
    if (!this.clientId) throw new Error("No registered OAuth client");
    const body = new URLSearchParams({ ...params, client_id: this.clientId });
    const headers: Record<string, string> = {
      "Content-Type": "application/x-www-form-urlencoded",
    };
    if (this.tokenAuthMethod === "client_secret_post")
      body.set("client_secret", this.clientSecret!);
    if (this.tokenAuthMethod === "client_secret_basic") {
      const encode = (s: string) =>
        new URLSearchParams({ v: s }).toString().slice(2);
      headers.Authorization = `Basic ${Buffer.from(`${encode(this.clientId)}:${encode(this.clientSecret!)}`).toString("base64")}`;
    }
    return this.request(url, operation, { method: "POST", headers, body });
  }

  private async exchange(params: Record<string, string>): Promise<void> {
    if (!this.metadata) throw new Error("Discovery required");
    const token = await this.jsonResponse(
      await this.oauthRequest(this.metadata.tokenEndpoint, "token", params),
      "token",
    );
    if (string(token.token_type).toLowerCase() !== "bearer")
      throw new Error("Unsupported token type");
    this.accessToken = string(token.access_token);
    // A new grant starts a new transport session; refresh retains the current one.
    if (params.grant_type === "authorization_code") this.sessionId = undefined;
    if (token.refresh_token !== undefined)
      this.refreshToken = string(token.refresh_token);
    else if (params.grant_type === "authorization_code")
      this.refreshToken = undefined;
  }

  async refresh(): Promise<void> {
    if (!this.refreshToken || !this.metadata)
      throw new Error("No refresh token available");
    await this.exchange({
      grant_type: "refresh_token",
      refresh_token: this.refreshToken,
      resource: this.metadata.resource,
    });
  }

  async revoke(): Promise<void> {
    if (!this.revocationEndpoint || !this.accessToken)
      throw new Error("Token revocation unavailable");
    for (const [token, hint] of [
      [this.refreshToken, "refresh_token"],
      [this.accessToken, "access_token"],
    ] as const) {
      if (!token) continue;
      const response = await this.oauthRequest(
        this.revocationEndpoint,
        "revoke",
        { token, token_type_hint: hint },
      );
      await response.body?.cancel();
      if (!response.ok)
        throw new Error(`revoke failed (HTTP ${response.status})`);
    }
    // Keep the revoked access token to exercise server rejection, never silently authorize.
    this.refreshToken = undefined;
  }

  private async rpc(
    method: string,
    params?: JsonObject,
    notification = false,
    signal?: AbortSignal,
  ): Promise<unknown> {
    const id = notification ? undefined : ++this.requestId;
    const headers = new Headers(this.options.headers);
    headers.set("Content-Type", "application/json");
    headers.set("Accept", "application/json, text/event-stream");
    if (this.accessToken)
      headers.set("Authorization", `Bearer ${this.accessToken}`);
    if (this.sessionId) headers.set("mcp-session-id", this.sessionId);
    if (method !== "initialize")
      headers.set("MCP-Protocol-Version", this.protocolVersion);
    const response = await this.request(this.options.endpoint, method, {
      method: "POST",
      headers,
      body: JSON.stringify({ jsonrpc: "2.0", id, method, params }),
      signal,
    });
    if (!response.ok) {
      await response.body?.cancel();
      throw new Error(`${method} failed (HTTP ${response.status})`);
    }
    const session = response.headers.get("mcp-session-id");
    if (session) this.sessionId = session;
    if (notification) {
      await response.body?.cancel();
      return undefined;
    }
    let message: JsonObject | undefined;
    if (response.headers.get("content-type")?.includes("text/event-stream")) {
      if (!response.body) throw new Error("Missing SSE body");
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      try {
        while (!message) {
          const { value, done } = await reader.read();
          buffer += decoder.decode(value, { stream: !done });
          if (buffer.length > 1_048_576) throw new Error("SSE event too large");
          let match: RegExpExecArray | null;
          while ((match = /\r?\n\r?\n/.exec(buffer))) {
            const event = buffer.slice(0, match.index);
            buffer = buffer.slice(match.index + match[0].length);
            const data = event
              .split(/\r?\n/)
              .filter((line) => line.startsWith("data:"))
              .map((line) => line.slice(5).replace(/^ /, ""))
              .join("\n");
            if (!data) continue;
            let candidate: JsonObject;
            try {
              candidate = object(JSON.parse(data));
            } catch {
              throw new Error("Invalid SSE JSON");
            }
            if (candidate.id === id) {
              message = candidate;
              break;
            }
          }
          if (done) break;
        }
      } finally {
        await reader.cancel();
      }
    } else {
      message = await this.jsonResponse(response, method);
    }
    if (!message || message.id !== id || message.jsonrpc !== "2.0")
      throw new Error("Invalid JSON-RPC response");
    if ("error" in message) {
      const event = this.responseEvents.get(response);
      if (event) Object.assign(event, errorDiagnostics(message));
      await this.captureProtocolFailure(method, message);
      throw new Error(`${method} returned JSON-RPC error`);
    }
    if (!("result" in message)) throw new Error("Missing JSON-RPC result");
    return message.result;
  }

  private protocolVersion = "2025-03-26";
  async initialize(): Promise<unknown> {
    // Initialize starts a new MCP session, not a request on the previous session.
    this.sessionId = undefined;
    const result = object(
      await this.rpc("initialize", {
        protocolVersion: this.protocolVersion,
        capabilities: {},
        clientInfo: {
          name: this.options.clientName ?? "auth-e2e",
          version: "1.0.0",
        },
      }),
    );
    this.protocolVersion = string(result.protocolVersion);
    await this.rpc("notifications/initialized", undefined, true);
    return result;
  }
  async listTools(): Promise<{
    tools: Array<{ name: string; [key: string]: unknown }>;
  }> {
    const result = object(await this.rpc("tools/list"));
    if (!Array.isArray(result.tools)) throw new Error("Invalid tools list");
    return {
      tools: result.tools.map((tool) => {
        const value = object(tool);
        return { ...value, name: string(value.name) };
      }),
    };
  }
  callTool(
    name: string,
    args: Record<string, unknown> = {},
    signal?: AbortSignal,
  ): Promise<unknown> {
    return this.rpc("tools/call", { name, arguments: args }, false, signal);
  }
  async close(): Promise<void> {
    await this.pendingCancel?.();
    this.closed = true;
    this.accessToken = undefined;
    this.refreshToken = undefined;
    this.clientSecret = undefined;
  }
}
