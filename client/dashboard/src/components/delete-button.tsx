import { cn } from "@/lib/utils";
import { Trash2Icon } from "lucide-react";
import { Button } from "./ui/button";

export function DeleteButton({
  tooltip,
  onClick,
  size = "default",
  className,
}: {
  tooltip: string;
  size?: "default" | "sm";
  onClick: () => void;
  className?: string;
}) {
  return (
    <Button
      variant="ghost"
      size={size}
      className={cn(
        "text-muted-foreground hover:text-destructive hover:border-destructive",
        className,
      )}
      tooltip={tooltip}
      onClick={onClick}
    >
      <Trash2Icon className="w-4 h-4" />
    </Button>
  );
}
