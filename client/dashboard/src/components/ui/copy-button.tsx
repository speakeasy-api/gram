import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { Check, Copy } from "lucide-react";
import { useState } from "react";

export const CopyButton = ({
  text,
  absolute = false,
  size = "icon",
  className,
  tooltip,
  onCopy,
}: {
  text: string;
  size?: "icon" | "icon-sm" | "inline";
  absolute?: boolean;
  className?: string;
  tooltip?: string;
  onCopy?: () => void; // Extra callback to do something when the code is copied
}) => {
  const [recentlyCopied, setRecentlyCopied] = useState(false);

  const handleCopy = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    e.preventDefault();
    
    navigator.clipboard.writeText(text);
    setRecentlyCopied(true);
    setTimeout(() => {
      setRecentlyCopied(false);
    }, 1000);
    onCopy?.();
  };

  return (
    <Button
      variant={absolute ? "outline" : "ghost"}
      size={size ?? "icon"}
      onClick={handleCopy}
      tooltip={tooltip}
      className={cn(
        absolute && "absolute top-3 right-3 z-10 shadow-md",
        size === "inline" && "h-6 w-6",
        className
      )}
      style={absolute ? { boxShadow: "0 2px 8px rgba(0,0,0,0.08)" } : undefined}
    >
      {recentlyCopied ? (
        <Check className="h-5 w-5" />
      ) : (
        <Copy className="h-5 w-5" />
      )}
    </Button>
  );
};
