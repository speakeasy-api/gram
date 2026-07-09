import { ReactElement } from "react";
import { Tool } from "@/lib/toolTypes";
import { CanonicalToolAttributes } from "@gram/client/models/components/canonicaltoolattributes.js";
import { Icon } from "@/components/ui/moonshine";
import { SimpleTooltip } from "./ui/tooltip";

export const ToolVariationBadge = ({
  tool,
}: {
  tool: Tool;
}): ReactElement | null => {
  if (!tool.variation) {
    return null;
  }

  const excludedFields = [
    "createdAt",
    "updatedAt",
    "groupId",
    "id",
    "srcToolName",
    "srcToolUrn",
  ];
  const fieldsChanged = Object.entries(tool.variation)
    .filter(
      ([key, value]) =>
        !excludedFields.includes(key) &&
        value !== tool.canonical?.[key as keyof CanonicalToolAttributes],
    )
    .map(([key]) => key);

  if (fieldsChanged.length === 0) {
    return null;
  }

  return (
    <SimpleTooltip
      tooltip={`This tool has been modified. Fields changed: ${fieldsChanged.join(", ")}`}
    >
      <Icon name="layers-2" size="small" className="text-muted-foreground/70" />
    </SimpleTooltip>
  );
};
