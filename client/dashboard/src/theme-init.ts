// Storage key for the user's theme choice. MUST equal
// PREFERRED_THEME_STORAGE_KEY in `src/lib/local-storage-keys.ts` — it cannot
// be imported here because vite.config.ts emits this file as a standalone
// classic (non-module) script chunk that runs before first paint; an import
// of a module shared with the main bundle would emit an `import` statement
// that a classic script cannot execute.
const PREFERRED_THEME_STORAGE_KEY = "preferred-theme";

// MUST equal LOGOUT_PRESERVE_WINDOW_NAME_PREFIX in `src/lib/logout-storage.ts`.
// Logout's Clear-Site-Data header empties localStorage before /login paints;
// the snapshot lives on window.name so this script can put theme (and the
// other preserved keys) back before first paint.
const LOGOUT_PRESERVE_WINDOW_NAME_PREFIX = "gram:logout-preserve:";

(function () {
  try {
    if (window.name.startsWith(LOGOUT_PRESERVE_WINDOW_NAME_PREFIX)) {
      const parsed = JSON.parse(
        window.name.slice(LOGOUT_PRESERVE_WINDOW_NAME_PREFIX.length),
      );
      if (Array.isArray(parsed)) {
        for (const entry of parsed) {
          if (
            Array.isArray(entry) &&
            entry.length === 2 &&
            typeof entry[0] === "string" &&
            typeof entry[1] === "string"
          ) {
            localStorage.setItem(entry[0], entry[1]);
          }
        }
      }
      window.name = "";
    }
  } catch {
    // Backup unreadable — continue with whatever localStorage still has.
  }

  try {
    const theme =
      localStorage.getItem(PREFERRED_THEME_STORAGE_KEY) === "dark"
        ? "dark"
        : "light";
    const root = document.documentElement;
    root.classList.remove("light", "dark");
    root.classList.add(theme);
  } catch {
    // localStorage unavailable — fall back to CSS defaults.
  }
})();
