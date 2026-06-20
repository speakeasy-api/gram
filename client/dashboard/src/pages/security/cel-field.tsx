import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { useRiskDetectionSchema } from "@gram/client/react-query";
import { Check, ChevronRight, CircleAlert, Loader2 } from "lucide-react";
import { Suspense, useMemo, useState, type JSX } from "react";
import { useCelStatus, type CelStatus } from "./use-cel-status";
import type { CelSchema } from "./cel-monaco-editor";
import CelMonacoEditorLazy from "./cel-monaco-editor.lazy";

/** A named, insertable CEL snippet shown beneath the field. */
export type CelExample = { label: string; expr: string };

/** Turn the raw cel-go compiler error into a readable message plus the optional
 *  source/caret diagram. The engine wraps errors as `compile "<expr>": ERROR:
 *  <input>:L:C: <msg>\n | <src>\n | ...^`; we strip the wrapper and engine-
 *  internal jargon (ANTLR phrasing, `celenv.*` type names) and keep the caret,
 *  which actually points at the problem. */
function formatCelError(raw: string): { message: string; pointer?: string } {
  // Drop the `compile "…":` / `program "…":` wrapper the engine adds.
  const unwrapped = raw
    .trim()
    .replace(/^(?:compile|program)\s+"(?:[^"\\]|\\.)*":\s*/, "");

  // Separate the diagnostic (first line) from the cel-go caret diagram (the
  // `| <source>` / `| ...^` lines that follow).
  const lines = unwrapped.split("\n");
  const pointerLines = lines
    .slice(1)
    .filter((l) => l.trimStart().startsWith("|"))
    .map((l) => l.replace(/^\s*\|\s?/, ""));
  const pointer = pointerLines.length ? pointerLines.join("\n") : undefined;

  const message = (lines[0] ?? unwrapped)
    .replace(/^ERROR:\s*/, "")
    .replace(/<input>:\d+:(\d+):\s*/, (_m, col: string) => `Column ${col}: `)
    .replace(/Syntax error:\s*/i, "")
    .replace(/no viable alternative at input/i, "unexpected")
    .replace(/(?:mismatched|extraneous) input/i, "unexpected")
    .replace(/'<EOF>'/g, "end of expression")
    .replace(
      /undeclared reference to '([^']+)'(?:\s*\(in container '[^']*'\))?/i,
      "unknown name '$1'",
    )
    // Surface our opaque CEL types under names authors recognise.
    .replace(/celenv\.celTool/g, "tool")
    .replace(/celenv\.field/g, "field")
    .replace(
      /expression must evaluate to bool, got (\w+)/i,
      "expression must be true or false, but it's a $1",
    );

  return { message, pointer };
}

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
    case "error": {
      const { message, pointer } = formatCelError(status.message);
      return (
        <div className="text-destructive flex min-w-0 items-start gap-1 text-xs">
          <CircleAlert className="mt-0.5 h-3 w-3 shrink-0" />
          <div className="min-w-0 space-y-1">
            <span>{message}</span>
            {pointer && (
              <pre className="text-muted-foreground overflow-x-auto font-mono text-[11px] leading-tight whitespace-pre">
                {pointer}
              </pre>
            )}
          </div>
        </div>
      );
    }
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

/** A controlled raw-CEL authoring field: a Monaco editor (syntax highlighting,
 *  schema-driven autocomplete, inline compile-error markers) with debounced
 *  backend validation, an insertable set of example snippets, and the schema
 *  reference. Used for detection_cel and the policy scope predicates. */
export function CelExpressionField({
  value,
  onChange,
  examples,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  examples?: CelExample[];
  disabled?: boolean;
}): JSX.Element {
  const status = useCelStatus(value);
  const { data } = useRiskDetectionSchema();

  // Map the backend editor schema into the Monaco completion shape. Memoized so
  // the editor's completion-schema effect only fires when the schema changes.
  const celSchema = useMemo<CelSchema>(
    () => ({
      variables: (data?.variables ?? []).map((v) => ({
        name: v.name,
        detail: v.type,
        doc: v.description,
        // Member fields of each element when the variable is a list/object
        // (e.g. a `tools` element's name/server/function/args) — drives
        // completion of the macro bind variable in tools.exists(t, t.name…).
        fields: (v.fields ?? []).map((f) => ({
          name: f.name,
          detail: f.type,
          doc: f.description,
        })),
      })),
      functions: (data?.functions ?? []).map((f) => ({
        name: f.name,
        detail: f.signature,
        doc: f.description,
      })),
    }),
    [data],
  );

  return (
    <div className="space-y-2">
      <Suspense
        fallback={
          <div className="border-input bg-input/30 h-16 w-full animate-pulse rounded-md border" />
        }
      >
        <CelMonacoEditorLazy
          value={value}
          onChange={onChange}
          schema={celSchema}
          errorMessage={
            status.kind === "error"
              ? formatCelError(status.message).message
              : null
          }
          disabled={disabled}
        />
      </Suspense>

      <div className="flex min-h-4 items-start justify-between gap-2">
        <CelStatusLine status={status} />
      </div>

      {examples && examples.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-muted-foreground text-xs">Examples:</span>
          {examples.map((ex) => (
            <button
              key={ex.label}
              type="button"
              onClick={() => onChange(ex.expr)}
              disabled={disabled}
              className="border-border text-muted-foreground hover:bg-muted hover:text-foreground rounded-full border px-2.5 py-1 text-xs transition-colors disabled:pointer-events-none disabled:opacity-50"
            >
              {ex.label}
            </button>
          ))}
        </div>
      )}

      <CelReference />
    </div>
  );
}
