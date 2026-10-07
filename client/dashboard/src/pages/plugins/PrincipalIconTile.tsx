import type { ComponentType } from "react";

// The square icon tile that leads each audience row in the plugin assignment
// list, sheet and install mode list, so their labels line up.
export function PrincipalIconTile({
  icon: IconComponent,
}: {
  icon: ComponentType<{ className?: string }>;
}): JSX.Element {
  return (
    <div className="bg-muted text-muted-foreground flex h-9 w-9 shrink-0 items-center justify-center">
      <IconComponent className="h-4 w-4" />
    </div>
  );
}
