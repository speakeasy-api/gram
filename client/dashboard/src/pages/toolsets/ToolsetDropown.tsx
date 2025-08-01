import { Type } from "@/components/ui/type";
import { Combobox } from "@/components/ui/combobox";
import { capitalize } from "@/lib/utils";
import { ToolsetEntry } from "@gram/client/models/components";
import { useListToolsets } from "@gram/client/react-query";
import { useEffect } from "react";

export function ToolsetDropdown({
  selectedToolset,
  setSelectedToolset,
  placeholder = "Select Toolset",
  noLabel = false,
  disabledMessage,
  defaultSelection = "most-recent",
}: {
  selectedToolset: ToolsetEntry | undefined;
  setSelectedToolset: (toolset: ToolsetEntry) => void;
  placeholder?: string;
  noLabel?: boolean;
  disabledMessage?: string;
  defaultSelection?: "most-recent";
}) {
  const { data: toolsets } = useListToolsets();

  const toolsetDropdownItems =
    toolsets?.toolsets?.map((toolset) => ({
      ...toolset,
      label: toolset.name,
      value: toolset.slug,
    })) ?? [];

  toolsetDropdownItems.sort(
    (a, b) => b.updatedAt.getTime() - a.updatedAt.getTime()
  );

  // Set the default selection if no selection is made
  useEffect(() => {
    if (
      defaultSelection === "most-recent" &&
      toolsetDropdownItems.length > 0 &&
      !selectedToolset
    ) {
      setSelectedToolset(toolsetDropdownItems[0]!);
    }
  }, [toolsetDropdownItems, defaultSelection, setSelectedToolset]);

  return (
    <Combobox
      label={noLabel ? undefined : "Toolset"}
      items={toolsetDropdownItems}
      selected={toolsetDropdownItems.find(
        (item) => item.value === selectedToolset?.slug
      )}
      onSelectionChange={setSelectedToolset}
      disabledMessage={disabledMessage}
      className="max-w-fit"
    >
      <Type variant="small" className="font-medium">
        {capitalize(selectedToolset?.name ?? placeholder)}
      </Type>
    </Combobox>
  );
}
