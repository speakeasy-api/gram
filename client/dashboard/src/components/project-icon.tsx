import { getGradientColors } from "@/components/gradient-colors";
import { cn } from "@/lib/utils";

/** The same project gets the same colored initial everywhere it appears. */
export function ProjectIcon({
  project,
  className,
}: {
  project: { id: string; name: string };
  className?: string;
}): JSX.Element {
  const colors = getGradientColors(project.id);
  return (
    <span
      role="img"
      aria-label={project.name}
      className={cn(
        "inline-flex size-6 shrink-0 items-center justify-center text-xs font-semibold text-white",
        className,
      )}
      style={{
        backgroundImage: `linear-gradient(${colors.angle}deg, ${colors.from}, ${colors.to})`,
      }}
    >
      {Array.from(project.name.trim())[0]?.toUpperCase() || "P"}
    </span>
  );
}
