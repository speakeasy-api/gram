import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import type { IdentityMode } from "./identity";

/**
 * What the identity rules say when a header row breaks one. Grouped so the
 * copy lives in one place and callers can assert on a symbol rather than
 * repeating the sentence.
 */
export const IdentityErrors = {
  /** No Identity is selected, so nothing may set a static Authorization. */
  NoAuthorization:
    "Switch to Service Account to use a static Authorization credential.",
  /** User Identity or a Service Account owns the row, so a hand-written one may not. */
  ManagedAuthorization: "Authorization is managed in the Identity section.",
} as const;

export type IdentityError =
  (typeof IdentityErrors)[keyof typeof IdentityErrors];

/** Header names are case-insensitive, and operators type them by hand. */
function isAuthorizationHeader(name: string): boolean {
  return name.trim().toLowerCase() === "authorization";
}

/** The Authorization header the Service Account credential writes. */
export function findStaticAuthorizationHeader(
  headers: readonly RemoteMcpServerHeader[],
): RemoteMcpServerHeader | undefined {
  return headers.find(
    (header) =>
      isAuthorizationHeader(header.name) &&
      !!header.value &&
      !header.valueFromRequestHeader,
  );
}

/** The legacy row that forwards an inbound Authorization straight through. */
export function findPassThroughAuthorizationHeader(
  headers: readonly RemoteMcpServerHeader[],
): RemoteMcpServerHeader | undefined {
  return headers.find(
    (header) =>
      isAuthorizationHeader(header.name) && !!header.valueFromRequestHeader,
  );
}

/**
 * The rule the identity choice imposes on the header list: it owns the
 * Authorization name, so a hand-written row may not claim it.
 *
 * Returns the message to show, or null when the name is allowed.
 */
export function authorizationHeaderGuard(
  mode: IdentityMode,
  headerName: string,
  isManagedAuthorizationRow: boolean,
): IdentityError | null {
  if (!isAuthorizationHeader(headerName)) return null;
  if (mode === "none") return IdentityErrors.NoAuthorization;
  if (isManagedAuthorizationRow) return null;
  return IdentityErrors.ManagedAuthorization;
}

/**
 * The Authorization row the identity choice owns.
 *
 * The identity section publishes this; the header list consumes it. Before
 * this existed the header list re-derived the mode and re-found the row for
 * itself, which meant two places computing the same answer from two separate
 * fetches of the same data — and nothing guaranteeing they agreed.
 */
export type ManagedHeader = {
  readonly ownedBy: "user" | "agent";
  /** Null when the mode owns the name but no row exists yet. */
  readonly headerId: string | null;
};

export function managedAuthorizationHeader(
  mode: IdentityMode,
  headers: readonly RemoteMcpServerHeader[],
): ManagedHeader | null {
  if (mode === "none") return null;
  return {
    ownedBy: mode,
    headerId: findStaticAuthorizationHeader(headers)?.id ?? null,
  };
}

/**
 * Header names are matched the way the proxy matches them: case-insensitive,
 * with underscores read as dashes because some upstreams treat `X_Foo` as
 * `X-Foo`.
 */
function headerKey(name: string): string {
  return name.trim().toLowerCase().replaceAll("_", "-");
}

/** The caller assertion header Speakeasy signs for tunnel upstreams. */
const CALLER_ASSERTION_HEADER = "x-speakeasy-identity";

/**
 * Inbound headers that carry a Speakeasy credential, session, caller
 * assertion or tunnel transport field. A remote MCP server's header may not
 * be populated from one of them; mirrors the proxy's protected-header list.
 */
export function isProtectedInboundHeader(name: string): boolean {
  const key = headerKey(name);
  return (
    key.startsWith("gram-") ||
    key.startsWith("x-gram-tunnel-") ||
    key === "x-gram-agent-version" ||
    key === CALLER_ASSERTION_HEADER ||
    key === "authorization" ||
    key === "proxy-authorization" ||
    key === "cookie" ||
    key === "set-cookie"
  );
}

