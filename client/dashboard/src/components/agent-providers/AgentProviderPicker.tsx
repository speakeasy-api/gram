import * as RadioGroupPrimitive from "@radix-ui/react-radio-group";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import { AgentProviderIcon } from "./AgentProviderIcon";

/** One AI tool a set of setup instructions can be for. */
export type AgentProviderOption<T extends string> = {
  id: T;
  name: string;
  iconSource: string;
  /** False shows the tool as coming soon: listed, but not selectable. */
  available?: boolean;
};

/**
 * Chooses which AI tool the setup instructions below it are for: one chip per
 * tool, with its logo, and the instructions switch with the choice. A radio
 * group underneath, so arrow keys move between tools and the choice is
 * announced as one of several.
 */
export function AgentProviderPicker<T extends string>({
  providers,
  value,
  onValueChange,
  "aria-label": ariaLabel,
}: {
  providers: AgentProviderOption<T>[];
  value: T;
  onValueChange: (id: T) => void;
  "aria-label": string;
}): JSX.Element {
  return (
    <RadioGroupPrimitive.Root
      aria-label={ariaLabel}
      value={value}
      onValueChange={(next) => {
        const chosen = providers.find((p) => p.id === next);
        if (chosen !== undefined && chosen.available !== false) {
          onValueChange(chosen.id);
        }
      }}
      orientation="horizontal"
      className="flex flex-wrap gap-3"
    >
      {providers.map((p) => {
        const available = p.available !== false;
        const item = (
          <RadioGroupPrimitive.Item
            key={p.id}
            value={p.id}
            disabled={!available}
            className={cn(
              "relative flex items-center gap-2 border px-3 py-2 text-sm font-medium transition-colors",
              value === p.id
                ? "border-primary bg-primary/5"
                : "border-border hover:border-primary/50 hover:bg-muted/50",
              !available &&
                "hover:border-border cursor-not-allowed opacity-50 hover:bg-transparent",
            )}
          >
            <AgentProviderIcon source={p.iconSource} className="size-5" />
            {p.name}
            {!available && (
              <span className="text-muted-foreground ml-1 text-[10px] tracking-wide uppercase">
                Coming soon
              </span>
            )}
          </RadioGroupPrimitive.Item>
        );
        if (available) return item;
        return (
          <Tooltip key={p.id}>
            {/* A disabled button fires no pointer events, so the span is the
                trigger: hovering a coming-soon tool still says why. */}
            <TooltipTrigger asChild>
              <span className="inline-flex">{item}</span>
            </TooltipTrigger>
            <TooltipContent>
              <p>Coming soon</p>
            </TooltipContent>
          </Tooltip>
        );
      })}
    </RadioGroupPrimitive.Root>
  );
}
