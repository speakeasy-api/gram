import type { CustomDomain } from "@gram/client/models/components/customdomain.js";

/**
 * Base path of the public skill share page. Shared with the route
 * registration in App.tsx so the URL builder and router cannot drift.
 */
export const SHARED_SKILL_BASE_PATH = "/shared/skills";

/**
 * Picks the host share links should use: the org's custom domain when it is
 * verified and activated (the server renders /shared/skills/* on custom
 * domains), otherwise undefined to fall back to the dashboard origin.
 */
export function skillShareDomain(
  domain: CustomDomain | undefined,
): string | undefined {
  return domain?.verified && domain.activated ? domain.domain : undefined;
}

/** Builds the public URL for a skill share token. */
export function skillShareUrl(token: string, customDomain?: string): string {
  const origin = customDomain
    ? `https://${customDomain}`
    : window.location.origin;
  return `${origin}${SHARED_SKILL_BASE_PATH}/${token}`;
}
