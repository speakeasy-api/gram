import { useIsDarkTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { resolveCatalogLogo, type CatalogLogoServer } from "./logo";
import type { ReactNode } from "react";

export function CatalogLogo({
  server,
  alt = "",
  className,
  fallback = null,
}: {
  server: CatalogLogoServer;
  alt?: string;
  className?: string;
  fallback?: ReactNode;
}): JSX.Element {
  const logo = resolveCatalogLogo(server, useIsDarkTheme() ? "dark" : "light");
  return logo.src ? (
    <img src={logo.src} alt={alt} className={cn(className, logo.className)} />
  ) : (
    <>{fallback}</>
  );
}
