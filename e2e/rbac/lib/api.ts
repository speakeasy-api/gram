import type { APIRequestContext, APIResponse } from "@playwright/test";
import { env } from "./env.ts";

// A credential the server accepts, in the form a test hands to `ApiClient`.
export type Credential =
  | { kind: "session"; token: string }
  | { kind: "key"; key: string }
  | { kind: "none" };

export type CallOptions = {
  query?: Record<string, string | number | boolean | undefined>;
  body?: unknown;
  // Project slug sent as the `Gram-Project` header. `null` sends none.
  project?: string | null;
  headers?: Record<string, string>;
  // Send the credential as `Authorization: Bearer`, the way an MCP client
  // presents it to the gateway, instead of the management API headers.
  bearer?: boolean;
};

export type Method = "GET" | "POST" | "PUT" | "DELETE";

// Calls the management API as one credential. Never throws on a non-2xx: the
// status is the thing most tests assert on.
export class ApiClient {
  constructor(
    private readonly request: APIRequestContext,
    readonly credential: Credential,
  ) {}

  headersFor(opts: CallOptions = {}): Record<string, string> {
    const headers: Record<string, string> = { ...opts.headers };
    if (opts.bearer) {
      const secret =
        this.credential.kind === "session"
          ? this.credential.token
          : this.credential.kind === "key"
            ? this.credential.key
            : undefined;
      if (secret) headers["Authorization"] = `Bearer ${secret}`;
    } else if (this.credential.kind === "session") {
      headers["Gram-Session"] = this.credential.token;
    } else if (this.credential.kind === "key") {
      headers["Gram-Key"] = this.credential.key;
    }
    const project = opts.project === undefined ? env.projectSlug : opts.project;
    if (project !== null) headers["Gram-Project"] = project;
    return headers;
  }

  call(
    method: Method,
    path: string,
    opts: CallOptions = {},
  ): Promise<APIResponse> {
    const url = new URL(path, env.serverURL);
    for (const [k, v] of Object.entries(opts.query ?? {})) {
      if (v !== undefined) url.searchParams.set(k, String(v));
    }
    const init = {
      headers: this.headersFor(opts),
      ...(opts.body === undefined ? {} : { data: opts.body }),
      failOnStatusCode: false,
      maxRedirects: 0,
    };
    switch (method) {
      case "GET":
        return this.request.get(url.toString(), init);
      case "POST":
        return this.request.post(url.toString(), init);
      case "PUT":
        return this.request.put(url.toString(), init);
      case "DELETE":
        return this.request.delete(url.toString(), init);
    }
  }

  // For setup steps that must succeed: returns the parsed body or throws with
  // the server's answer.
  async ok<T = Record<string, unknown>>(
    method: Method,
    path: string,
    opts: CallOptions = {},
  ): Promise<T> {
    const res = await this.call(method, path, opts);
    if (!res.ok()) {
      throw new Error(
        `${method} ${path} as ${this.credential.kind} failed: ${res.status()} ${await res.text()}`,
      );
    }
    const text = await res.text();
    return (text ? JSON.parse(text) : {}) as T;
  }
}
