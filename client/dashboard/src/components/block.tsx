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
      className={cn("w-full rounded-md p-1", className)}
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
            "rounded-sm rounded-b-none px-2 pt-1",
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
          <Type small italic className="text-destructive! z-1 w-full pt-1">
            {error}
          </Type>
        )}
      </Stack>

      <div
        className={cn(
          "h-full w-full rounded-md rounded-tl-none p-1",
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
        "bg-card dark:bg-background rounded-sm border-1 border-stone-300 p-2 dark:border-stone-700",
        className,
      )}
    >
      {children}
    </div>
  );
};
