import {
  PREFERRED_THEME_STORAGE_KEY,
  PROJECT_FAVORITES_STORAGE_PREFIX,
} from "@/lib/local-storage-keys";

const PRESERVED_LOCAL_STORAGE_KEYS = new Set([PREFERRED_THEME_STORAGE_KEY]);
// Favorites hold only opaque project UUIDs keyed by org id — nothing
// dereferenceable without an authenticated session — so they survive logout.
const PRESERVED_LOCAL_STORAGE_PREFIXES = [PROJECT_FAVORITES_STORAGE_PREFIX];

const LEGACY_USER_STORAGE_KEYS = [
  "pylon_user_email",
  "pylon_user_display_name",
];

function shouldPreserveLocalStorageKey(key: string) {
  return (
    PRESERVED_LOCAL_STORAGE_KEYS.has(key) ||
    PRESERVED_LOCAL_STORAGE_PREFIXES.some((prefix) => key.startsWith(prefix))
  );
}

/**
 * Merely reading `window.localStorage` / `window.sessionStorage` throws a
 * `SecurityError` when the browser blocks persistence (cookies/site data
 * disabled, sandboxed frames). Both cleanup functions run on paths that must
 * survive that — auth initialization and the 401 login redirect — and blocked
 * storage means nothing was persisted anyway, so each storage degrades to a
 * no-op independently.
 */
const STORAGE_ACCESSORS = [
  () => window.localStorage,
  () => window.sessionStorage,
];

/**
 * Removes user-identifying values written by older dashboard versions.
 *
 * This runs during auth initialization as well as session teardown so users
 * who logged out before the cleanup shipped do not retain stale PII.
 */
export function clearLegacyUserStorage(): void {
  if (typeof window === "undefined") return;

  for (const getStorage of STORAGE_ACCESSORS) {
    try {
      const storage = getStorage();
      for (const key of LEGACY_USER_STORAGE_KEYS) {
        storage.removeItem(key);
      }
    } catch {
      // Storage blocked — nothing persisted, nothing to remove.
    }
  }
}

export function clearStorageForLogout(): void {
  if (typeof window === "undefined") return;

  try {
    const local = window.localStorage;
    const preserved = new Map<string, string>();

    for (let i = 0; i < local.length; i++) {
      const key = local.key(i);
      if (!key || !shouldPreserveLocalStorageKey(key)) continue;

      const value = local.getItem(key);
      if (value !== null) {
        preserved.set(key, value);
      }
    }

    local.clear();

    for (const [key, value] of preserved) {
      local.setItem(key, value);
    }
  } catch {
    // Storage blocked — nothing persisted, nothing to clear.
  }

  try {
    window.sessionStorage.clear();
  } catch {
    // Storage blocked — nothing persisted, nothing to clear.
  }
}
