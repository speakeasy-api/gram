import { cn } from "@/lib/utils";
import { Trash2Icon } from "lucide-react";
import { Button } from "@/components/ui/moonshine";
import { SimpleTooltip } from "@/components/ui/tooltip";

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
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={tooltip}>
      <Button
        variant="tertiary"
        size={size === "default" ? "md" : "sm"}
        className={cn(
          "text-muted-foreground hover:text-destructive hover:border-destructive",
          className,
        )}
        aria-label={tooltip}
        onClick={onClick}
      >
        <Trash2Icon className="h-4 w-4" />
      </Button>
    </SimpleTooltip>
  );
}
