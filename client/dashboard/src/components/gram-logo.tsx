/* The dark mode fix here is temporary pending a full set of color tokens */

export function GramLogo({ className }: { className?: string }) {
  return (
    <span className={`bsmnt-text-display-xl ${className} dark:text-[#fafafa]`}>
      gram
    </span>
  );
}
