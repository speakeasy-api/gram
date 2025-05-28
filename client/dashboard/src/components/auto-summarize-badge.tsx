import { AArrowDown } from "lucide-react";
import { Badge } from "./ui/badge";

export const AutoSummarizeBadge = () => {
  return (
    <Badge
      size="sm"
      className="text-sm capitalize rounded-full px-1 py-0"
      tooltip="Responses from this tool are automatically summarized"
    >
      <AArrowDown size={4} className="w-4! h-4!" />
    </Badge>
  );
};
