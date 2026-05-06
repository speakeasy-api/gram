import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * Compact "click to copy" affordance for short identifiers (uuids, workos
 * subs). Renders the value as inline `<code>` with a hover-revealed copy
 * button on the right; the button briefly flips to a check on success.
 *
 * Width is intrinsic to the value — caller is responsible for layout.
 */
export function InlineCopy({
  value,
  label,
  className,
}: {
  value: string;
  label?: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);

  const onCopy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1200);
    } catch {
      // Clipboard API can be unavailable; ignore silently.
    }
  };

  return (
    <button
      type="button"
      onClick={onCopy}
      className={cn(
        "group/copy inline-flex items-center gap-1.5 rounded px-1.5 py-0.5",
        "text-xs text-muted-foreground hover:text-foreground hover:bg-muted/60",
        "transition-colors max-w-full",
        className,
      )}
      aria-label={`Copy ${label ?? "value"}`}
    >
      {label && (
        <span className="font-mono uppercase tracking-wider text-[10px] text-muted-foreground/80 shrink-0">
          {label}
        </span>
      )}
      <code className="font-mono truncate min-w-0">{value}</code>
      {copied ? (
        <Check
          className="size-3 shrink-0 text-[var(--retro-green)]"
          strokeWidth={2.5}
        />
      ) : (
        <Copy className="size-3 shrink-0 opacity-0 group-hover/copy:opacity-70 transition-opacity" />
      )}
    </button>
  );
}
