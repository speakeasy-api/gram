import { Stack } from "@/components/ui/Stack";
import { cn } from "@/lib/utils";
import { Text } from "@/components/ui/Text";

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
  const blockBackground = "bg-stone-100 dark:bg-stone-900";

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
          <Text
            small
            className={cn("z-1", error && "text-destructive! text-nowrap")}
          >
            {label}
          </Text>
          {labelRHS && (
            <Text muted variant="small" className="z-1">
              {labelRHS}
            </Text>
          )}
        </Stack>
        {error && !labelRHS && (
          <Text small italic className="text-destructive! z-1 w-full pt-1">
            {error}
          </Text>
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
        "bg-card dark:bg-background border-1 border-stone-300 p-2 dark:border-stone-700",
        className,
      )}
    >
      {children}
    </div>
  );
};
