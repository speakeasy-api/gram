import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

/**
 * DetailBody — the width contract for the scrollable body of a detail page.
 *
 * The raw string `mx-auto w-full max-w-[1270px] px-8 py-8` was copy-pasted into
 * ~13 detail pages (mcp, plugins, skills, sources, external services, …). This
 * centralizes that contract so the detail-page column width lives in one place
 * and pairs with `DetailHero` / the detail sidebar rail.
 *
 *   <DetailBody spacing="loose">
 *     <SettingsSection …/>
 *     <SettingsSection …/>
 *   </DetailBody>
 */
export function DetailBody({
  children,
  /** Vertical rhythm between stacked sections. Default: "normal" (space-y-8). */
  spacing = "normal",
  /** Let the body grow to fill a flex column (fullWidth Page.Body layouts). */
  fill = false,
  className,
}: {
  children: ReactNode;
  spacing?: "none" | "normal" | "loose";
  fill?: boolean;
  className?: string;
}): JSX.Element {
  const spacingClass = {
    none: "",
    normal: "space-y-8",
    loose: "space-y-10",
  }[spacing];

  return (
    <div
      className={cn(
        "mx-auto w-full max-w-[1270px] px-8 py-8",
        fill && "flex-1",
        spacingClass,
        className,
      )}
    >
      {children}
    </div>
  );
}
