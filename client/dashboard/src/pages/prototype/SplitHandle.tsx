import { cn } from "@/lib/utils";

/** The divider itself: a hairline that thickens on hover and while dragging. */
export function SplitHandle({
  dragging,
  ...props
}: Omit<React.HTMLAttributes<HTMLDivElement>, "className" | "title"> & {
  dragging: boolean;
}): JSX.Element {
  return (
    <div
      {...props}
      title="Drag to resize · double-click to reset"
      className="group hidden w-4 shrink-0 cursor-col-resize items-stretch justify-center outline-none md:flex"
    >
      <span
        aria-hidden="true"
        className={cn(
          "w-px transition-all",
          dragging
            ? "bg-foreground w-0.5"
            : "group-hover:bg-neutral-default group-focus-visible:bg-foreground bg-transparent group-hover:w-0.5",
        )}
      />
    </div>
  );
}
