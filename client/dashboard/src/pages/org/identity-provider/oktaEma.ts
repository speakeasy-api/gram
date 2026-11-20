import { GramError } from "@gram/client/models/errors/gramerror.js";

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

/** The deployment cannot hold identity provider connections at all. */
export function connectionsUnavailable(error: unknown): boolean {
  return error instanceof GramError && error.statusCode === 503;
}
