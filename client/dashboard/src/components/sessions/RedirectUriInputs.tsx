import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";

/**
 * An editable list of redirect URIs, one field per URI. Removing the last one
 * is not offered: a registration needs at least one.
 */
export function RedirectUriInputs({
  value,
  onChange,
  disabled,
}: {
  value: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
}): JSX.Element {
  const update = (index: number, uri: string) =>
    onChange(value.map((existing, i) => (i === index ? uri : existing)));
  const remove = (index: number) =>
    onChange(value.filter((_, i) => i !== index));

  return (
    <div className="flex flex-col gap-2">
      {value.map((uri, index) => (
        // Positional keys are fine: fields are only ever appended or removed,
        // and each one is fully controlled by `value`.
        <div key={index} className="flex items-center gap-2">
          {/* Input's own wrapper has no width, so in a row it would shrink
              to its content; this one takes the row's free space. */}
          <div className="min-w-0 flex-1">
            <Input
              value={uri}
              onChange={(next) => update(index, next)}
              placeholder="https://agent.example.com/oauth/callback"
              aria-label={`Redirect URI ${index + 1}`}
              className="font-mono text-xs"
              disabled={disabled}
            />
          </div>
          {value.length > 1 && (
            <Button
              type="button"
              variant="tertiary"
              size="sm"
              icon="x"
              aria-label={`Remove redirect URI ${index + 1}`}
              onClick={() => remove(index)}
              disabled={disabled}
            />
          )}
        </div>
      ))}
      <Button
        type="button"
        variant="secondary"
        size="sm"
        icon="plus"
        className="self-start"
        onClick={() => onChange([...value, ""])}
        disabled={disabled}
      >
        <Button.Text>Add redirect URI</Button.Text>
      </Button>
    </div>
  );
}
