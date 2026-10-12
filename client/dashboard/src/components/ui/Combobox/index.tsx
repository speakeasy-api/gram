import { Button } from "@/components/ui/Button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/Command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { cn } from "@/lib/utils";
import { Stack } from "@/components/ui/Stack";
import { Check, ChevronsUpDown } from "lucide-react";
import { Fragment, ReactNode, useState } from "react";
import { Text } from "@/components/ui/Text";

export type DropdownItem = {
  value: string;
  label: string;
  icon?: ReactNode;
  keywords?: string[];
  onClick?: () => void;
  disabled?: boolean;
  description?: string;
  /** Draws a divider under this item, to set an action apart from choices. */
  separatorAfter?: boolean;
};

export function Combobox<T extends DropdownItem>({
  items,
  children,
  selected,
  onSelectionChange,
  onOpenChange,
  variant = "secondary",
  className,
  id,
  label,
  disabledMessage,
  tooltip,
  searchable = false,
  searchPlaceholder = "Search...",
  contentClassName,
  onSearchChange,
  emptyMessage = "No items found.",
  listFooter,
}: {
  items: T[];
  selected: T | string | undefined;
  onSelectionChange: (value: T) => void;
  onOpenChange?: (open: boolean) => void;
  children: ReactNode;
  className?: string;
  id?: string;
  variant?: Parameters<typeof Button>[0]["variant"];
  label?: string;
  disabledMessage?: string;
  tooltip?: string;
  searchable?: boolean;
  searchPlaceholder?: string;
  contentClassName?: string;
  /** Searches on the caller's side (usually the server) instead of filtering
   * `items` locally: the input is shown and each keystroke is reported here,
   * and `items` is rendered as given. Cleared to "" whenever the list closes. */
  onSearchChange?: (search: string) => void;
  emptyMessage?: ReactNode;
  /** Rendered below the items inside the scrolling list, e.g. a load-more row. */
  listFooter?: ReactNode;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const remoteSearch = !!onSearchChange;

  const setOpenAndResetSearch = (open: boolean) => {
    setOpen(open);
    if (!open && remoteSearch) {
      setSearch("");
      onSearchChange("");
    }
  };

  const handleOpenChange = (open: boolean) => {
    setOpenAndResetSearch(open);
    onOpenChange?.(open);
  };

  const handleSearchChange = (value: string) => {
    setSearch(value);
    onSearchChange?.(value);
  };

  let trigger = (
    <PopoverTrigger asChild>
      <Button
        id={id}
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
        <Command label={searchPlaceholder} shouldFilter={!remoteSearch}>
          {(searchable || remoteSearch || items.length > 4) &&
            (remoteSearch ? (
              <CommandInput
                placeholder={searchPlaceholder}
                className="h-9"
                value={search}
                onValueChange={handleSearchChange}
              />
            ) : (
              <CommandInput placeholder={searchPlaceholder} className="h-9" />
            ))}
          <CommandList>
            <CommandEmpty>{emptyMessage}</CommandEmpty>
            <CommandGroup>
              {items.map((item) => (
                <Fragment key={item.value}>
                  <CommandItem
                    value={item.value}
                    keywords={[item.label, ...(item.keywords ?? [])]}
                    disabled={item.disabled}
                    className={cn(
                      "cursor-pointer truncate",
                      item.separatorAfter && "mb-1.5",
                    )}
                    onSelect={(v) => {
                      onSelectionChange(
                        items.find((item) => item.value === v)!,
                      );
                      setOpenAndResetSearch(false);
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
                  {item.separatorAfter && (
                    <CommandSeparator className="mb-1.5" />
                  )}
                </Fragment>
              ))}
            </CommandGroup>
            {listFooter}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
