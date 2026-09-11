import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { ChevronDown } from "lucide-react";
import { Fragment, type JSX } from "react";

export interface InlineChoiceOption {
  label: string;
  description?: string;
  onSelect: () => void;
  separatorBefore?: boolean;
}

/**
 * A choice worded as part of a sentence: "Applies to All servers",
 * "Access Use". The dotted underline marks it editable without boxing every
 * row in controls; hover firms the underline.
 */
export function InlineChoice({
  lead,
  value,
  options,
  disabled,
  className,
}: {
  /** The words before the choice, e.g. "Applies to". */
  lead?: string;
  value: string;
  options: InlineChoiceOption[];
  disabled?: boolean;
  className?: string;
}): JSX.Element {
  return (
    <span className="flex items-center gap-1">
      {lead && (
        <Text muted small>
          {lead}
        </Text>
      )}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="tertiary"
            size="sm"
            disabled={disabled}
            className={cn(
              "hover:bg-transparent focus-visible:ring-ring/40 h-auto px-1 py-0 font-sans normal-case tracking-normal underline decoration-dotted underline-offset-4 hover:decoration-solid focus-visible:ring-1 focus-visible:outline-none",
              className,
            )}
          >
            <Button.Text className="font-sans normal-case tracking-normal">
              {value}
            </Button.Text>
            <Button.RightIcon>
              <ChevronDown className="h-4 w-4" />
            </Button.RightIcon>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="max-w-72">
          {options.map((option) => (
            <Fragment key={option.label}>
              {option.separatorBefore && <DropdownMenuSeparator />}
              <DropdownMenuItem onClick={option.onSelect}>
                {option.description ? (
                  <div className="flex flex-col gap-0.5">
                    <span className="font-medium">{option.label}</span>
                    <span className="text-muted-foreground text-xs">
                      {option.description}
                    </span>
                  </div>
                ) : (
                  option.label
                )}
              </DropdownMenuItem>
            </Fragment>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </span>
  );
}
