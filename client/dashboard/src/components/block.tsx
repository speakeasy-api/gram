import { cn, Stack } from "@speakeasy-api/moonshine";
import { Type } from "./ui/type";

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
}) => {
  const blockBackground = "bg-stone-100 dark:bg-stone-900";

  return (
    <Stack
      className={cn("p-1 rounded-md w-full", className)}
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
          className={cn(
            "px-2 pt-1 rounded-sm rounded-b-none",
            blockBackground,
            labelRHS && "w-full",
          )}
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
          <Type small italic className="pt-1 text-destructive! w-full z-1">
            {error}
          </Type>
        )}
      </Stack>

      <div
        className={cn(
          "h-full w-full p-1 rounded-md rounded-tl-none",
          blockBackground,
          labelRHS && "rounded-tr-none",
        )}
      >
        {children}
      </div>
    </Stack>
  );
};

export const BlockInner = ({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) => {
  return (
    <div
      className={cn(
        "bg-card dark:bg-background rounded-sm p-2 border-1 border-stone-300 dark:border-stone-700",
        className,
      )}
    >
      {children}
    </div>
  );
};
