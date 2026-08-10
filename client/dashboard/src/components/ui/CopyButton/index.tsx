import { Button, type ButtonProps } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import { Check, Copy, type LucideIcon } from "lucide-react";
import { useState } from "react";

export const CopyButton = ({
  text,
  absolute = false,
  size = "md",
  className,
  tooltip,
  onCopy,
  icon: Icon = Copy,
}: {
  text: string;
  size?: ButtonProps["size"];
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

  return (
    <Button
      variant={absolute ? "secondary" : "tertiary"}
      size={size ?? "md"}
      onClick={handleCopy}
      tooltip={tooltip}
      className={cn(
        absolute && "absolute top-3 right-3 z-10 shadow-md",
        size === "xs" && "h-6 w-6",
        className,
      )}
      style={absolute ? { boxShadow: "0 2px 8px rgba(0,0,0,0.08)" } : undefined}
    >
      {recentlyCopied ? (
        <Check className="h-5 w-5" />
      ) : (
        <Icon className="h-5 w-5" />
      )}
    </Button>
  );
};
