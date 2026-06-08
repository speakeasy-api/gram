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

// __PLAYGROUND_PROXY_URL__ is the dashboard origin in dev (so MCP requests
// from the playground can ride the vite proxy and ferry cookies the Vercel
// AI SDK can't forward across origins), and undefined in prod. The
// playground is the only consumer; everything else uses getServerURL().
export function getPlaygroundMcpBaseURL(): string {
  return __PLAYGROUND_PROXY_URL__ ?? getServerURL();
}

export function buildLoginRedirectURL(redirectTo: string | null): string {
  let href = `${getServerURL()}/rpc/auth.login`;
  if (redirectTo) href += `?redirect=${encodeURIComponent(redirectTo)}`;
  return href;
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
