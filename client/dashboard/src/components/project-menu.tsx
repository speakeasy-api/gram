import { cn } from "@/lib/utils.ts";
import { ProjectEntry } from "@gram/client/models/components/projectentry.js";
import React from "react";

import { getGradientColors } from "@/components/gradient-colors";

export function ProjectAvatar({
  project,
  className,
}: {
  project: Pick<ProjectEntry, "id">;
  className?: string;
}): React.JSX.Element {
  const colors = getGradientColors(project.id);
  return (
    <div
      className={cn("h-6 w-6 rounded-full bg-gradient-to-br", className)}
      style={{
        backgroundImage: `linear-gradient(${colors.angle}deg, ${colors.from}, ${colors.to})`,
      }}
    />
  );
}
