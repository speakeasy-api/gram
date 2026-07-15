import { Stack } from "@/components/ui/stack";
import { cn } from "@/lib/utils";
import { Type } from "./ui/type";

/**
 * A labelled container: the label sits on a tab that reads as part of the
 * surface below it. Squared corners and a muted fill, per the design system —
 * the tab and the body share one continuous shape.
 */
export const Block = ({
  label,
  error,
  labelRHS,
  className,
  children,
}: {
  label: string;
  error?: string | null; // Can't be set if labelRHS is set, for now
  labelRHS?: string;
  className?: string;
  children: React.ReactNode;
}): JSX.Element => {
  const blockBackground = "bg-muted";

  return (
    <Stack
      className={cn("w-full p-1", className)}
      align={labelRHS ? "stretch" : "start"}
    >
      <Stack
        direction={"horizontal"}
        className={cn(!labelRHS && "mb-[-2px]")}
        gap={2}
      >
        <Stack
          direction="horizontal"
          align="center"
          justify="space-between"
          className={cn("px-2 pt-1", blockBackground, labelRHS && "w-full")}
        >
          <Type
            small
            className={cn("z-1", error && "text-destructive! text-nowrap")}
          >
            {label}
          </Type>
          {labelRHS && (
            <Type muted variant="small" className="z-1">
              {labelRHS}
            </Type>
          )}
        </Stack>
        {error && !labelRHS && (
          <Type small italic className="text-destructive! z-1 w-full pt-1">
            {error}
          </Type>
        )}
      </Stack>

      <div className={cn("h-full w-full p-1", blockBackground)}>{children}</div>
    </Stack>
  );
};

export const BlockInner = ({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}): JSX.Element => {
  return (
    <div
      className={cn(
        "bg-card dark:bg-background border-neutral-softest border p-2",
        className,
      )}
    >
      {children}
    </div>
  );
};
