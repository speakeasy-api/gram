import { useIdentityTint } from "@/components/gradient-colors";
import { cn } from "@/lib/utils";

/** The same project gets the same colored initial everywhere it appears. */
export function ProjectIcon({
  project,
  className,
  decorative = false,
}: {
  project: { id: string; name: string };
  className?: string;
  decorative?: boolean;
}): JSX.Element {
  const tint = useIdentityTint(project.id);
  return (
    <span
      role={decorative ? undefined : "img"}
      aria-label={decorative ? undefined : project.name}
      aria-hidden={decorative || undefined}
      className={cn(
        "inline-flex size-6 shrink-0 items-center justify-center text-xs font-semibold",
        className,
      )}
      style={tint}
    >
      {Array.from(project.name.trim())[0]?.toUpperCase() || "P"}
    </span>
  );
}
