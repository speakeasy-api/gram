import { Badge } from "@/components/ui/Badge";
import { installModeOption, type InstallMode } from "./install-modes";

// InstallModeBadge labels an assignment row with its install mode, or "Mixed"
// for a row of members that don't share one.
export function InstallModeBadge({
  mode,
}: {
  mode: InstallMode | undefined;
}): JSX.Element {
  if (!mode) {
    return (
      <Badge variant="neutral" className="shrink-0">
        Mixed
      </Badge>
    );
  }
  const option = installModeOption(mode);
  return (
    <Badge variant={option.badge} className="shrink-0">
      {option.label}
    </Badge>
  );
}
