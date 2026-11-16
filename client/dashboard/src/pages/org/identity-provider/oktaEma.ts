import { GramError } from "@gram/client/models/errors/gramerror.js";

/** Where Enterprise Managed Auth setup sent the admin from, and returns to. */
export type EmaReturn = {
  /** The MCP Authentication Provider being set up. */
  providerName: string;
  /** Its page, as a same-origin path. */
  path: string;
};

/**
 * Okta's organization authorization server is named by the org URL itself:
 * "acme.okta.com", "https://acme.okta.com/" and a pasted admin console URL
 * all mean https://acme.okta.com. Undefined when nothing usable was typed.
 */
export function oktaIssuerUrl(domain: string): string | undefined {
  const trimmed = domain.trim();
  if (trimmed === "") return undefined;
  let url: URL;
  try {
    url = new URL(
      /^[a-z]+:\/\//i.test(trimmed) ? trimmed : `https://${trimmed}`,
    );
  } catch {
    return undefined;
  }
  if (url.protocol !== "https:" || !url.hostname.includes(".")) {
    return undefined;
  }
  // Okta's admin console lives on "<org>-admin.okta.com"; its issuer does not.
  const host = url.hostname.replace(
    /-admin(\.(okta|oktapreview)\.com)$/i,
    "$1",
  );
  return `https://${host.toLowerCase()}`;
}

/** Reads the EMA setup a link carried in, refusing anything off this site. */
export function parseEmaReturn(
  providerName: string | null,
  path: string | null,
): EmaReturn | undefined {
  if (!providerName || !path) return undefined;
  // A same-origin path only. Browsers drop tabs and newlines and read "\" as
  // "/" before parsing, so "/\n/evil.example" leaves the site like "//host"
  // does: refuse control characters, then let the URL parser decide.
  // eslint-disable-next-line no-control-regex
  if (!path.startsWith("/") || /[\u0000-\u001f\u007f\\]/.test(path)) {
    return undefined;
  }
  const base = "https://speakeasy.invalid";
  let resolved: URL;
  try {
    resolved = new URL(path, base);
  } catch {
    return undefined;
  }
  if (resolved.origin !== base) return undefined;
  return {
    providerName,
    path: `${resolved.pathname}${resolved.search}${resolved.hash}`,
  };
}

/** The provider page again, asking it to turn EMA on with this issuer. */
export function emaReturnHref(target: EmaReturn, issuerUrl: string): string {
  // The path may carry its own query or hash; add to them, don't append blindly.
  const url = new URL(target.path, "https://speakeasy.invalid");
  url.searchParams.set("ema", issuerUrl);
  return `${url.pathname}${url.search}${url.hash}`;
}

/** The deployment cannot hold identity provider connections at all. */
export function connectionsUnavailable(error: unknown): boolean {
  return error instanceof GramError && error.statusCode === 503;
}
