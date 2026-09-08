import { CheckIcon, CopyIcon } from "lucide-react";
import { useRef, useState, type JSX } from "react";

import { Button } from "@/components/ui/button";
import { useOnUnmount } from "@/hooks/useOnUnmount";
import { cn } from "@/lib/utils";

const COPY_CONFIRM_MS = 1500;

function noop(): void {}

// An identifier the operator can take away with one click. `label` names the
// value for the control's accessible name, so two controls in one panel are
// told apart by what they copy.
export function CopyValue({
  label,
  value,
  className,
}: {
  label: string;
  value: string;
  className?: string;
}): JSX.Element {
  // The value copied, not a flag. Peek swaps the record under a mounted panel,
  // and a flag would stand as a confirmation against an id never copied,
  // including where the write resolves after the swap.
  const [copiedValue, setCopiedValue] = useState<string>();
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const copied = copiedValue === value;

  useOnUnmount(() => clearTimeout(timer.current));

  return (
    <span className="flex items-center gap-1">
      <span className={cn("truncate font-mono text-xs", className)}>
        {value}
      </span>
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label={copied ? `${label} copied` : `Copy ${label}`}
        onClick={() => {
          // Undefined outside a secure context, where the call would throw.
          if (!navigator.clipboard?.writeText) return;
          // A check over a failed write sends the operator off with the wrong id.
          void navigator.clipboard.writeText(value).then(() => {
            setCopiedValue(value);
            clearTimeout(timer.current);
            timer.current = setTimeout(
              () => setCopiedValue(undefined),
              COPY_CONFIRM_MS,
            );
          }, noop);
        }}
      >
        {copied ? <CheckIcon /> : <CopyIcon />}
      </Button>
    </span>
  );
}
