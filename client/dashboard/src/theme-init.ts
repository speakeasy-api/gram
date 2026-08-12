// Storage key for the user's theme choice. MUST equal
// PREFERRED_THEME_STORAGE_KEY in `src/lib/local-storage-keys.ts` — it cannot
// be imported here because vite.config.ts emits this file as a standalone
// classic (non-module) script chunk that runs before first paint; an import
// of a module shared with the main bundle would emit an `import` statement
// that a classic script cannot execute.
const PREFERRED_THEME_STORAGE_KEY = "preferred-theme";

(function () {
  try {
    const theme =
      localStorage.getItem(PREFERRED_THEME_STORAGE_KEY) === "dark"
        ? "dark"
        : "light";
    const root = document.documentElement;
    root.classList.remove("light", "dark");
    root.classList.add(theme);

    const favicon = document.getElementById(
      "favicon",
    ) as HTMLLinkElement | null;
    const faviconAlt = document.getElementById(
      "favicon-alt",
    ) as HTMLLinkElement | null;

    if (favicon) {
      favicon.href = theme === "dark" ? "/favicon-dark.png" : "/favicon.png";
    }

    if (faviconAlt) {
      faviconAlt.href = theme === "dark" ? "/favicon-dark.ico" : "/favicon.ico";
    }
  } catch {
    // localStorage unavailable — fall back to CSS defaults.
  }
})();
