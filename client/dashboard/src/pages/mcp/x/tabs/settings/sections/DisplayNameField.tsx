import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";

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
  /**
   * Stops typing past this many characters, and shows a live count in the
   * same UTF-16 units the input enforces.
   */
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
          {`${value.length}/${maxLength}`}
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
