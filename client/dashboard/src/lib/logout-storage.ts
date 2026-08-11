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

/** localStorage entries that outlive a logout, as key/value pairs. */
export type PreservedStorage = ReadonlyArray<readonly [string, string]>;

/**
 * Reads the localStorage entries that survive logout.
 *
 * Exported for the logout request itself: that response carries
 * `Clear-Site-Data: "cookies", "storage"`, and every engine finishes emptying
 * localStorage before the response reaches the page, so there is nothing left
 * to preserve by the time a response handler runs. Capturing before the request
 * goes out and handing the result to `clearStorageForLogout` is what keeps the
 * theme and favorites across a logout.
 */
export function capturePreservedStorage(): PreservedStorage {
  if (typeof window === "undefined") return [];

  const preserved: Array<readonly [string, string]> = [];

  try {
    const local = window.localStorage;

    for (let i = 0; i < local.length; i++) {
      const key = local.key(i);
      if (!key || !shouldPreserveLocalStorageKey(key)) continue;

      const value = local.getItem(key);
      if (value !== null) {
        preserved.push([key, value]);
      }
    }
  } catch {
    // Storage blocked — nothing persisted, nothing to preserve.
  }

  return preserved;
}

export function restorePreservedStorage(preserved: PreservedStorage): void {
  if (typeof window === "undefined" || preserved.length === 0) return;

  try {
    const local = window.localStorage;
    for (const [key, value] of preserved) {
      local.setItem(key, value);
    }
  } catch {
    // Storage blocked — nothing to restore into.
  }
}

/**
 * Empties storage, keeping the entries that outlive a logout.
 *
 * Pass `preserved` when the store may already have been emptied — after a
 * `Clear-Site-Data` response, a snapshot taken before the request is the only
 * remaining copy of those entries. Callers clearing an intact store omit it and
 * the entries are read from storage directly.
 */
export function clearStorageForLogout(
  preserved: PreservedStorage = capturePreservedStorage(),
): void {
  if (typeof window === "undefined") return;

  try {
    window.localStorage.clear();
  } catch {
    // Storage blocked — nothing persisted, nothing to clear.
  }

  restorePreservedStorage(preserved);

  try {
    window.sessionStorage.clear();
  } catch {
    // Storage blocked — nothing persisted, nothing to clear.
  }
}
