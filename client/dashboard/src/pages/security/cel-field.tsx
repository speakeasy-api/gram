import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { TextArea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { useRiskDetectionSchema } from "@gram/client/react-query";
import { Button } from "@speakeasy-api/moonshine";
import { Check, ChevronRight, CircleAlert, Loader2 } from "lucide-react";
import { useState, type JSX } from "react";
import { useCelStatus, type CelStatus } from "./use-cel-status";

/** A named, insertable CEL snippet shown beneath the field. */
export type CelExample = { label: string; expr: string };

function CelStatusLine({ status }: { status: CelStatus }): JSX.Element | null {
  switch (status.kind) {
    case "idle":
      return null;
    case "validating":
      return (
        <span className="text-muted-foreground flex items-center gap-1 text-xs">
          <Loader2 className="h-3 w-3 animate-spin" /> Checking…
        </span>
      );
    case "ok":
      return (
        <span className="text-success-foreground flex items-center gap-1 text-xs">
          <Check className="h-3 w-3" /> Compiles
        </span>
      );
    case "error":
      return (
        <span className="text-destructive flex items-start gap-1 text-xs">
          <CircleAlert className="mt-0.5 h-3 w-3 shrink-0" />
          <span className="font-mono break-all">{status.message}</span>
        </span>
      );
  }
}

/** Collapsible reference listing the fields and matcher functions an author may
 *  use, sourced from the backend's `getDetectionSchema` so it never drifts from
 *  what the engine accepts. */
export function CelReference(): JSX.Element {
  const { data } = useRiskDetectionSchema();
  const [open, setOpen] = useState(false);

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs">
        <ChevronRight
          className={cn("h-3 w-3 transition-transform", open && "rotate-90")}
        />
        Fields &amp; matchers
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-2 space-y-3">
        <ReferenceGroup
          title="Fields"
          items={(data?.variables ?? []).map((v) => ({
            term: v.name,
            note: `${v.type} — ${v.description}`,
          }))}
        />
        <ReferenceGroup
          title="Matchers"
          items={(data?.functions ?? []).map((f) => ({
            term: f.signature,
            note: f.description,
          }))}
        />
      </CollapsibleContent>
    </Collapsible>
  );
}

function ReferenceGroup({
  title,
  items,
}: {
  title: string;
  items: { term: string; note: string }[];
}): JSX.Element | null {
  if (items.length === 0) return null;
  return (
    <div className="space-y-1">
      <p className="text-muted-foreground text-xs font-medium uppercase">
        {title}
      </p>
      <ul className="space-y-1">
        {items.map((item) => (
          <li key={item.term} className="text-xs">
            <code className="text-foreground font-mono">{item.term}</code>
            <span className="text-muted-foreground"> — {item.note}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** A controlled raw-CEL authoring field: a monospace textarea with debounced
 *  backend compile validation, an insertable set of example snippets, and the
 *  schema reference. Used for detection_cel and the policy scope predicates. */
export function CelExpressionField({
  value,
  onChange,
  placeholder,
  examples,
  disabled,
  rows = 3,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  examples?: CelExample[];
  disabled?: boolean;
  rows?: number;
}): JSX.Element {
  const status = useCelStatus(value);

  return (
    <div className="space-y-2">
      <TextArea
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        disabled={disabled}
        rows={rows}
        className="font-mono text-xs"
      />

      <div className="flex min-h-4 items-start justify-between gap-2">
        <CelStatusLine status={status} />
      </div>

      {examples && examples.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-muted-foreground text-xs">Examples:</span>
          {examples.map((ex) => (
            <Button
              key={ex.label}
              variant="tertiary"
              size="sm"
              onClick={() => onChange(ex.expr)}
              disabled={disabled}
            >
              <Button.Text>{ex.label}</Button.Text>
            </Button>
          ))}
        </div>
      )}

      <CelReference />
    </div>
  );
}
