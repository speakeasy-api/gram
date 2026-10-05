import { Field, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";

/**
 * The typed confirmation for a destructive action: what to type, shown on its
 * own, and the field to type it in.
 *
 * The value sits in its own block rather than inline in the label. The values
 * these dialogs confirm are identifiers — subjects, issuer URLs — that are one
 * long unbroken token, and inline they split the sentence around them into
 * ragged columns.
 */
export function TypeToConfirmField({
  id,
  label,
  expected,
  value,
  onChange,
}: {
  id: string;
  label: string;
  expected: string;
  value: string;
  onChange: (value: string) => void;
}): JSX.Element {
  const expectedId = `${id}-expected`;
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <code
        id={expectedId}
        className="bg-muted block px-3 py-2 font-mono text-xs break-all"
      >
        {expected}
      </code>
      <Input
        id={id}
        value={value}
        onChange={onChange}
        aria-describedby={expectedId}
        className="font-mono"
        autoComplete="off"
        spellCheck={false}
      />
    </Field>
  );
}
