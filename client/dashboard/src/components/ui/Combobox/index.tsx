import { Button } from "@/components/ui/Button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/Command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { cn } from "@/lib/utils";
import { Stack } from "@/components/ui/Stack";
import { Check, ChevronsUpDown } from "lucide-react";
import { ReactNode, useState } from "react";
import { Text } from "@/components/ui/Text";

export type DropdownItem = {
  value: string;
  label: string;
  icon?: ReactNode;
  keywords?: string[];
  onClick?: () => void;
  disabled?: boolean;
  description?: string;
};

export function Combobox<T extends DropdownItem>({
  items,
  children,
  selected,
  onSelectionChange,
  onOpenChange,
  variant = "secondary",
  className,
  label,
  disabledMessage,
  tooltip,
  searchable = false,
  searchPlaceholder = "Search...",
  contentClassName,
}: {
  items: T[];
  selected: T | string | undefined;
  onSelectionChange: (value: T) => void;
  onOpenChange?: (open: boolean) => void;
  children: ReactNode;
  className?: string;
  variant?: Parameters<typeof Button>[0]["variant"];
  label?: string;
  disabledMessage?: string;
  tooltip?: string;
  searchable?: boolean;
  searchPlaceholder?: string;
  contentClassName?: string;
}): JSX.Element {
  const [open, setOpen] = useState(false);

  const handleOpenChange = (open: boolean) => {
    setOpen(open);
    onOpenChange?.(open);
  };

  let trigger = (
    <PopoverTrigger asChild>
      <Button
        variant={variant}
        role="combobox"
        aria-expanded={open}
        className={cn("px-2", className)}
        disabled={!!disabledMessage}
        tooltip={disabledMessage || tooltip}
      >
        <div className="flex w-full items-center justify-between gap-2">
          <div className="truncate font-medium">{children}</div>
          <ChevronsUpDown className="opacity-50" />
        </div>
      </Button>
    </PopoverTrigger>
  );

  if (label) {
    trigger = (
      <Stack
        direction="horizontal"
        align="center"
        className="w-fit bg-stone-200 dark:bg-stone-800"
      >
        <Text variant="small" className="px-2">
          {label}
        </Text>
        {trigger}
      </Stack>
    );
  }

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      {trigger}
      <PopoverContent className={cn("w-[200px] p-0", contentClassName)}>
        <Command label={searchPlaceholder}>
          {(searchable || items.length > 4) && (
            <CommandInput placeholder={searchPlaceholder} className="h-9" />
          )}
          <CommandList>
            <CommandEmpty>No items found.</CommandEmpty>
            <CommandGroup>
              {items.map((item) => (
                <CommandItem
                  key={item.value}
                  value={item.value}
                  keywords={[item.label, ...(item.keywords ?? [])]}
                  disabled={item.disabled}
                  className="cursor-pointer truncate"
                  onSelect={(v) => {
                    onSelectionChange(items.find((item) => item.value === v)!);
                    setOpen(false);
                  }}
                >
                  {item.icon}
                  <div className="min-w-0 flex-1">
                    <div className="truncate">{item.label}</div>
                    {item.description ? (
                      <div className="text-muted-foreground truncate text-xs">
                        {item.description}
                      </div>
                    ) : null}
                  </div>
                  <Check
                    className={cn(
                      "ml-auto",
                      (
                        typeof selected === "string"
                          ? selected === item.value
                          : selected?.value === item.value
                      )
                        ? "opacity-100"
                        : "opacity-0",
                    )}
                  />
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
