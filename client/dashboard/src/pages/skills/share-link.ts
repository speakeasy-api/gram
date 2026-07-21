/**
 * Base path of the public skill share page. Shared with the route
 * registration in App.tsx so the URL builder and router cannot drift.
 */
export const SHARED_SKILL_BASE_PATH = "/shared/skills";

/** Builds the public URL for a skill share token. */
export function skillShareUrl(token: string): string {
  return `${window.location.origin}${SHARED_SKILL_BASE_PATH}/${token}`;
}
