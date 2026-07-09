import { Card } from "./card";
import { cn } from "@/lib/utils";

interface DotCardProps {
  children: React.ReactNode;
  icon?: React.ReactNode;
  className?: string;
  overlay?: React.ReactNode;
  onClick?: (e: React.MouseEvent<HTMLDivElement>) => void;
}

/**
 * @deprecated The dot-pattern sidebar is now built into the design-system
 * Card — pass `icon` (and optionally `overlay`) to `Card` instead. This
 * wrapper remains only until existing callsites migrate.
 */
export function DotCard({
  children,
  icon,
  className,
  overlay,
  onClick,
}: DotCardProps): JSX.Element {
  return (
    <Card
      onClick={onClick}
      icon={icon}
      overlay={overlay}
      className={cn("dot-card", className)}
    >
      {children}
    </Card>
  );
}
