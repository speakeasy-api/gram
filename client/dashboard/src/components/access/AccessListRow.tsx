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
import { ChevronDown, X } from "lucide-react";
import { Fragment, type JSX, type ReactNode } from "react";

/**
 * The row shared by the two access surfaces: the organization's role editor
 * and a server's own access list. One reads "what this role can do", the other
 * "who can use this server", but both are a named thing, a sentence describing
 * how far it reaches, and a way to remove it — so both are this component.
 */
export function AccessListRow({
  icon,
  title,
  description,
  meta,
  children,
  onRemove,
  removeLabel,
  removeDisabled,
  removeReason,
}: {
  /** Leading tile. Omitted for rows whose title already identifies itself. */
  icon?: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  /** Right-aligned annotation, before the controls. */
  meta?: ReactNode;
  /** The sentence controls: an InlineChoice, usually. */
  children?: ReactNode;
  onRemove?: () => void;
  removeLabel: string;
  removeDisabled?: boolean;
  removeReason?: string;
}): JSX.Element {
  return (
    <div className="flex items-start gap-3 px-4 py-3">
      {icon && (
        <div className="bg-muted text-muted-foreground flex h-9 w-9 shrink-0 items-center justify-center">
          {icon}
        </div>
      )}
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">{title}</div>
        {description && (
          <Text as="div" small muted className="truncate">
            {description}
          </Text>
        )}
        {children && (
          <div className="mt-2 flex flex-wrap items-center gap-x-1 gap-y-1">
            {children}
          </div>
        )}
      </div>
      {meta}
      {onRemove && (
        <Button
          variant="tertiary"
          size="sm"
          disabled={removeDisabled}
          onClick={onRemove}
          aria-label={removeLabel}
          title={removeDisabled ? removeReason : removeLabel}
        >
          <Button.LeftIcon>
            <X className="h-4 w-4" />
          </Button.LeftIcon>
        </Button>
      )}
    </div>
  );
}

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
