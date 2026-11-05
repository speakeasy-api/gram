// These catalog servers use dark logos that need inversion on dark surfaces.
const DARK_MODE_INVERTED_LOGOS = new Set([
  "app.linear/linear",
  "com.vercel/vercel-mcp",
  "io.github.github/github-mcp-server",
]);

function catalogLogoClassName(registrySpecifier: string): string {
  return DARK_MODE_INVERTED_LOGOS.has(registrySpecifier) ? "dark:invert" : "";
}

export type CatalogLogoServer = {
  registrySpecifier: string;
  iconUrl?: string;
  meta?: Record<string, unknown>;
};

export function resolveCatalogLogo(
  server: CatalogLogoServer,
  theme: "light" | "dark",
): { src: string | undefined; className: string } {
  const icons = server.meta?.["com.speakeasy.ai/icons"];
  const src = (variant: string): string | undefined => {
    if (!icons || typeof icons !== "object") return undefined;
    const icon = (icons as Record<string, unknown>)[variant];
    if (!icon || typeof icon !== "object" || !("src" in icon)) return undefined;
    return typeof icon.src === "string" && icon.src.trim()
      ? icon.src
      : undefined;
  };
  const curated =
    (theme === "dark" ? src("dark") : undefined) ?? src("default");
  return curated
    ? { src: curated, className: "" }
    : {
        src: server.iconUrl,
        className: catalogLogoClassName(server.registrySpecifier),
      };
}

export function hasCatalogLogo(server: CatalogLogoServer | undefined): boolean {
  return (
    !!server &&
    !!(
      resolveCatalogLogo(server, "light").src ||
      resolveCatalogLogo(server, "dark").src
    )
  );
}
