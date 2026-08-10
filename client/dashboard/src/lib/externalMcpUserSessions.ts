import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { randomSlugSuffix } from "@/lib/slug";

export const ONBOARD_EXTERNAL_MCP_TO_USER_SESSIONS_FLAG =
  FEATURE_FLAGS.externalMcpUserSessions;

export const DEFAULT_USER_SESSION_DURATION_HOURS = 24 * 14;

const MAX_SLUG_LENGTH = 40;

export function buildUserSessionResourceSlug(baseSlug: string): string {
  const suffix = randomSlugSuffix();
  const normalizedBase = slugify(baseSlug) || "mcp";
  const maxBaseLength = MAX_SLUG_LENGTH - suffix.length - 1;
  const trimmedBase =
    normalizedBase.slice(0, maxBaseLength).replace(/-+$/g, "") || "mcp";
  return `${trimmedBase}-${suffix}`;
}

function slugify(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}
