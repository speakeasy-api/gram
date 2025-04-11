export function GramLogo({
  animate,
  className,
}: {
  animate?: boolean;
  className?: string;
}) {
  const textGradient =
    "bg-linear-to-b from-stone-600 dark:from-stone-300 to-transparent inline-block text-transparent bg-clip-text";

  const classes = animate
    ? "animate-pulse"
    : "group-hover/logo:text-stone-500 dark:group-hover/logo:text-stone-400 trans";

  return (
    <span
      className={`font-[Mona_Sans] tracking-wide font-[1] text-3xl ${textGradient} ${classes} ${className}`}
    >
      Gram
    </span>
  );
}
