import { cn } from "@/lib/utils";
import { ExternalLinkIcon } from "lucide-react";
import { LinkProps, Link as RouterLink } from "react-router";

export function Link({
  to,
  children,
  external,
  noIcon,
  className,
  ...props
}: LinkProps & { external?: boolean; className?: string; noIcon?: boolean }) {
  let content = children || (typeof to === "string" ? to : undefined);

  if (external && !noIcon) {
    content = (
      <>
        {content}
        <ExternalLinkIcon className="text-muted-foreground group-hover:text-foreground ml-1 inline h-4 w-4 align-[-0.125em]" />
      </>
    );
  }
  return (
    <RouterLink
      to={to}
      target={external ? "_blank" : undefined}
      className={cn("group hover:underline", className)}
      {...props}
    >
      {content}
    </RouterLink>
  );
}
