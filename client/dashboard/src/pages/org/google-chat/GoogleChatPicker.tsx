import { Combobox, type DropdownItem } from "@/components/ui/Combobox";

export function GoogleChatPicker({
  items,
  selected,
  ...props
}: {
  id: string;
  label: string;
  items: DropdownItem[];
  selected: string;
  onSelectionChange: (item: DropdownItem) => void;
  disabledMessage?: string;
}): JSX.Element {
  return (
    <Combobox {...props} items={items} selected={selected} searchable>
      {items.find((item) => item.value === selected)?.label ??
        `Choose ${props.label.toLowerCase()}`}
    </Combobox>
  );
}
