import { useSyncExternalStore } from "react";

type ScopedStorageStore<T> = {
  useValue: (slug: string | undefined) => T;
  write: (slug: string, value: T) => void;
};

export function createScopedStorageStore<T>(
  prefix: string,
  defaultValue: T,
  decode: (stored: string | null) => T,
  encode: (value: T) => string | null,
): ScopedStorageStore<T> {
  const listeners = new Set<() => void>();
  const storageKey = (slug: string) => `${prefix}:${slug}`;
  // Session-scoped fallback for when localStorage is unavailable (storage
  // disabled, some private-browsing modes): writes land here regardless, so
  // dismiss/resume still works for the session — it just won't persist.
  const memory = new Map<string, T>();

  function read(slug: string | undefined): T {
    if (!slug) return defaultValue;
    // `write()` always lands the value in `memory`, so once this session has
    // touched a slug, `memory` is the freshest source of truth — prefer it.
    // localStorage may be stale (its write threw on quota/disabled) or simply
    // unreadable here, and we must not let either case mask a just-applied
    // write.
    if (memory.has(slug)) return memory.get(slug)!;
    try {
      return decode(localStorage.getItem(storageKey(slug)));
    } catch {
      return defaultValue;
    }
  }

  function subscribe(listener: () => void) {
    listeners.add(listener);
    const onStorage = (event: StorageEvent) => {
      if (event.key === null) {
        memory.clear();
        listener();
        return;
      }
      if (!event.key.startsWith(`${prefix}:`)) return;
      const slug = event.key.slice(prefix.length + 1);
      if (!slug) return;
      memory.set(slug, decode(event.newValue));
      listener();
    };
    window.addEventListener("storage", onStorage);
    return () => {
      listeners.delete(listener);
      window.removeEventListener("storage", onStorage);
    };
  }

  function write(slug: string, value: T) {
    memory.set(slug, value);
    try {
      const serialized = encode(value);
      if (serialized === null) {
        localStorage.removeItem(storageKey(slug));
      } else {
        localStorage.setItem(storageKey(slug), serialized);
      }
    } catch {
      // localStorage unavailable — `memory` above keeps the value readable
      // for the session.
    }
    listeners.forEach((listener) => listener());
  }

  function useValue(slug: string | undefined): T {
    return useSyncExternalStore(
      subscribe,
      () => read(slug),
      () => defaultValue,
    );
  }

  return { useValue, write };
}

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
  const store = createScopedStorageStore(
    prefix,
    false,
    (stored) => stored === "true",
    (value) => (value ? "true" : null),
  );
  return { useFlag: store.useValue, write: store.write };
}
