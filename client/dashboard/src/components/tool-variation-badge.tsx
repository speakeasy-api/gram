import { Icon } from "@speakeasy-api/moonshine";
import { SimpleTooltip } from "./ui/tooltip";
import { StandardTool } from "@/lib/toolTypes";
import { CanonicalToolAttributes } from "@gram/client/models/components";

export const ToolVariationBadge = ({ tool }: { tool: StandardTool }) => {
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
