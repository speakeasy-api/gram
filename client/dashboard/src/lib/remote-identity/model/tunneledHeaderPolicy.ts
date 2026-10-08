/**
 * The header rules a tunneled MCP server enforces, mirrored so the form can
 * refuse a row while it is being edited instead of on Save. The server is the
 * control that holds; this list only has to agree with it.
 *
 * Names are folded case-insensitively, with underscores read as dashes, because
 * some upstream servers treat `X_Foo` as `X-Foo`.
 */

function headerKey(name: string): string {
  return name.trim().replaceAll("_", "-").toLowerCase();
}

/** Speakeasy's own request headers: API keys, sessions, project, consent. */
const SPEAKEASY_PREFIX = "gram-";

/** The documented Speakeasy-AI-* names for the same headers. */
const SPEAKEASY_AI_PREFIX = "speakeasy-ai-";

/** The tunnel transport fields exchanged with the gateway and agent. */
const TUNNEL_PREFIX = "x-gram-tunnel-";

const PROTECTED_INBOUND = new Set([
  "authorization",
  "proxy-authorization",
  "cookie",
  "set-cookie",
  "x-gram-agent-version",
  "x-speakeasy-identity",
]);

const RESERVED_DESTINATIONS = new Set([
  "connection",
  "keep-alive",
  "proxy-authenticate",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
  "content-length",
  "host",
  "accept-encoding",
  "mcp-session-id",
  "mcp-protocol-version",
  "mcp-method",
  "mcp-name",
  "last-event-id",
]);

const MCP_PARAM_PREFIX = "mcp-param-";

/**
 * Whether an inbound header carries a Speakeasy credential, session, caller
 * assertion or tunnel field, so a tunneled server never passes it through.
 */
export function isProtectedInboundHeader(name: string): boolean {
  const key = headerKey(name);
  return (
    PROTECTED_INBOUND.has(key) ||
    key.startsWith(SPEAKEASY_PREFIX) ||
    key.startsWith(SPEAKEASY_AI_PREFIX) ||
    key.startsWith(TUNNEL_PREFIX)
  );
}

/**
 * Whether a tunneled server refuses a header with this name. Authorization is
 * allowed: a static service credential is a legitimate configuration.
 */
export function isReservedTunneledHeaderName(name: string): boolean {
  const key = headerKey(name);
  if (key === "authorization") return false;
  return (
    isProtectedInboundHeader(name) ||
    RESERVED_DESTINATIONS.has(key) ||
    (key.startsWith(MCP_PARAM_PREFIX) && key.length > MCP_PARAM_PREFIX.length)
  );
}

// An HTTP field name: one or more token characters (RFC 9110 section 5.6.2).
const FIELD_NAME = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;

/** Whether `name` is a valid HTTP header name once surrounding spaces go. */
export function isValidHeaderName(name: string): boolean {
  return FIELD_NAME.test(name.trim());
}

// Control characters other than horizontal tab cannot appear in a value.
// eslint-disable-next-line no-control-regex
const INVALID_VALUE = /[\u0000-\u0008\u000a-\u001f\u007f]/;

/** Whether `value` can be sent as an HTTP header value. */
export function isValidHeaderValue(value: string): boolean {
  return !INVALID_VALUE.test(value);
}

/**
 * The key two header names collide on for a tunneled source: case-insensitive,
 * with underscores read as dashes, matching how the server checks duplicates.
 */
export function tunneledHeaderNameKey(name: string): string {
  return headerKey(name);
}
