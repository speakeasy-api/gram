import crypto from "node:crypto";
import type { APIRequestContext, APIResponse } from "@playwright/test";
import { env } from "./env.ts";

// A Platform MCP connection made the way an MCP client makes one: register an
// OAuth client, send the person through authorization, approve on the connect
// page, exchange the code. The identity provider signs in its current user, so
// the caller picks the persona by setting that user first.

const REDIRECT_URI = "http://127.0.0.1:47911/callback";
const PROTOCOL_VERSION = "2025-06-18";

export const platformMcpURL = (): string => `${env.serverURL}/platform-mcp`;

export type Connection =
  | { outcome: "connected"; token: string; clientId: string }
  | { outcome: string; token?: undefined; clientId?: undefined };

function hiddenField(html: string, name: string): string | undefined {
  return html.match(new RegExp(`name="${name}" value="([^"]*)"`))?.[1];
}

export async function connectPlatformMcp(
  request: APIRequestContext,
): Promise<Connection> {
  const registered = await request.post(`${platformMcpURL()}/register`, {
    data: {
      client_name: "rbac-e2e",
      redirect_uris: [REDIRECT_URI],
      token_endpoint_auth_method: "none",
      grant_types: ["authorization_code", "refresh_token"],
      response_types: ["code"],
    },
    failOnStatusCode: false,
  });
  if (registered.status() !== 201) {
    return { outcome: `register-${registered.status()}` };
  }
  const { client_id: clientId } = (await registered.json()) as {
    client_id: string;
  };

  const verifier = crypto.randomBytes(32).toString("base64url");
  const authorize = new URL(`${platformMcpURL()}/authorize`);
  for (const [k, v] of Object.entries({
    response_type: "code",
    client_id: clientId,
    redirect_uri: REDIRECT_URI,
    state: crypto.randomUUID(),
    code_challenge: crypto
      .createHash("sha256")
      .update(verifier)
      .digest("base64url"),
    code_challenge_method: "S256",
    resource: platformMcpURL(),
  })) {
    authorize.searchParams.set(k, v);
  }
  const page = await request.get(authorize.toString(), {
    maxRedirects: 20,
    failOnStatusCode: false,
  });
  const landed = new URL(page.url());
  // Someone the server cannot place in an organization is sent to pick or
  // create one instead of the connect page.
  if (!landed.pathname.endsWith("/platform-mcp/connect")) {
    return { outcome: `ended-on ${landed.pathname} (${page.status()})` };
  }
  const html = await page.text();
  const state = hiddenField(html, "state");
  const csrf = hiddenField(html, "csrf_token");
  if (!state || !csrf) return { outcome: `connect-page-${page.status()}` };

  const approved = await request.post(`${platformMcpURL()}/connect`, {
    form: {
      state,
      csrf_token: csrf,
      organization_id: hiddenField(html, "organization_id") ?? "",
      action: "approve",
    },
    maxRedirects: 0,
    failOnStatusCode: false,
  });
  const location = approved.headers()["location"];
  const code = location
    ? new URL(location).searchParams.get("code")
    : undefined;
  if (!code) {
    const error = location
      ? new URL(location).searchParams.get("error")
      : undefined;
    return { outcome: `approve-${error ?? approved.status()}` };
  }

  const token = await request.post(`${platformMcpURL()}/token`, {
    form: {
      grant_type: "authorization_code",
      code,
      redirect_uri: REDIRECT_URI,
      client_id: clientId,
      code_verifier: verifier,
      resource: platformMcpURL(),
    },
    failOnStatusCode: false,
  });
  if (!token.ok()) return { outcome: `token-${token.status()}` };
  const { access_token: accessToken } = (await token.json()) as {
    access_token: string;
  };
  return { outcome: "connected", token: accessToken, clientId };
}

// Ends a connection the suite made, so runs do not pile them up.
export async function revokePlatformMcp(
  request: APIRequestContext,
  connection: Connection,
): Promise<void> {
  if (!connection.token) return;
  await request.post(`${platformMcpURL()}/revoke`, {
    form: { token: connection.token, client_id: connection.clientId },
    failOnStatusCode: false,
  });
}

type RpcAnswer = {
  error?: { message?: string };
  result?: {
    isError?: boolean;
    content?: { type: string; text?: string }[];
    tools?: { name: string }[];
  };
};

// Speaks JSON-RPC to the Platform MCP with one bearer token (or none).
export class PlatformMcpClient {
  private nextId = 1;

  constructor(
    private readonly request: APIRequestContext,
    private readonly bearer: string | undefined,
  ) {}

  async rpc(
    method: string,
    params?: unknown,
  ): Promise<{ status: number; answer?: RpcAnswer; text: string }> {
    const res: APIResponse = await this.request.post(platformMcpURL(), {
      headers: {
        Accept: "application/json, text/event-stream",
        "Content-Type": "application/json",
        ...(this.bearer ? { Authorization: `Bearer ${this.bearer}` } : {}),
      },
      data: {
        jsonrpc: "2.0",
        id: this.nextId++,
        method,
        ...(params === undefined ? {} : { params }),
      },
      failOnStatusCode: false,
      maxRedirects: 0,
    });
    const text = await res.text();
    let answer: RpcAnswer | undefined;
    try {
      answer = JSON.parse(text) as RpcAnswer;
    } catch {
      answer = undefined;
    }
    return { status: res.status(), answer, text };
  }

  initialize() {
    return this.rpc("initialize", {
      protocolVersion: PROTOCOL_VERSION,
      capabilities: {},
      clientInfo: { name: "rbac-e2e", version: "1" },
    });
  }
}

// What a Platform MCP answer says about authorization, ignoring everything
// else. Tool errors carry a JSON body with a machine-readable code: a refusal
// is "denied", and any other code is kept as "error:<code>", since an error
// such as a missing argument means the call got past the decision.
export function mcpOutcome(res: {
  status: number;
  answer?: RpcAnswer;
}): string {
  if (res.status === 401) return "unauthenticated";
  if (res.status === 403) return "forbidden";
  if (res.status !== 200) return `http-${res.status}`;
  if (!res.answer) return "unreadable";
  if (res.answer.error) return "rpc-error";
  if (!res.answer.result?.isError) return "ok";
  const text = (res.answer.result.content ?? [])
    .map((c) => c.text ?? "")
    .join(" ");
  let code: string | undefined;
  try {
    code = (JSON.parse(text) as { code?: string }).code;
  } catch {
    code = undefined;
  }
  if (code === "permission_denied" || code === "forbidden") return "denied";
  return `error:${code ?? "unstructured"}`;
}

export function toolErrorText(answer?: RpcAnswer): string {
  if (answer?.error) return answer.error.message ?? "";
  return (answer?.result?.content ?? [])
    .map((c) => c.text ?? "")
    .join(" ")
    .slice(0, 200);
}
