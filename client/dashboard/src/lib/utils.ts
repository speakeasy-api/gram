import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

// __GRAM_SERVER_URL__ is the server's authoritative URL (injected at build
// time from GRAM_SERVER_URL). The viteprod profile leaves it unset, so fall
// back to the current origin instead of degrading to `undefined/rpc/...`.
// Use everywhere except the playground — MCP configs, callback URL
// displays, anything operator-facing.
export function getServerURL(): string {
  return __GRAM_SERVER_URL__ ?? window.location.origin;
}

// tunnel.speakeasy.com in prod, tunnel-pr-N.<env> in previews (single label
// keeps the wildcard cert valid), tunnel.<host> otherwise.
export function tunnelGatewayURL(): string {
  const server = new URL(getServerURL());
  const host =
    server.host === "app.getgram.ai"
      ? "tunnel.speakeasy.com"
      : /^pr-\d+\./.test(server.host)
        ? `tunnel-${server.host}`
        : `tunnel.${server.host}`;
  return `${server.protocol === "http:" ? "ws" : "wss"}://${host}/connect`;
}

// __PLAYGROUND_PROXY_URL__ is the dashboard origin in dev (so browser-side MCP
// requests can ride the vite proxy and ferry cookies the Vercel AI SDK can't
// forward across origins), and undefined in prod. Consumed by the playground
// and the remote-MCP tools listing; everything else uses getServerURL().
export function getPlaygroundMcpBaseURL(): string {
  return __PLAYGROUND_PROXY_URL__ ?? getServerURL();
}

// mcpConnectionUrl rebases a display MCP URL (built from getServerURL) onto the
// dev proxy origin so a browser MCP client connects same-origin — the Vercel AI
// SDK doesn't carry credentials cross-origin, and the gateway's proxied SSE
// response doesn't survive the cross-origin hop. In prod the proxy base equals
// getServerURL so this is a no-op, and custom-domain URLs (a different origin)
// are left untouched.
export function mcpConnectionUrl(
  displayUrl: string | undefined,
): string | undefined {
  if (!displayUrl) return displayUrl;
  const base = getPlaygroundMcpBaseURL();
  const server = getServerURL();
  if (base === server) return displayUrl;
  try {
    const url = new URL(displayUrl);
    if (url.origin !== new URL(server).origin) return displayUrl;
    const baseUrl = new URL(base);
    url.protocol = baseUrl.protocol;
    url.host = baseUrl.host;
    return url.toString();
  } catch {
    return displayUrl;
  }
}

// firstPartyConnectUrl derives the runtime first-party connect entry point
// (`/x/mcp/<slug>/connect/first-party`) for a display MCP URL. It's always built
// on the Gram server origin (getServerURL), never the display URL's origin: a
// custom-domain endpoint's display URL is `https://<customer-domain>/mcp/<slug>`,
// but the connect page is a Gram auth surface — the IDP callback, routes, and
// any session live on the Gram origin, not the customer's MCP domain. Opened as
// a top-level new tab; the IDP flow is state-based so no dev proxy is needed.
// Returns undefined when the slug can't be derived.
export function firstPartyConnectUrl(
  displayUrl: string | undefined,
): string | undefined {
  if (!displayUrl) return undefined;
  try {
    const slug = new URL(displayUrl).pathname.split("/").filter(Boolean).pop();
    if (!slug) return undefined;
    const url = new URL(getServerURL());
    url.pathname = `/x/mcp/${slug}/connect/first-party`;
    return url.toString();
  } catch {
    return undefined;
  }
}

export function buildLoginRedirectURL(redirectTo: string | null): string {
  let href = `${getServerURL()}/rpc/auth.login`;
  if (redirectTo) href += `?redirect=${encodeURIComponent(redirectTo)}`;
  return href;
}

/**
 * True on macOS/iOS — used to pick ⌘ vs Ctrl in keyboard shortcut hints.
 * Defaults to true during SSR/tests where `navigator` is unavailable.
 */
export function isMacPlatform(): boolean {
  if (typeof navigator === "undefined") return true;
  return /mac|iphone|ipad|ipod/i.test(
    navigator.platform || navigator.userAgent,
  );
}

export function capitalize(str: string): string {
  return str.charAt(0).toUpperCase() + str.slice(1);
}

/**
 * Turn a URL slug into a Title Case display string, e.g.
 * "custom-tools" -> "Custom Tools". Intended for the static (literal) segments
 * of a route path — not for dynamic slug params like toolset or user
 * identifiers, which should keep their original casing.
 */
export function titleCaseSlug(slug: string): string {
  return slug.split("-").filter(Boolean).map(capitalize).join(" ");
}

export function assert(condition: unknown, message: string): asserts condition {
  if (!condition) {
    throw new Error(message);
  }
}

export function getCustomDomainCNAME(): string {
  try {
    const url = new URL(getServerURL());
    const parts = url.hostname.split(".");
    if (parts.length > 2) {
      parts[0] = parts[0] === "app" ? "cname" : `cname.${parts[0]}`;
    }
    return `${parts.join(".")}.`;
  } catch {
    return "cname.getgram.ai.";
  }
}
