import { cn } from "@/lib/utils";
import { Stack } from "@speakeasy-api/moonshine";
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
      <Stack direction="horizontal" gap={1} align="center">
        {content}
        <ExternalLinkIcon className="w-4 h-4 text-muted-foreground group-hover:text-foreground" />
      </Stack>
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
