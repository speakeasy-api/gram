import { Button } from "@/components/ui/moonshine";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Check, Copy, type LucideIcon } from "lucide-react";
import { useState } from "react";

export const CopyButton = ({
  text,
  absolute = false,
  size = "icon",
  className,
  tooltip,
  onCopy,
  icon: Icon = Copy,
}: {
  text: string;
  size?: "icon" | "icon-sm" | "inline";
  absolute?: boolean;
  className?: string;
  tooltip?: string;
  onCopy?: () => void; // Extra callback to do something when the code is copied
  icon?: LucideIcon;
}): JSX.Element => {
  const [recentlyCopied, setRecentlyCopied] = useState(false);

  const handleCopy = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    e.preventDefault();

    void navigator.clipboard.writeText(text);
    setRecentlyCopied(true);
    setTimeout(() => {
      setRecentlyCopied(false);
    }, 1000);
    onCopy?.();
  };

  const button = (
    <Button
      variant={absolute ? "secondary" : "tertiary"}
      size={size === "inline" || size === "icon-sm" ? "sm" : "md"}
      onClick={handleCopy}
      aria-label={tooltip ?? "Copy"}
      className={cn(
        absolute && "absolute top-3 right-3 z-10",
        size === "inline" && "h-6 w-6",
        className,
      )}
    >
      {recentlyCopied ? (
        <Check className="h-5 w-5" />
      ) : (
        <Icon className="h-5 w-5" />
      )}
    </Button>
  );

  return tooltip ? (
    <SimpleTooltip tooltip={tooltip}>{button}</SimpleTooltip>
  ) : (
    button
  );
};
