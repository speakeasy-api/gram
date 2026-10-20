import { Alert } from "@/components/ui/Alert";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { ExternalLink } from "lucide-react";
import type { JSX, ReactNode } from "react";
import { ConsoleRow, type ConsoleField } from "./ConsoleRow";
import { variableProblem, type CatalogVariable } from "./workloadCatalog";
import type { SetupValues } from "./workloadTokenEndpoints";

/** One value a platform's policy asks the operator for. */
export function VariableField({
  variable,
  value,
  onChange,
}: {
  variable: CatalogVariable;
  value: string;
  onChange: (value: string) => void;
}): JSX.Element {
  // Say nothing about an untouched field; an empty value already keeps
  // Continue disabled.
  const problem =
    value.trim().length > 0 ? variableProblem(variable, value) : null;
  const id = `setup-${variable.key}`;

  return (
    <Stack gap={2}>
      <Label htmlFor={id}>{variable.label}</Label>
      <Input
        id={id}
        value={value}
        placeholder={variable.placeholder}
        aria-invalid={problem !== null}
        // Only one of the two is ever on screen, so each case points at its
        // own id rather than naming a box that is not rendered.
        aria-describedby={
          problem !== null
            ? `${id}-error`
            : variable.help !== undefined
              ? `${id}-help`
              : undefined
        }
        onChange={onChange}
      />
      {problem !== null ? (
        <Text id={`${id}-error`} role="alert" small destructive>
          {problem}
        </Text>
      ) : (
        variable.help !== undefined && (
          <Text id={`${id}-help`} muted small>
            {variable.help}
          </Text>
        )
      )}
    </Stack>
  );
}

/** One value the operator types, where no policy declares it. */
export function TextField({
  id,
  label,
  value,
  onChange,
  placeholder,
  help,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  help?: string;
}): JSX.Element {
  return (
    <Stack gap={2}>
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        value={value}
        placeholder={placeholder}
        // Without this the help reads as loose text beside the field and a
        // screen reader announces the label alone.
        aria-describedby={help !== undefined ? `${id}-help` : undefined}
        onChange={onChange}
      />
      {help !== undefined && (
        <Text id={`${id}-help`} muted small>
          {help}
        </Text>
      )}
    </Stack>
  );
}

/**
 * Which user session issuer the derived values come from, and why there are
 * none when there are none.
 */
export function ComputedStatus({
  setupValues,
}: {
  setupValues: SetupValues;
}): JSX.Element {
  if (setupValues.unavailableReason !== null) {
    return (
      <Alert variant="info" alignTop>
        <div className="text-sm">{setupValues.unavailableReason}</div>
      </Alert>
    );
  }
  return (
    <Stack gap={2}>
      <Label htmlFor="setup-token-endpoint">User session issuer</Label>
      <Select
        value={setupValues.selectedEndpointId}
        onValueChange={setupValues.onEndpointChange}
      >
        <SelectTrigger id="setup-token-endpoint" className="w-full">
          <SelectValue placeholder="Select an issuer" />
        </SelectTrigger>
        <SelectContent>
          {setupValues.endpointGroups.map((group) => (
            <SelectGroup key={group.label}>
              <SelectLabel>{group.label}</SelectLabel>
              {group.options.map((option) => (
                <SelectItem key={option.id} value={option.id}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectGroup>
          ))}
        </SelectContent>
      </Select>
      <Text muted small>
        Tokens are issued for the organization or project that owns this issuer.
      </Text>
    </Stack>
  );
}

/**
 * The documents a step points at, on one row: stacked a line each they read
 * as a reading list standing between the reader and the step.
 */
export function Links({
  links,
}: {
  links: { href: string; label: string }[];
}): JSX.Element {
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1">
      {links.map((link) => (
        <a
          key={link.href}
          href={link.href}
          target="_blank"
          rel="noreferrer"
          className="text-foreground inline-flex items-center gap-1.5 text-sm underline underline-offset-4 hover:no-underline"
        >
          {link.label}
          <ExternalLink className="size-3.5 shrink-0" />
        </a>
      ))}
    </div>
  );
}

/**
 * The fields of a dialog in Claude's own console, in its order.
 *
 * That dialog is a compact form — a label, a box, a hint. Rendering each
 * field as a card of its own makes a page several times the height of the
 * thing it describes, which is the opposite of a guide: the reader holds two
 * screens side by side and the shapes do not match.
 */
export function Console({
  rows,
}: {
  rows: {
    label: ReactNode;
    link?: { href: string; label: string };
    fields?: ConsoleField[];
    hint?: ReactNode;
  }[];
}): JSX.Element {
  return (
    <ol className="border-border divide-border divide-y border">
      {rows.map((row, index) => (
        <ConsoleRow
          key={index}
          number={index + 1}
          label={row.label}
          link={row.link}
          fields={row.fields}
          hint={row.hint}
        />
      ))}
    </ol>
  );
}
