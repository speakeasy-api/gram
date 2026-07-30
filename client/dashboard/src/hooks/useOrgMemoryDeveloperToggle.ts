import { useCallback, useSyncExternalStore } from "react";

const STORAGE_KEY = "gram-dev-org-memory";
const CHANGE_EVENT = "org-memory-developer-toggle-change";

function readEnabled(): boolean {
  try {
    return window.sessionStorage.getItem(STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

function subscribe(onStoreChange: () => void): () => void {
  window.addEventListener(CHANGE_EVENT, onStoreChange);
  return () => window.removeEventListener(CHANGE_EVENT, onStoreChange);
}

/**
 * Session-scoped developer opt-in for the Org Memory dashboard.
 *
 * sessionStorage keeps this isolated to the current browser tab and clears it
 * when the tab's session ends. The custom event keeps all mounted consumers in
 * the tab synchronized.
 */
export function useOrgMemoryDeveloperToggle(): readonly [
  boolean,
  (enabled: boolean) => void,
] {
  const enabled = useSyncExternalStore(subscribe, readEnabled, () => false);

  const setEnabled = useCallback((nextEnabled: boolean) => {
    try {
      if (nextEnabled) {
        window.sessionStorage.setItem(STORAGE_KEY, "1");
      } else {
        window.sessionStorage.removeItem(STORAGE_KEY);
      }
    } catch {
      // The in-memory snapshot remains false when sessionStorage is unavailable.
    }
    window.dispatchEvent(new Event(CHANGE_EVENT));
  }, []);

  return [enabled, setEnabled] as const;
}
