import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { Check, Copy } from "lucide-react";
import { useState } from "react";

export const CopyButton = ({
  text,
  absolute = false,
  size = "icon",
  className,
}: {
  text: string;
  size?: "icon" | "icon-sm";
  absolute?: boolean;
  className?: string;
}) => {
  const [recentlyCopied, setRecentlyCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(text);
    setRecentlyCopied(true);
    setTimeout(() => {
      setRecentlyCopied(false);
    }, 1000);
  };

  return (
    <Button
      variant={absolute ? "outline" : "ghost"}
      size={size}
      onClick={handleCopy}
      className={cn(
        // "bg-background shadow-md border border-border hover:bg-accent",
        absolute && "absolute top-3 right-3 z-10 shadow-md",
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
