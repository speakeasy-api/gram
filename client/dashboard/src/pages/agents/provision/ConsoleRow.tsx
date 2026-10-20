import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { ExternalLink } from "lucide-react";
import { useState, type JSX, type ReactNode } from "react";

/** A value to paste into the platform's console, with its own label. */
export type ConsoleField = {
  /** The console's own name for the field, so it reads as a quotation. */
  label: ReactNode;
  /** The value to paste. Omit where the field is left empty. */
  value?: string;
  hint?: ReactNode;
};

/**
 * One numbered thing to do in the platform's own console: open a page, find
 * a section, click a control, or fill in a set of fields.
 *
 * Laid out as the console lays it out — label, box, hint — so the two can be
 * read side by side without translating between them.
 */
export function ConsoleRow({
  number,
  label,
  link,
  fields,
  hint,
}: {
  /** Its place in the console's own order, which is the order to work in. */
  number: number;
  /** What to do, quoting the console's own words in bold. */
  label: ReactNode;
  /** Where the step is done, when it starts somewhere else. */
  link?: { href: string; label: string };
  /** The values this step asks for, one line each. */
  fields?: ConsoleField[];
  hint?: ReactNode;
}): JSX.Element {
  return (
    <li className="flex items-start gap-3 px-3 py-2.5">
      <span className="text-muted-foreground mt-0.5 w-4 shrink-0 text-right font-mono text-xs tabular-nums">
        {number}
      </span>
      <div className="min-w-0 flex-1 space-y-2">
        <p className="text-sm font-medium">{label}</p>
        {link !== undefined && (
          <a
            href={link.href}
            target="_blank"
            rel="noreferrer"
            className="text-foreground inline-flex items-center gap-1.5 text-sm underline underline-offset-4 hover:no-underline"
          >
            {link.label}
            <ExternalLink className="size-3.5 shrink-0" />
          </a>
        )}
        {fields?.map((field, index) => (
          <CopyField key={index} field={field} />
        ))}
        {hint !== undefined && (
          <Text muted small className="block">
            {hint}
          </Text>
        )}
      </div>
    </li>
  );
}

/** One field of the console's form: its name, and the value to paste in. */
function CopyField({ field }: { field: ConsoleField }): JSX.Element {
  const [copied, setCopied] = useState(false);
  return (
    <div className="space-y-1">
      <p className="text-muted-foreground font-mono text-xs font-medium">
        {field.label}
      </p>
      {field.value !== undefined && (
        <div className="flex items-center gap-2">
          <code className="border-border bg-muted/40 min-w-0 flex-1 truncate border px-2 py-1 text-xs">
            {field.value || "—"}
          </code>
          <Button
            size="sm"
            variant="tertiary"
            disabled={!field.value}
            onClick={() => {
              if (!field.value) return;
              void navigator.clipboard
                .writeText(field.value)
                .then(() => setCopied(true));
            }}
          >
            <Button.Text>{copied ? "Copied" : "Copy"}</Button.Text>
          </Button>
        </div>
      )}
      {field.hint !== undefined && (
        <Text muted small className="block">
          {field.hint}
        </Text>
      )}
    </div>
  );
}
