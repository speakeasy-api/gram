import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { cn } from "@/lib/utils";
import { useId } from "react";

export type RadioRow = {
  value: string;
  title: React.ReactNode;
  description?: React.ReactNode;
  /** Beside the title: a suggestion, or why the row can't be picked. */
  badge?: React.ReactNode;
  disabled?: boolean;
  /** Mono title, for protocol values the operator copies from a provider. */
  mono?: boolean;
};

/**
 * Plain radio rows in one hairline box, for picking among a few options that
 * sit inside a section. Cards would nest a box in a box; these do not.
 */
export function RadioRows({
  label,
  rows,
  value,
  onValueChange,
  disabled = false,
  footer,
}: {
  label: string;
  rows: RadioRow[];
  value: string | null;
  onValueChange: (value: string) => void;
  disabled?: boolean;
  /** Inside the box under the last row, such as the form a row reveals. */
  footer?: React.ReactNode;
}): JSX.Element {
  const id = useId();
  return (
    <RadioGroup
      aria-label={label}
      value={value ?? ""}
      onValueChange={onValueChange}
      disabled={disabled}
      className="gap-0 border"
    >
      {rows.map((row, index) => {
        const itemId = `${id}-${row.value}`;
        const rowDisabled = disabled || row.disabled;
        return (
          <label
            key={row.value}
            htmlFor={itemId}
            className={cn(
              "flex items-start gap-3 px-3 py-2.5",
              index > 0 && "border-t",
              rowDisabled
                ? "cursor-not-allowed opacity-50"
                : "hover:bg-muted/40 cursor-pointer",
              value === row.value && "bg-muted/40",
            )}
          >
            {/* Named by the title alone; the badge and description would
                otherwise be read as part of every option's name. */}
            <RadioGroupItem
              id={itemId}
              value={row.value}
              disabled={row.disabled}
              aria-labelledby={`${itemId}-title`}
              aria-describedby={
                row.description ? `${itemId}-description` : undefined
              }
              className="mt-0.5"
            />
            <span className="flex min-w-0 flex-col gap-0.5">
              <span
                className={cn(
                  "flex flex-wrap items-center gap-2 text-sm font-medium",
                  row.mono && "font-mono text-[13px] font-normal",
                )}
              >
                <span id={`${itemId}-title`}>{row.title}</span>
                {row.badge}
              </span>
              {row.description ? (
                <span
                  id={`${itemId}-description`}
                  className="text-muted-foreground truncate text-[13px]"
                >
                  {row.description}
                </span>
              ) : null}
            </span>
          </label>
        );
      })}
      {footer ? (
        <div className="bg-muted/20 border-t px-3 py-3">{footer}</div>
      ) : null}
    </RadioGroup>
  );
}
