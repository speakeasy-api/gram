import { createContext, useContext } from "react";

/**
 * True inside the global settings overlay, which draws its own chrome: pages
 * rendered there skip the app's page header (workspace switcher, search).
 */
export const SettingsOverlayContext = createContext(false);

export function useInSettingsOverlay(): boolean {
  return useContext(SettingsOverlayContext);
}

// The last page visited in the app proper, so closing settings returns to it.
const LAST_APP_PATH_KEY = "gram:lastAppPath";

export function rememberAppPath(path: string): void {
  try {
    sessionStorage.setItem(LAST_APP_PATH_KEY, path);
  } catch {
    // sessionStorage unavailable
  }
}

/** The remembered app path, if it belongs to the given organization. */
export function lastAppPath(orgSlug: string): string | null {
  let path: string | null = null;
  try {
    path = sessionStorage.getItem(LAST_APP_PATH_KEY);
  } catch {
    return null;
  }
  return path?.startsWith(`/${orgSlug}/projects/`) ? path : null;
}
