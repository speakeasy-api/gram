import { useNavArea } from "@/hooks/useNavArea";
import { cn } from "@/lib/utils";
import * as React from "react";

/**
 * The area micro-label above every page title ("Observe", "Secure", ...).
 * Auto-derives from the current route's sidebar area; pass `area` to override.
 * Renders nothing when no area applies. Exposed on the Page compound as
 * `Page.Eyebrow`; custom page headers import it from here directly so they
 * don't pull the whole page-layout graph into tests.
 */
export function PageEyebrow({
  area,
  className,
}: {
  area?: React.ReactNode;
  className?: string;
}): React.JSX.Element | null {
  const navArea = useNavArea();
  const label = area ?? navArea;
  if (!label) return null;
  return <div className={cn("text-eyebrow", className)}>{label}</div>;
}
