import {
  PRESERVED_LOCAL_STORAGE_KEYS,
  PRESERVED_LOCAL_STORAGE_PREFIXES,
} from "@/lib/local-storage-keys";

const PRESERVED_KEY_SET = new Set<string>(PRESERVED_LOCAL_STORAGE_KEYS);

const LEGACY_USER_STORAGE_KEYS = [
  "pylon_user_email",
  "pylon_user_display_name",
];

// Survives Clear-Site-Data and the /login navigation that follows logout.
// localStorage and sessionStorage are both emptied by that header; window.name
// is not. theme-init.ts reads the same prefix before first paint — keep them
// in lockstep. Cleared as soon as the entries are written back.
export const LOGOUT_PRESERVE_WINDOW_NAME_PREFIX = "gram:logout-preserve:";

function shouldPreserveLocalStorageKey(key: string) {
  return (
    PRESERVED_KEY_SET.has(key) ||
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

// Last snapshot taken in this document. Used when the logout response hook
// cannot find the per-request WeakMap entry (cloned Request identity) and
// when logout times out after Clear-Site-Data has already emptied the store.
let lastCaptured: PreservedStorage = [];

function persistPreservedStorageBackup(preserved: PreservedStorage): void {
  if (typeof window === "undefined") return;

  try {
    const current = window.name;
    if (
      current !== "" &&
      !current.startsWith(LOGOUT_PRESERVE_WINDOW_NAME_PREFIX)
    ) {
      return;
    }
    window.name =
      preserved.length === 0
        ? ""
        : `${LOGOUT_PRESERVE_WINDOW_NAME_PREFIX}${JSON.stringify(preserved)}`;
  } catch {
    // window.name unavailable — same-document restore still works.
  }
}

function readPreservedStorageBackup(): PreservedStorage {
  if (typeof window === "undefined") return [];

  try {
    const raw = window.name;
    if (!raw.startsWith(LOGOUT_PRESERVE_WINDOW_NAME_PREFIX)) return [];

    const parsed: unknown = JSON.parse(
      raw.slice(LOGOUT_PRESERVE_WINDOW_NAME_PREFIX.length),
    );
    if (!Array.isArray(parsed)) return [];

    const preserved: Array<readonly [string, string]> = [];
    for (const entry of parsed) {
      if (
        !Array.isArray(entry) ||
        entry.length !== 2 ||
        typeof entry[0] !== "string" ||
        typeof entry[1] !== "string" ||
        !shouldPreserveLocalStorageKey(entry[0])
      ) {
        continue;
      }
      preserved.push([entry[0], entry[1]]);
    }
    return preserved;
  } catch {
    return [];
  }
}

/**
 * Writes a captured snapshot back into localStorage and drops the
 * navigation-surviving backup so a later site in this tab cannot read it.
 */
export function restorePreservedStorageBackup(): void {
  const preserved = readPreservedStorageBackup();
  if (preserved.length === 0) return;

  restorePreservedStorage(preserved);
  persistPreservedStorageBackup([]);
}

function snapshotForRestore(preserved?: PreservedStorage): PreservedStorage {
  if (preserved && preserved.length > 0) return preserved;
  if (lastCaptured.length > 0) return lastCaptured;

  const backup = readPreservedStorageBackup();
  if (backup.length > 0) return backup;

  return capturePreservedStorage();
}

/**
 * Reads the localStorage entries that survive logout.
 *
 * Exported for the logout request itself: that response carries
 * `Clear-Site-Data: "cookies", "storage"`, and every engine finishes emptying
 * localStorage before the response reaches the page, so there is nothing left
 * to preserve by the time a response handler runs. Capturing before the request
 * goes out and handing the result to `clearStorageForLogout` is what keeps the
 * theme and favorites across a logout.
 *
 * The snapshot is also written to `window.name` so it still exists if logout
 * times out and the page navigates to /login after the browser has already
 * applied Clear-Site-Data — the WeakMap in SdkProvider does not survive that.
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

  lastCaptured = preserved;
  persistPreservedStorageBackup(preserved);
  return preserved;
}

export function restorePreservedStorage(preserved: PreservedStorage): void {
  if (typeof window === "undefined" || preserved.length === 0) return;

  try {
    const local = window.localStorage;
    for (const [key, value] of preserved) {
      if (!shouldPreserveLocalStorageKey(key)) continue;
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
export function clearStorageForLogout(preserved?: PreservedStorage): void {
  if (typeof window === "undefined") return;

  const toRestore = snapshotForRestore(preserved);

  try {
    window.localStorage.clear();
  } catch {
    // Storage blocked — nothing persisted, nothing to clear.
  }

  restorePreservedStorage(toRestore);
  lastCaptured = toRestore;
  persistPreservedStorageBackup(toRestore);

  try {
    window.sessionStorage.clear();
  } catch {
    // Storage blocked — nothing persisted, nothing to clear.
  }
}
