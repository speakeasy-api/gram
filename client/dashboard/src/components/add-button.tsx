import { Button } from "@/components/ui/button";
import { PlusIcon } from "lucide-react";

export const AddButton = ({
  onClick,
  tooltip,
}: {
  onClick: () => void;
  tooltip: string;
}) => {
  return (
    <Button
      variant="ghost"
      className="text-muted-foreground hover:text-foreground"
      onClick={onClick}
      tooltip={tooltip}
    >
      <PlusIcon className="w-4 h-4" />
    </Button>
  );
};
