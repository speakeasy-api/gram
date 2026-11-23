import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";

// Iterating a string walks code points, so an emoji counts once; .length would
// count UTF-16 units.
function codePointLength(value: string): number {
  let count = 0;
  for (const _ of value) count++;
  return count;
}

/** The display name input servers and gateways share on their settings tab. */
export function DisplayNameField({
  id,
  value,
  onChange,
  placeholder,
  error,
  maxLength,
  overLimitMessage,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  /** The failed save's message, when the last save failed. */
  error?: string;
  /** Stops typing past this many characters, and shows a live count. */
  maxLength?: number;
  /** Set when the draft is over the limit: warns rather than blocks typing. */
  overLimitMessage?: string;
}): JSX.Element {
  const overLimit = overLimitMessage !== undefined;
  return (
    <Field
      data-invalid={error !== undefined ? true : undefined}
      className="max-w-md"
    >
      <FieldLabel htmlFor={id}>Display name</FieldLabel>
      <Input
        id={id}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        maxLength={maxLength}
        aria-invalid={error !== undefined || overLimit}
        aria-describedby={maxLength !== undefined ? `${id}-count` : undefined}
        className={cn(overLimit && "border-warning-default")}
      />
      {maxLength !== undefined ? (
        <Text
          small
          muted
          id={`${id}-count`}
          className="block text-right tabular-nums"
        >
          {`${codePointLength(value)}/${maxLength}`}
        </Text>
      ) : null}
      {overLimit ? (
        <Text small warning className="block">
          {overLimitMessage}
        </Text>
      ) : null}
      {error !== undefined && <FieldError>{error}</FieldError>}
    </Field>
  );
}
