import { cn } from "@/lib/utils";
import { Trash2Icon } from "lucide-react";
import { Button } from "./ui/Button";

export function DeleteButton({
  tooltip,
  onClick,
  size = "md",
  className,
}: {
  tooltip: string;
  size?: "md" | "sm";
  onClick: () => void;
  className?: string;
}): JSX.Element {
  return (
    <Button
      variant="tertiary"
      size={size}
      className={cn(
        "text-muted-foreground hover:text-destructive hover:border-destructive",
        className,
      )}
      tooltip={tooltip}
      onClick={onClick}
    >
      <Trash2Icon className="h-4 w-4" />
    </Button>
  );
}