/** The Mcp-Param-{Name} family; a bare prefix is not a standard header. */
const MCP_PARAM_PREFIX = "mcp-param-";

/**
 * Standard MCP request headers, which an intermediary forwards untouched, so
 * configuration never sets them.
 */
function isStandardMcpRequestHeader(name: string): boolean {
  const key = headerKey(name);
  return (
    key === "mcp-protocol-version" ||
    key === "mcp-method" ||
    key === "mcp-name" ||
    (key.startsWith(MCP_PARAM_PREFIX) && key.length > MCP_PARAM_PREFIX.length)
  );
}

/**
 * What the proxy does with a saved header row that its policy refuses.
 *
 * - `ignored`: the name belongs to the MCP protocol or the caller assertion,
 *   so the row is skipped whether or not it is required.
 * - `suppressed`: an optional row that is not sent.
 * - `blocks-requests`: a required row, so every request fails until it is
 *   changed or removed.
 * - `blocks-unless-upstream-token`: a required Authorization row; requests
 *   fail unless a resolved upstream token supplies Authorization instead.
 */
export type RemoteHeaderPolicyEffect =
  | "ignored"
  | "suppressed"
  | "blocks-requests"
  | "blocks-unless-upstream-token";

export type RemoteHeaderPolicyIssue = {
  readonly reason: "protected-source" | "reserved-name";
  readonly effect: RemoteHeaderPolicyEffect;
};

/**
 * Whether the remote header policy refuses a row, and what that means for
 * requests. Classifies by destination AND source: a custom header populated
 * from Authorization or Gram-Key is refused just as Set-Cookie is, while
 * Authorization populated from a separately supplied header is allowed.
 *
 * Returns null when the row is allowed.
 */
export function remoteHeaderPolicyIssue(header: {
  readonly name: string;
  readonly valueFromRequestHeader?: string;
  readonly isRequired: boolean;
}): RemoteHeaderPolicyIssue | null {
  const name = headerKey(header.name);
  const source = header.valueFromRequestHeader?.trim() ?? "";

  if (name === CALLER_ASSERTION_HEADER || isStandardMcpRequestHeader(name)) {
    return { reason: "reserved-name", effect: "ignored" };
  }

  let reason: RemoteHeaderPolicyIssue["reason"] | null = null;
  if (source && isProtectedInboundHeader(source)) {
    reason = "protected-source";
  } else if (
    name === "set-cookie" ||
    name === "proxy-authorization" ||
    (name === "cookie" && !!source)
  ) {
    reason = "reserved-name";
  }
  if (!reason) return null;

  if (!header.isRequired) return { reason, effect: "suppressed" };
  if (name === "authorization") {
    return { reason, effect: "blocks-unless-upstream-token" };
  }
  return { reason, effect: "blocks-requests" };
}

/** What the row's warning says the proxy does with it. */
export function remoteHeaderPolicyEffectMessage(
  effect: RemoteHeaderPolicyEffect,
): string {
  switch (effect) {
    case "ignored":
      return "Speakeasy never sets this header from configuration, so this row has no effect.";
    case "suppressed":
      return "Speakeasy does not send this header.";
    case "blocks-requests":
      return "Every request to this server fails until this row is changed or removed.";
    case "blocks-unless-upstream-token":
      return "Requests to this server fail unless a connected upstream account supplies Authorization.";
  }
}

/** Why the policy refuses the row, and what to do instead. */
export function remoteHeaderPolicyReasonMessage(
  reason: RemoteHeaderPolicyIssue["reason"],
  source: string,
): string {
  switch (reason) {
    case "protected-source":
      return `Speakeasy does not forward "${source.trim()}" or other Speakeasy credentials to remote MCP servers. Have clients send the upstream credential in a separate request header, store a static credential, or configure upstream OAuth where the server supports it.`;
    case "reserved-name":
      return "This header name cannot be configured on a remote MCP server. Change the name or remove this row.";
  }
}
