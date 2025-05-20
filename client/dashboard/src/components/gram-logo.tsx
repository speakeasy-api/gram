/* The dark mode fix here is temporary pending a full set of color tokens */

export function GramLogo({ className }: { className?: string }) {
  return (
    <span className={`bsmnt-text-display-xl text-foreground ${className}`}>
      gram
    </span>
  );
}
