import { Trash2Icon } from "lucide-react";
import { Button } from "./ui/button";
import { cn } from "@/lib/utils";

export function DeleteButton({
  tooltip,
  onClick,
  className,
}: {
  tooltip: string;
  onClick: () => void;
  className?: string;
}) {
  return (
    <Button
      variant="ghost"
      className={cn(
        "text-muted-foreground hover:text-destructive hover:border-destructive",
        className
      )}
      tooltip={tooltip}
      onClick={onClick}
    >
      <Trash2Icon className="w-4 h-4" />
    </Button>
  );
}
