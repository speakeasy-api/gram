import { cn } from "@/lib/utils";

/**
 * Marketing-site crosshatch rule: a hairline closed by small hollow squares
 * sitting on the line at each end, the way the section grid on the marketing
 * pages marks its intersections. Used to demarcate the app chrome (sidebar
 * header, page header) instead of a heavier brand bar.
 */
export function HatchRule({ className }: { className?: string }): JSX.Element {
  return (
    <div
      aria-hidden
      className={cn(
        "border-border relative h-px w-full shrink-0 border-t",
        className,
      )}
    >
      <HatchNode className="-left-[3px]" />
      <HatchNode className="-right-[3px]" />
    </div>
  );
}

function HatchNode({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "border-border bg-background absolute -top-[3px] size-[5px] border",
        className,
      )}
    />
  );
}
