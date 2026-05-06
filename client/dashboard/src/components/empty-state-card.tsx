import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

export type EmptyStateCardProps = {
  icon: ReactNode;
  heading: string;
  description: ReactNode;
  cta?: ReactNode;
  className?: string;
};

/**
 * Dashed-border empty state card used inside Page.Section bodies on list pages
 * (no MCP servers yet, no environments yet, etc). The heading + description +
 * CTA combination is the standard layout — keep it consistent across pages.
 */
export function EmptyStateCard({
  icon,
  heading,
  description,
  cta,
  className,
}: EmptyStateCardProps) {
  return (
    <div
      className={cn(
        "bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16",
        className,
      )}
    >
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <div className="text-muted-foreground flex h-6 w-6 items-center justify-center [&>svg]:h-6 [&>svg]:w-6">
          {icon}
        </div>
      </div>
      <Type variant="subheading" className="mb-1">
        {heading}
      </Type>
      <Type small muted className="max-w-md text-center">
        {description}
      </Type>
      {cta ? <div className="mt-4">{cta}</div> : null}
    </div>
  );
}
