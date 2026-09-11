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
// in lockstep. Left in place after restore so a later impersonation document
// can still recover the admin snapshot; a safe capture overwrites it.
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

// Empty `[]` is a real snapshot (admin has no theme/favorites). Distinct
// from "never captured", which is when a WorkOS landing may still seal
// the live store on the rising edge of impersonation.
let hasSnapshot = false;

// WorkOS impersonation and org support sessions must not refresh the
// snapshot: that would replace the platform admin's own theme/favorites
// with whatever the impersonated org wrote. Auth writes this before logout.
let sessionIsImpersonating = false;

// Theme/favorite upserts stay closed until Auth classifies the session,
// so a mount-time theme apply cannot seal customer keys while auth.info
// is still pending on an impersonation document.
let sessionClassified = false;

export function setPreservedStorageImpersonating(value: boolean): void {
  // WorkOS impersonation lands in a new document with no heap snapshot and
  // usually no window.name backup. Seal the live store on the rising edge,
  // before children write the customer org's favorites.
  if (value && !sessionIsImpersonating && !hasSnapshot) {
    capturePreservedStorage();
  }
  sessionIsImpersonating = value;
  sessionClassified = true;
}

/** Drop the in-memory snapshot the way a full navigation would. Tests only. */
export function resetPreservedStorageCapture(): void {
  lastCaptured = [];
  hasSnapshot = false;
  sessionIsImpersonating = false;
  sessionClassified = false;
}

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
    // Persist `[]` too — an empty sealed snapshot must survive navigation
    // so the next document does not treat it as "never captured".
    window.name = `${LOGOUT_PRESERVE_WINDOW_NAME_PREFIX}${JSON.stringify(preserved)}`;
  } catch {
    // window.name unavailable — same-document restore still works.
  }
}

function hasPreservedStorageBackup(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.name.startsWith(LOGOUT_PRESERVE_WINDOW_NAME_PREFIX);
  } catch {
    return false;
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
 * Writes a captured snapshot back into localStorage. The window.name backup
 * stays until a later *safe* capture replaces it — impersonation loads in a
 * new document and must still be able to restore the admin's prefs on exit.
 */
export function restorePreservedStorageBackup(): void {
  if (!hasPreservedStorageBackup()) return;

  const preserved = readPreservedStorageBackup();
  restorePreservedStorage(preserved);
  lastCaptured = preserved;
  hasSnapshot = true;
}

function snapshotForRestore(preserved?: PreservedStorage): PreservedStorage {
  if (preserved && preserved.length > 0) return preserved;
  if (hasSnapshot) return lastCaptured;
  if (lastCaptured.length > 0) return lastCaptured;

  if (hasPreservedStorageBackup()) return readPreservedStorageBackup();

  // An impersonated session's current store is not the admin's prefs.
  if (sessionIsImpersonating) return [];
  return capturePreservedStorage();
}

/**
 * Snapshot theme and favorites only for a normal (non-impersonation) session.
 * While impersonating, returns the last safe snapshot without reading
 * localStorage, so a customer org's keys cannot replace the admin's.
 */
export function capturePreservedStorageIfSafe(): PreservedStorage {
  if (sessionIsImpersonating) {
    return lastCaptured.length > 0
      ? lastCaptured
      : readPreservedStorageBackup();
  }
  return capturePreservedStorage();
}

/**
 * Merge one preserved key into the snapshot. Theme/favorite writes use this
 * so a full localStorage scan cannot pick up another tab's impersonated keys.
 */
export function rememberPreservedStorageKey(key: string, value: string): void {
  if (
    !sessionClassified ||
    sessionIsImpersonating ||
    !shouldPreserveLocalStorageKey(key)
  ) {
    return;
  }

  const current =
    lastCaptured.length > 0 ? lastCaptured : readPreservedStorageBackup();
  const next = new Map(current);
  next.set(key, value);
  lastCaptured = Array.from(next.entries());
  hasSnapshot = true;
  persistPreservedStorageBackup(lastCaptured);
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
  hasSnapshot = true;
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
 * remaining copy of those entries. Callers that omit it restore lastCaptured
 * or the window.name backup, not a live re-read of a possibly mixed store.
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
  hasSnapshot = true;
  persistPreservedStorageBackup(toRestore);

  try {
    window.sessionStorage.clear();
  } catch {
    // Storage blocked — nothing persisted, nothing to clear.
  }
}
