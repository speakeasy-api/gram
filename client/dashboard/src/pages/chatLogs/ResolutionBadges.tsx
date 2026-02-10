import type { ChatResolution } from "@gram/client/models/components";
import { CircularProgress } from "./CircularProgress";

interface ResolutionBadgesProps {
  resolutions: ChatResolution[];
}

export function ResolutionBadges({ resolutions }: ResolutionBadgesProps) {
  if (resolutions.length === 0) {
    return (
      <div className="text-xs text-muted-foreground italic">
        Chat in progress...
      </div>
    );
  }

  return (
    <div className="flex gap-2">
      {resolutions.map((resolution) => (
        <CircularProgress
          key={resolution.id}
          score={resolution.score}
          status={resolution.resolution as "success" | "failure" | "partial"}
          size="sm"
        />
      ))}
    </div>
  );
}
