import { useSyncExternalStore } from "react";

/**
 * Creates a localStorage-backed, key-scoped boolean flag with a module-level
 * pub/sub, so surfaces living in different parts of the tree sync off a
 * single source of truth instead of a shared React context. Used for
 * "dismiss to the sidebar" CTAs (e.g. a banner or dock plus a sidebar resume
 * button) and for persisted per-resource consent (e.g. MCP Connect).
 */
export function createPersistedFlagStore(prefix: string): {
  useFlag: (key: string | undefined) => boolean;
  write: (key: string, value: boolean) => void;
} {
  const listeners = new Set<() => void>();
  const storageKey = (key: string) => `${prefix}:${key}`;
  // Session-scoped fallback for when localStorage is unavailable (storage
  // disabled, some private-browsing modes): writes land here regardless, so
  // dismiss/resume still works for the session — it just won't persist.
  const memory = new Map<string, boolean>();

  function read(key: string): boolean {
    // `write()` always lands the value in `memory`, so once this session has
    // touched a key, `memory` is the freshest source of truth — prefer it.
    // localStorage may be stale (its write threw on quota/disabled) or simply
    // unreadable here, and we must not let either case mask a just-applied
    // dismiss/resume.
    const cached = memory.get(key);
    if (cached !== undefined) return cached;
    try {
      return localStorage.getItem(storageKey(key)) === "true";
    } catch {
      return false;
    }
  }

  function subscribe(listener: () => void) {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  }

  function write(key: string, value: boolean) {
    memory.set(key, value);
    try {
      if (value) {
        localStorage.setItem(storageKey(key), "true");
      } else {
        localStorage.removeItem(storageKey(key));
      }
    } catch {
      // localStorage unavailable — `memory` above keeps the value readable
      // for the session
    }
    listeners.forEach((listener) => listener());
  }

  function useFlag(key: string | undefined): boolean {
    return useSyncExternalStore(
      subscribe,
      () => (key ? read(key) : false),
      () => false,
    );
  }

  return { useFlag, write };
}
