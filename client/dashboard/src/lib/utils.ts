import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// __GRAM_SERVER_URL__ is the server's authoritative URL (injected at build
// time from GRAM_SERVER_URL). The viteprod profile leaves it unset, so fall
// back to the current origin instead of degrading to `undefined/rpc/...`.
// Use everywhere except the playground — MCP configs, callback URL
// displays, anything operator-facing.
export function getServerURL(): string {
  // On the setup subdomain, route API calls through the current origin
  // (Vite proxy in dev, same-origin in prod) so that session cookies —
  // which are scoped to the setup host — are included by the browser.
  if (isSetupDomain()) {
    return window.location.origin;
  }
  return __GRAM_SERVER_URL__ ?? window.location.origin;
}

// __PLAYGROUND_PROXY_URL__ is the dashboard origin in dev (so MCP requests
// from the playground can ride the vite proxy and ferry cookies the Vercel
// AI SDK can't forward across origins), and undefined in prod. The
// playground is the only consumer; everything else uses getServerURL().
export function getPlaygroundMcpBaseURL(): string {
  return __PLAYGROUND_PROXY_URL__ ?? getServerURL();
}

export function buildLoginRedirectURL(redirectTo: string | null): string {
  // On the setup subdomain, route auth through the current origin so the
  // session cookie is scoped to setup.* (Vite proxies /rpc to the server).
  const base = isSetupDomain() ? window.location.origin : getServerURL();
  let href = `${base}/rpc/auth.login`;
  if (redirectTo) href += `?redirect=${encodeURIComponent(redirectTo)}`;
  return href;
}

export function titleCase(str: string) {
  return str.replace(/\b\w/g, (char) => char.toUpperCase());
}

export function capitalize(str: string) {
  return str.charAt(0).toUpperCase() + str.slice(1);
}

export function assert(condition: unknown, message: string): asserts condition {
  if (!condition) {
    throw new Error(message);
  }
}

/**
 * Returns true when the dashboard is served from the setup subdomain
 * (setup.getgram.ai in prod, setup.localhost in dev).
 */
export function isSetupDomain(): boolean {
  const hostname = window.location.hostname;
  return hostname.startsWith("setup.");
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
