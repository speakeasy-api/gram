import { cn } from "@/lib/utils";
import { CopyButton } from "./ui/copy-button";

export function CodeBlock({
  className,
  copyable = true,
  onCopy,
  children,
}: {
  className?: string;
  copyable?: boolean;
  onCopy?: () => void; // Extra actions to take when the code is copied
  children: string;
}) {
  let singleLine = false;
  if (!children.includes("\n")) {
    singleLine = true;
  }
  return (
    <div className={cn("relative bg-muted p-3 rounded-md", className)}>
      {copyable && (
        <CopyButton
          text={children}
          absolute
          size={singleLine ? "icon-sm" : "icon"}
          className={cn(singleLine && "right-1 top-1")}
          onCopy={onCopy}
        />
      )}
      <pre className="break-all whitespace-pre-wrap text-xs pr-10">
        {children}
      </pre>
    </div>
  );
}
