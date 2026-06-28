import {
  PREFERRED_THEME_STORAGE_KEY,
  PROJECT_FAVORITES_STORAGE_PREFIX,
  RECENTS_STORAGE_PREFIX,
} from "@/lib/local-storage-keys";

const PRESERVED_LOCAL_STORAGE_KEYS = new Set([PREFERRED_THEME_STORAGE_KEY]);
const PRESERVED_LOCAL_STORAGE_PREFIXES = [
  PROJECT_FAVORITES_STORAGE_PREFIX,
  RECENTS_STORAGE_PREFIX,
];

function shouldPreserveLocalStorageKey(key: string) {
  return (
    PRESERVED_LOCAL_STORAGE_KEYS.has(key) ||
    PRESERVED_LOCAL_STORAGE_PREFIXES.some((prefix) => key.startsWith(prefix))
  );
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
