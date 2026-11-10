import { loadState } from "@/pages/platform-admin/rbac-override-state";

/**
 * Returns the X-Gram-Scope-Override header value if the dev override is active,
 * or null if disabled. Called by the SDK fetcher on every request.
 *
 * @param allowed - Whether the caller is permitted to read override data.
 *   Defaults to `import.meta.env.DEV` so the SDK fetcher (which lives outside
 *   auth context) never sends the header in production. Callers that have
 *   verified admin status should pass `import.meta.env.DEV || isAdmin`.
 */
export function getRBACScopeOverrideHeader(
  allowed: boolean = import.meta.env.DEV,
): string | null {
  if (!allowed) return null;
  try {
    const state = loadState();
    if (!state.enabled) return null;
    const parts = Object.entries(state.scopes)
      .filter(([, s]) => {
        if (typeof s === "boolean") return s; // backwards compat
        return s.enabled;
      })
      .map(([scope, s]) => {
        if (typeof s === "boolean") return scope;
        // Include resource IDs if restricted: project:read=proj_1|proj_2
        if (s.resources && s.resources.length > 0) {
          return `${scope}=${s.resources.join("|")}`;
        }
        return scope;
      });
    // The backend treats an absent/empty header as no override. An unknown,
    // nonempty scope produces zero matching grants instead of restoring access.
    return parts.length > 0 ? parts.join(",") : "rbac-override:none";
  } catch {
    return null;
  }
}
