import { PREFERRED_THEME_STORAGE_KEY } from "@/lib/local-storage-keys";

const PRESERVED_LOCAL_STORAGE_KEYS = new Set([PREFERRED_THEME_STORAGE_KEY]);

const LEGACY_USER_STORAGE_KEYS = [
  "pylon_user_email",
  "pylon_user_display_name",
];

function shouldPreserveLocalStorageKey(key: string) {
  return PRESERVED_LOCAL_STORAGE_KEYS.has(key);
}

/**
 * Removes user-identifying values written by older dashboard versions.
 *
 * This runs during auth initialization as well as session teardown so users
 * who logged out before the cleanup shipped do not retain stale PII.
 */
export function clearLegacyUserStorage(): void {
  if (typeof window === "undefined") return;

  for (const storage of [window.localStorage, window.sessionStorage]) {
    for (const key of LEGACY_USER_STORAGE_KEYS) {
      storage.removeItem(key);
    }
  }
}

export function clearStorageForLogout(): void {
  const local = typeof window !== "undefined" ? window.localStorage : undefined;
  const session =
    typeof window !== "undefined" ? window.sessionStorage : undefined;

  if (local) {
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
  }

  session?.clear();
}
