(function () {
  try {
    const theme =
      localStorage.getItem("preferred-theme") === "dark" ? "dark" : "light";
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
