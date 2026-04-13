import { AArrowDown } from "lucide-react";
import { Badge } from "./ui/badge";

export const AutoSummarizeBadge = () => {
  return (
    <Badge
      size="sm"
      className="rounded-full px-1 py-0 text-sm capitalize"
      tooltip="Responses from this tool are automatically summarized"
    >
      <AArrowDown size={4} className="h-4! w-4!" />
    </Badge>
  );
};
