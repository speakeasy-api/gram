import { Button, cn, Icon, IconName, Stack } from "@speakeasy-api/moonshine";
import { Type } from "./type";

export interface BigToggleOption {
  value: string;
  icon?: IconName;
  label: string;
  description?: string;
}

export const BigToggle = ({
  options,
  selectedValue,
  onSelect,
  align = "center",
}: {
  options: BigToggleOption[];
  selectedValue: string;
  onSelect: (value: string) => void;
  align?: "center" | "start" | "end";
}) => {
  const classes = {
    both: "px-2 py-1 rounded-sm border-1 w-full",
    active: "border-border bg-card text-foreground",
    inactive:
      "border-transparent text-muted-foreground hover:bg-card hover:cursor-pointer hover:border-border hover:text-foreground",
    activeText: "text-foreground!",
    inactiveText: "text-muted-foreground! italic",
    align: {
      center: "justify-center",
      start: "justify-start",
      end: "justify-end",
    },
  };

  const toggle = (
    <Stack gap={1} className="border rounded-md p-1 w-fit bg-background">
      {options.map((option) => {
        const isActive = selectedValue === option.value;
        return (
          <Button
            key={option.value}
            variant="tertiary"
            size="sm"
            className={cn(
              classes.align[align],
              classes.both,
              isActive ? classes.active : classes.inactive,
            )}
            {...(!isActive ? { onClick: () => onSelect(option.value) } : {})}
          >
            {option.icon && (
              <Button.LeftIcon>
                <Icon name={option.icon} className="h-4 w-4" />
              </Button.LeftIcon>
            )}
            <Button.Text>{option.label}</Button.Text>
          </Button>
        );
      })}
    </Stack>
  );

  return (
    <Stack direction="horizontal" align="center" gap={3} className="mt-2">
      {toggle}
      {options.some((o) => o.description) && (
        <Stack className="gap-4.5">
          {options.map((option) =>
            option.description ? (
              <Type
                key={option.value}
                small
                className={cn(
                  selectedValue === option.value
                    ? classes.activeText
                    : classes.inactiveText,
                )}
              >
                {option.description}
              </Type>
            ) : null,
          )}
        </Stack>
      )}
    </Stack>
  );
};
