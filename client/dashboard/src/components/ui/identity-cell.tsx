import * as React from "react";

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { getInitials } from "@/lib/initials";
import { cn } from "@/lib/utils";

export type IdentityCellSize = "sm" | "md";

export interface IdentityCellProps {
  name: string;
  /** Secondary line under the name — email, role, key prefix, etc. */
  subtitle?: string;
  imageUrl?: string;
  /** Overrides the initials fallback — for non-user principals like API keys or service accounts. */
  fallbackIcon?: React.ReactNode;
  size?: IdentityCellSize;
  className?: string;
}

const sizeClasses: Record<
  IdentityCellSize,
  {
    avatar: string;
    fallback: string;
    gap: string;
    name: string;
    subtitle: string;
  }
> = {
  sm: {
    avatar: "size-6",
    fallback: "text-2xs",
    gap: "gap-2",
    name: "text-sm",
    subtitle: "text-2xs",
  },
  md: {
    avatar: "size-9",
    fallback: "text-xs",
    gap: "gap-3",
    name: "text-sm font-medium",
    subtitle: "text-xs",
  },
};

/** Avatar + name + subtitle composite for table cells and identity lists. */
export function IdentityCell({
  name,
  subtitle,
  imageUrl,
  fallbackIcon,
  size = "md",
  className,
}: IdentityCellProps): React.JSX.Element {
  const sizing = sizeClasses[size];

  return (
    <div className={cn("flex min-w-0 items-center", sizing.gap, className)}>
      <Avatar className={cn(sizing.avatar, "shrink-0")}>
        {imageUrl && <AvatarImage src={imageUrl} alt="" />}
        <AvatarFallback className={cn(sizing.fallback, "font-medium")}>
          {fallbackIcon ?? getInitials(name)}
        </AvatarFallback>
      </Avatar>
      <div className="min-w-0">
        <p className={cn(sizing.name, "truncate")}>{name}</p>
        {subtitle && (
          <p className={cn(sizing.subtitle, "text-muted-foreground truncate")}>
            {subtitle}
          </p>
        )}
      </div>
    </div>
  );
}
