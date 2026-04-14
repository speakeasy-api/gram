import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { Info } from "lucide-react";
import { useState } from "react";

export interface QuerySample {
  value: string;
  label: string;
  description?: string;
}

interface QuerySamplesPopoverProps {
  title: string;
  samples: QuerySample[];
  onSelect: (sample: QuerySample) => void;
  /** Accessible label for the trigger button. */
  ariaLabel?: string;
  /** Alignment of the popover relative to the trigger. */
  align?: "start" | "center" | "end";
  className?: string;
}

export function QuerySamplesPopover({
  title,
  samples,
  onSelect,
  ariaLabel = "Show sample queries",
  align = "end",
  className,
}: QuerySamplesPopoverProps) {
  const [open, setOpen] = useState(false);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={ariaLabel}
          className={cn(
            "text-muted-foreground hover:text-foreground shrink-0 transition-colors",
            className,
          )}
          onClick={(e) => e.stopPropagation()}
        >
          <Info className="size-4" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-[320px] p-0" align={align}>
        <div className="border-border/60 border-b px-3 py-2">
          <span className="text-foreground text-xs font-medium">{title}</span>
          <p className="text-muted-foreground mt-0.5 text-[11px]">
            Click to copy into the input
          </p>
        </div>
        <ul className="max-h-[280px] overflow-y-auto py-1">
          {samples.map((sample) => (
            <li key={sample.value}>
              <button
                type="button"
                onClick={() => {
                  onSelect(sample);
                  setOpen(false);
                }}
                className="hover:bg-accent flex w-full flex-col items-start gap-0.5 px-3 py-2 text-left transition-colors"
              >
                <span className="font-mono text-xs">{sample.value}</span>
                <span className="text-muted-foreground text-[11px]">
                  {sample.label}
                  {sample.description ? ` — ${sample.description}` : null}
                </span>
              </button>
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  );
}
